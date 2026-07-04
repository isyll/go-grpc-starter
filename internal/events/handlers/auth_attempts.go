package handlers

import (
	"context"

	"github.com/isyll/go-grpc-starter/gen/db"
	"github.com/isyll/go-grpc-starter/internal/events"
	"github.com/isyll/go-grpc-starter/internal/store"
	"github.com/isyll/go-grpc-starter/pkg/logger"
)

type AuthAttemptHandler struct {
	store  *store.Store
	logger *logger.Logger
}

func NewAuthAttemptHandler(
	s *store.Store,
	logx *logger.Logger,
) *AuthAttemptHandler {
	return &AuthAttemptHandler{store: s, logger: logx}
}

func (h *AuthAttemptHandler) OnAuthAttemptRecorded(
	ctx context.Context,
	evt *events.AuthAttemptRecorded,
) error {
	var userID *int64
	if evt.UserID != 0 {
		userID = &evt.UserID
	}

	remaining := int32(evt.Remaining)

	err := h.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		return q.CreateLoginAttempt(ctx, db.CreateLoginAttemptParams{
			Email:     evt.Email,
			UserID:    userID,
			Channel:   evt.Channel,
			Outcome:   evt.Outcome,
			Remaining: &remaining,
			IpAddress: store.NullStr(evt.IPAddress),
			UserAgent: store.NullStr(evt.UserAgent),
			DeviceID:  store.NullStr(evt.DeviceID),
			RequestID: store.NullStr(evt.RequestID),
		})
	})
	if err != nil {
		h.logger.Error(
			"auth attempt handler: failed to persist login attempt row",
			"error", err,
			"email", evt.Email,
			"channel", evt.Channel,
			"outcome", evt.Outcome,
			"request_id", evt.RequestID,
		)
		return err
	}

	return nil
}
