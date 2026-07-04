package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/isyll/go-grpc-starter/gen/db"
	"github.com/isyll/go-grpc-starter/internal/metrics"
	"github.com/isyll/go-grpc-starter/internal/store"
	"github.com/isyll/go-grpc-starter/pkg/logger"
)

var ErrOutboxDuplicate = errors.New(
	"outbox: duplicate pending row with same (event_type, dedupe_key)",
)

type OutboxEvent struct {
	ID              int64
	EventType       string
	Payload         json.RawMessage
	DedupeKey       *string
	CreatedAt       time.Time
	ProcessedAt     *time.Time
	RetryCount      int
	LastError       *string
	LastAttemptedAt *time.Time
}

type OutboxRepository struct {
	store  *store.Store
	logger *logger.Logger
}

func NewOutboxRepository(s *store.Store, logx *logger.Logger) *OutboxRepository {
	return &OutboxRepository{store: s, logger: logx}
}

func toOutboxEvent(r db.EventsOutbox) *OutboxEvent {
	return &OutboxEvent{
		ID:              r.ID,
		EventType:       r.EventType,
		Payload:         r.Payload,
		DedupeKey:       r.DedupeKey,
		CreatedAt:       store.Time(r.CreatedAt),
		ProcessedAt:     store.TimePtr(r.ProcessedAt),
		RetryCount:      int(r.RetryCount),
		LastError:       r.LastError,
		LastAttemptedAt: store.TimePtr(r.LastAttemptedAt),
	}
}

// Write inserts an outbox row. When called inside a service transaction it
// joins that transaction, so the row commits atomically with the domain write.
func (r *OutboxRepository) Write(ctx context.Context, evt Event) (int64, error) {
	payload, err := json.Marshal(evt)
	if err != nil {
		return 0, fmt.Errorf("outbox: marshal: %w", err)
	}
	var dedupe *string
	if d, ok := evt.(Dedupable); ok {
		if k := strings.TrimSpace(d.OutboxDedupeKey()); k != "" {
			dedupe = &k
		}
	}

	var id int64
	err = r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		var qErr error
		id, qErr = q.InsertOutbox(ctx, db.InsertOutboxParams{
			EventType: evt.EventType(),
			Payload:   payload,
			DedupeKey: dedupe,
		})
		return qErr
	})
	if err != nil {
		if isOutboxDedupeViolation(err) {
			return 0, ErrOutboxDuplicate
		}
		return 0, fmt.Errorf("outbox: insert: %w", err)
	}
	return id, nil
}

func isOutboxDedupeViolation(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" &&
		strings.Contains(pgErr.ConstraintName, "outbox_pending_dedupe")
}

func (r *OutboxRepository) Publish(ctx context.Context, evt Event) error {
	if _, err := r.Write(ctx, evt); err != nil {
		if errors.Is(err, ErrOutboxDuplicate) {
			r.logger.Debug("outbox publish deduplicated", "event", evt.EventType())
			return nil
		}
		r.logger.Error("outbox publish failed", "event", evt.EventType(), "error", err)
		return err
	}
	return nil
}

func (r *OutboxRepository) MarkProcessed(ctx context.Context, id int64) error {
	err := r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		return q.MarkOutboxProcessed(ctx, id)
	})
	if err != nil {
		metrics.OutboxMarkFailuresTotal.WithLabelValues("processed").Inc()
		r.logger.Warn("outbox: mark processed failed", "id", id, "error", err)
		return fmt.Errorf("outbox: mark processed %d: %w", id, err)
	}
	return nil
}

func (r *OutboxRepository) MarkFailed(ctx context.Context, id int64, errMsg string) error {
	err := r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		return q.MarkOutboxFailed(ctx, db.MarkOutboxFailedParams{ID: id, LastError: store.Ptr(errMsg)})
	})
	if err != nil {
		metrics.OutboxMarkFailuresTotal.WithLabelValues("failed").Inc()
		r.logger.Warn("outbox: mark failed failed", "id", id, "error", err)
		return fmt.Errorf("outbox: mark failed %d: %w", id, err)
	}
	return nil
}

func (r *OutboxRepository) DeadLetter(
	ctx context.Context, row *OutboxEvent, reason, lastErr string,
) error {
	err := r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		return q.InsertOutboxDeadLetter(ctx, db.InsertOutboxDeadLetterParams{
			SourceID:      row.ID,
			EventType:     row.EventType,
			Payload:       row.Payload,
			FailureReason: reason,
			LastError:     store.NullStr(lastErr),
		})
	})
	if err != nil {
		return fmt.Errorf("outbox: dead-letter %d: %w", row.ID, err)
	}
	metrics.OutboxDeadLetterTotal.WithLabelValues(reason).Inc()
	return nil
}

func (r *OutboxRepository) PendingBatch(ctx context.Context, limit int) ([]*OutboxEvent, error) {
	var out []*OutboxEvent
	err := r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		rows, err := q.PendingOutboxBatch(ctx, db.PendingOutboxBatchParams{
			RetryCount: outboxMaxRetry,
			Limit:      int32(limit),
		})
		if err != nil {
			return err
		}
		out = make([]*OutboxEvent, len(rows))
		for i, row := range rows {
			out[i] = toOutboxEvent(row)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("outbox: pending batch: %w", err)
	}
	return out, nil
}

const outboxMaxRetry = 10

const (
	drainBreakerThreshold = 3
	drainBreakerCooldown  = 60 * time.Second
)
