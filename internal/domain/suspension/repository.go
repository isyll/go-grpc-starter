// Package suspension manages account suspensions used by moderation.
package suspension

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/isyll/go-grpc-starter/gen/db"
	"github.com/isyll/go-grpc-starter/internal/models"
	"github.com/isyll/go-grpc-starter/internal/store"
)

type Repository interface {
	GetActiveByUserID(ctx context.Context, userID int64) (*models.AccountSuspension, error)
	Create(ctx context.Context, s *models.AccountSuspension) error
	DeactivateByUserID(ctx context.Context, userID int64) error
}

type repository struct {
	store *store.Store
}

func NewRepository(s *store.Store) Repository {
	return &repository{store: s}
}

func toSuspension(row db.AuthAccountSuspension) *models.AccountSuspension {
	return &models.AccountSuspension{
		ID:             row.ID,
		UserID:         row.UserID,
		Reason:         models.SuspensionReason(row.Reason),
		Details:        store.Str(row.Details),
		SuspendedAt:    store.Time(row.SuspendedAt),
		SuspendedUntil: store.TimePtr(row.SuspendedUntil),
		IsPermanent:    row.IsPermanent,
		CreatedAt:      store.Time(row.CreatedAt),
		UpdatedAt:      store.Time(row.UpdatedAt),
	}
}

func (r *repository) GetActiveByUserID(
	ctx context.Context, userID int64,
) (*models.AccountSuspension, error) {
	var out *models.AccountSuspension
	err := r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		row, err := q.GetActiveSuspensionByUserID(ctx, userID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotSuspended
			}
			return fmt.Errorf("get active suspension: %w", err)
		}
		out = toSuspension(row)
		return nil
	})
	return out, err
}

func (r *repository) Create(ctx context.Context, s *models.AccountSuspension) error {
	return r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		row, err := q.CreateSuspension(ctx, db.CreateSuspensionParams{
			UserID:         s.UserID,
			Reason:         db.AuthSuspensionReason(s.Reason),
			Details:        store.NullStr(s.Details),
			SuspendedUntil: store.TSPtr(s.SuspendedUntil),
			IsPermanent:    s.IsPermanent,
		})
		if err != nil {
			return fmt.Errorf("create suspension: %w", err)
		}
		*s = *toSuspension(row)
		return nil
	})
}

func (r *repository) DeactivateByUserID(ctx context.Context, userID int64) error {
	return r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		if err := q.DeactivateActiveSuspensions(ctx, userID); err != nil {
			return fmt.Errorf("deactivate suspension: %w", err)
		}
		return nil
	})
}
