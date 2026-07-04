package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	"github.com/hibiken/asynq"

	"github.com/isyll/go-grpc-starter/internal/metrics"
	"github.com/isyll/go-grpc-starter/internal/store"
	"github.com/isyll/go-grpc-starter/pkg/logger"
)

var ErrEnqueueFailed = errors.New(
	"one or more async event enqueues failed",
)

type AsyncDispatcher interface {
	Enqueue(
		ctx context.Context,
		evt Event,
		opts []asynq.Option,
	) error
}

type Bus struct {
	mu        sync.RWMutex
	subs      map[string][]subscription
	asynq     AsyncDispatcher
	outbox    *OutboxRepository
	logger    *logger.Logger
	drainKick chan struct{}

	drainBreakerFailures int
	drainBreakerOpenedAt time.Time
}

type drainKey struct{}

type subscription struct {
	name      string
	invoke    func(context.Context, Event) error
	isAsync   bool
	asyncOpts []asynq.Option
	taskIDFn  func(Event) string
	critical  bool
}

func New(dispatcher AsyncDispatcher, logx *logger.Logger) *Bus {
	return &Bus{
		subs:      map[string][]subscription{},
		asynq:     dispatcher,
		logger:    logx,
		drainKick: make(chan struct{}, 1),
	}
}

func NewWithOutbox(
	dispatcher AsyncDispatcher,
	outbox *OutboxRepository,
	logx *logger.Logger,
) *Bus {
	return &Bus{
		subs:      map[string][]subscription{},
		asynq:     dispatcher,
		outbox:    outbox,
		logger:    logx,
		drainKick: make(chan struct{}, 1),
	}
}

func (b *Bus) kickDrain() {
	if b.drainKick == nil {
		return
	}
	select {
	case b.drainKick <- struct{}{}:
	default:
	}
}

func Subscribe[T Event](
	bus *Bus,
	handler Handler[T],
	opts ...SubscribeOption,
) {
	var zero T
	cfg := newSubConfig(opts)
	bus.add(zero.EventType(), subscription{
		name:     handlerName(handler),
		critical: cfg.critical,
		invoke: func(ctx context.Context, e Event) error {
			typed, ok := e.(T)
			if !ok {
				return fmt.Errorf(
					"events: type mismatch for %s: got %T",
					zero.EventType(), e,
				)
			}
			return handler(ctx, typed)
		},
	})
}

func SubscribeAsync[T Event](
	bus *Bus,
	handler Handler[T],
	opts ...SubscribeOption,
) {
	var zero T
	cfg := newSubConfig(opts)
	if cfg.taskIDFn == nil {
		panic(fmt.Sprintf(
			"events: SubscribeAsync for %s requires "+
				"WithTaskIDFn for idempotency",
			zero.EventType(),
		))
	}
	bus.add(zero.EventType(), subscription{
		name:      handlerName(handler),
		isAsync:   true,
		asyncOpts: cfg.asyncOpts,
		taskIDFn:  cfg.taskIDFn,
		invoke: func(ctx context.Context, e Event) error {
			typed, ok := e.(T)
			if !ok {
				return fmt.Errorf(
					"events: type mismatch for %s: got %T",
					zero.EventType(), e,
				)
			}
			return handler(ctx, typed)
		},
	})
}

func (b *Bus) Publish(ctx context.Context, evt Event) error {
	b.mu.RLock()
	subs := append(
		[]subscription(nil),
		b.subs[evt.EventType()]...,
	)
	b.mu.RUnlock()

	if len(subs) == 0 {
		return nil
	}

	metrics.EventsPublishedTotal.
		WithLabelValues(evt.EventType()).
		Inc()

	fromDrain := ctx.Value(drainKey{}) != nil

	var hasAsync bool
	for _, s := range subs {
		if s.isAsync {
			hasAsync = true
			break
		}
	}

	if fromDrain {
		return b.dispatch(ctx, evt, subs)
	}

	if store.InTx(ctx) && b.outbox != nil {
		if _, err := b.outbox.Write(ctx, evt); err != nil {
			return fmt.Errorf(
				"events: outbox write in tx failed: %w", err,
			)
		}
		b.kickDrain()
		return nil
	}

	var (
		outboxID int64
		err      error
	)
	if hasAsync && b.outbox != nil {
		outboxID, err = b.outbox.Write(ctx, evt)
		if err != nil {
			b.logger.Warn(
				"outbox write failed; event may be lost on crash",
				"event", evt.EventType(),
				"error", err,
			)
		}
	}

	dispatchErr := b.dispatch(ctx, evt, subs)

	if outboxID > 0 {
		if errors.Is(dispatchErr, ErrEnqueueFailed) {
			_ = b.outbox.MarkFailed(
				ctx, outboxID,
				"one or more enqueues failed",
			)
			b.kickDrain()
		} else {
			_ = b.outbox.MarkProcessed(ctx, outboxID)
		}
	}

	return dispatchErr
}

func (b *Bus) dispatch(
	ctx context.Context,
	evt Event,
	subs []subscription,
) error {
	var firstCritErr error
	allEnqueued := true

	for _, s := range subs {
		if s.isAsync {
			if b.asynq == nil {
				b.logger.Warn(
					"event async handler skipped: dispatcher not configured",
					"event",
					evt.EventType(),
					"handler",
					s.name,
				)
				allEnqueued = false
				continue
			}
			opts := s.asyncOpts
			if s.taskIDFn != nil {
				opts = append(
					append([]asynq.Option(nil), opts...),
					asynq.TaskID(s.taskIDFn(evt)),
				)
			}
			if err := b.asynq.Enqueue(
				ctx, evt, opts,
			); err != nil {
				b.logger.Error(
					"event async enqueue failed",
					"event", evt.EventType(),
					"handler", s.name,
					"error", err,
				)
				allEnqueued = false
				metrics.EventsEnqueueFailuresTotal.
					WithLabelValues(evt.EventType()).
					Inc()
			}
			continue
		}

		if err := b.runSync(ctx, evt, s); err != nil {
			b.logger.Warn(
				"event sync handler failed",
				"event", evt.EventType(),
				"handler", s.name,
				"error", err,
			)
			if s.critical && firstCritErr == nil {
				firstCritErr = err
			}
		}
	}

	if !allEnqueued {
		return ErrEnqueueFailed
	}
	return firstCritErr
}

func (b *Bus) DrainOutbox(
	ctx context.Context,
	interval time.Duration,
) {
	if b.outbox == nil {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.drainOnce(ctx)
		case <-b.drainKick:
			b.drainOnce(ctx)
		}
	}
}

func (b *Bus) drainOnce(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			metrics.WorkerPanicsTotal.
				WithLabelValues("outbox_drain").
				Inc()
			b.logger.Error(
				"outbox drain: panic recovered",
				"panic", r,
				"stack_trace", string(debug.Stack()),
			)
		}
	}()

	if b.drainBreakerFailures >= drainBreakerThreshold {
		if time.Since(b.drainBreakerOpenedAt) < drainBreakerCooldown {
			return
		}
		b.logger.Info(
			"outbox drain: breaker cooldown elapsed; retrying",
			"consecutive_failures", b.drainBreakerFailures,
		)
	}

	rows, err := b.outbox.PendingBatch(ctx, 100)
	if err != nil {
		b.logger.Error(
			"outbox drain: fetch pending failed",
			"error", err,
		)
		return
	}
	if len(rows) == 0 {
		return
	}

	var publishFailures, publishAttempts int

	drainCtx := context.WithValue(ctx, drainKey{}, true)
	for _, row := range rows {
		if row.RetryCount >= outboxMaxRetry {
			lastErr := ""
			if row.LastError != nil {
				lastErr = *row.LastError
			}
			if dlErr := b.outbox.DeadLetter(
				ctx, row, "retry_exhausted", lastErr,
			); dlErr != nil {
				b.logger.Error(
					"outbox drain: dead-letter failed",
					"id", row.ID, "error", dlErr,
				)
			}
			if mpErr := b.outbox.MarkProcessed(
				ctx, row.ID,
			); mpErr != nil {
				b.logger.Error(
					"outbox drain: mark processed failed",
					"id", row.ID, "error", mpErr,
				)
			}
			continue
		}

		factory := FactoryFor(row.EventType)
		if factory == nil {
			b.logger.Warn(
				"outbox drain: unknown event type",
				"type", row.EventType,
				"id", row.ID,
			)
			if dlErr := b.outbox.DeadLetter(
				ctx, row, "unknown_event_type", "",
			); dlErr != nil {
				b.logger.Error(
					"outbox drain: dead-letter failed",
					"id", row.ID, "error", dlErr,
				)
			}
			if mpErr := b.outbox.MarkProcessed(
				ctx, row.ID,
			); mpErr != nil {
				b.logger.Error(
					"outbox drain: mark processed failed",
					"id", row.ID, "error", mpErr,
				)
			}
			continue
		}

		evt := factory()
		if err := json.Unmarshal(row.Payload, evt); err != nil {
			b.logger.Error(
				"outbox drain: unmarshal failed",
				"type", row.EventType,
				"id", row.ID,
				"error", err,
			)
			if dlErr := b.outbox.DeadLetter(
				ctx, row, "unmarshal_failed", err.Error(),
			); dlErr != nil {
				b.logger.Error(
					"outbox drain: dead-letter failed",
					"id", row.ID, "error", dlErr,
				)
			}
			if mpErr := b.outbox.MarkProcessed(
				ctx, row.ID,
			); mpErr != nil {
				b.logger.Error(
					"outbox drain: mark processed failed",
					"id", row.ID, "error", mpErr,
				)
			}
			continue
		}

		publishAttempts++
		if err := b.Publish(drainCtx, evt); err != nil {
			publishFailures++
			if mfErr := b.outbox.MarkFailed(
				ctx, row.ID, err.Error(),
			); mfErr != nil {
				b.logger.Error(
					"outbox drain: mark failed failed",
					"id", row.ID, "error", mfErr,
				)
			}
		} else {
			if mpErr := b.outbox.MarkProcessed(
				ctx, row.ID,
			); mpErr != nil {
				b.logger.Error(
					"outbox drain: mark processed failed",
					"id", row.ID, "error", mpErr,
				)
			}
		}
	}

	switch {
	case publishAttempts == 0:
	case publishFailures == publishAttempts:
		b.drainBreakerFailures++
		if b.drainBreakerFailures == drainBreakerThreshold {
			b.drainBreakerOpenedAt = time.Now()
			b.logger.Warn(
				"outbox drain: breaker opened",
				"consecutive_failures", b.drainBreakerFailures,
				"cooldown", drainBreakerCooldown,
			)
		}
	default:
		if b.drainBreakerFailures >= drainBreakerThreshold {
			b.logger.Info(
				"outbox drain: breaker closed",
				"consecutive_failures_before_close",
				b.drainBreakerFailures,
			)
		}
		b.drainBreakerFailures = 0
	}
}

func (b *Bus) InvokeAsync(
	ctx context.Context,
	evt Event,
) error {
	b.mu.RLock()
	subs := append(
		[]subscription(nil),
		b.subs[evt.EventType()]...,
	)
	b.mu.RUnlock()

	var firstErr error
	for _, s := range subs {
		if !s.isAsync {
			continue
		}
		err := b.runSync(ctx, evt, s)
		if err != nil {
			b.logger.Error(
				"event async handler failed",
				"event", evt.EventType(),
				"handler", s.name,
				"error", err,
			)
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	return firstErr
}

func (b *Bus) HasSubscribers(eventType string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return len(b.subs[eventType]) > 0
}

func (b *Bus) add(eventType string, sub subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.subs[eventType] = append(b.subs[eventType], sub)
}

func (b *Bus) runSync(
	ctx context.Context,
	evt Event,
	s subscription,
) (err error) {
	start := time.Now()
	defer func() {
		elapsed := time.Since(start).Seconds()
		metrics.EventsHandlerDurationSeconds.
			WithLabelValues(evt.EventType(), s.name, "sync").
			Observe(elapsed)

		if r := recover(); r != nil {
			err = fmt.Errorf("events: handler panic: %v", r)
			b.logger.Error(
				"event handler panic recovered",
				"event", evt.EventType(),
				"handler", s.name,
				"panic", r,
			)
		}
	}()

	return s.invoke(ctx, evt)
}

func handlerName(h any) string {
	v := reflect.ValueOf(h)
	if !v.IsValid() || v.Kind() != reflect.Func {
		return "<unknown>"
	}
	if fn := runtime.FuncForPC(v.Pointer()); fn != nil {
		return fn.Name()
	}
	return "<unknown>"
}
