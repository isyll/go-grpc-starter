package webhooks

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"

	"github.com/isyll/go-grpc-starter/pkg/logger"
)

const (
	defaultMaxRetry = 5
	defaultTimeout  = 30 * time.Second
)

type Dispatcher struct {
	client *asynq.Client
	logger *logger.Logger
}

func NewDispatcher(redisAddr, redisPassword string, logx *logger.Logger) *Dispatcher {
	client := asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr, Password: redisPassword})
	return &Dispatcher{client: client, logger: logx}
}

func (d *Dispatcher) Enqueue(ctx context.Context, ev ReceivedEvent) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("webhooks: marshal event: %w", err)
	}

	opts := []asynq.Option{
		asynq.Queue(queueWebhooks),
		asynq.MaxRetry(defaultMaxRetry),
		asynq.Timeout(defaultTimeout),
	}

	task := asynq.NewTask(TaskWebhookReceived, body, opts...)
	info, err := d.client.EnqueueContext(ctx, task)
	if err != nil {
		return fmt.Errorf("webhooks: enqueue %s: %w", ev.Provider, err)
	}

	d.logger.Debug("webhook enqueued",
		"task_id", info.ID,
		"queue", info.Queue,
		"provider", ev.Provider,
		"request_id", ev.RequestID,
	)
	return nil
}

func (d *Dispatcher) Close() error {
	return d.client.Close()
}
