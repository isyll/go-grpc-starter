package handlers

import (
	"context"
	"encoding/json"

	"github.com/isyll/go-grpc-starter/gen/db"
	"github.com/isyll/go-grpc-starter/internal/events"
	"github.com/isyll/go-grpc-starter/internal/metrics"
	"github.com/isyll/go-grpc-starter/internal/store"
	"github.com/isyll/go-grpc-starter/pkg/logger"
)

type AuditLogHandler struct {
	store  *store.Store
	logger *logger.Logger
}

func NewAuditLogHandler(s *store.Store, logx *logger.Logger) *AuditLogHandler {
	return &AuditLogHandler{store: s, logger: logx}
}

func (h *AuditLogHandler) OnAuditLogWritten(
	ctx context.Context,
	evt *events.AuditLogWritten,
) error {
	status := evt.Status
	if status == "" {
		status = "success"
	}

	// Details is a JSONB column; nil map stays NULL (nil bytes).
	var detailsJSON []byte
	if evt.Details != nil {
		b, err := json.Marshal(evt.Details)
		if err != nil {
			metrics.AuditLogWriteFailuresTotal.WithLabelValues("marshal").Inc()
			h.logger.Error(
				"audit log handler: failed to marshal audit details",
				"error", err, "action", evt.Action,
				"admin_id", evt.AdminID, "request_id", evt.RequestID,
			)
			return err
		}
		detailsJSON = b
	}

	err := h.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		return q.CreateAuditLog(ctx, db.CreateAuditLogParams{
			AdminID:    evt.AdminID,
			Action:     evt.Action,
			Resource:   evt.Resource,
			ResourceID: store.NullStr(evt.ResourceID),
			Details:    detailsJSON,
			Status:     status,
			IpAddress:  store.NullStr(evt.IPAddress),
			UserAgent:  store.NullStr(evt.UserAgent),
			RequestID:  store.NullStr(evt.RequestID),
		})
	})
	if err != nil {
		metrics.AuditLogWriteFailuresTotal.WithLabelValues("db_write").Inc()
		h.logger.Error(
			"audit log handler: failed to write audit row",
			"error", err, "action", evt.Action,
			"admin_id", evt.AdminID, "request_id", evt.RequestID,
		)
		return err
	}
	return nil
}
