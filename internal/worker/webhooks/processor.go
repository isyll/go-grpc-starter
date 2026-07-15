package webhooks

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"

	"github.com/isyll/go-grpc-starter/pkg/logger"
)

type Processor struct {
	logger *logger.Logger
}

func NewProcessor(logx *logger.Logger) *Processor {
	return &Processor{logger: logx}
}

// ProcessTask decodes a verified webhook. Extend it to settle payments,
// update orders, or emit a domain event through the outbox.
func (p *Processor) ProcessTask(_ context.Context, task *asynq.Task) error {
	var ev ReceivedEvent
	if err := json.Unmarshal(task.Payload(), &ev); err != nil {
		return fmt.Errorf("webhooks: decode task: %w: %w", err, asynq.SkipRetry)
	}

	p.logger.Info("webhook received",
		"provider", ev.Provider,
		"bytes", len(ev.Payload),
		"request_id", ev.RequestID,
	)
	return nil
}
