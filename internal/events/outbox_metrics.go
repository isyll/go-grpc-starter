package events

import (
	"context"
	"fmt"

	"github.com/isyll/go-grpc-starter/gen/db"
	"github.com/isyll/go-grpc-starter/internal/metrics"
)

func (r *OutboxRepository) UpdateMetrics(ctx context.Context) error {
	return r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		stats, err := q.OutboxStats(ctx, outboxMaxRetry)
		if err != nil {
			return fmt.Errorf("outbox metrics: %w", err)
		}
		metrics.OutboxPendingRows.Set(float64(stats.Pending))
		metrics.OutboxExhaustedRows.Set(float64(stats.Exhausted))
		metrics.OutboxDrainLagSeconds.Set(stats.OldestPendingSecs)

		dlDepth, err := q.CountOutboxDeadLetters(ctx)
		if err != nil {
			return fmt.Errorf("outbox dead-letter depth: %w", err)
		}
		metrics.OutboxDeadLetterDepth.Set(float64(dlDepth))
		return nil
	})
}
