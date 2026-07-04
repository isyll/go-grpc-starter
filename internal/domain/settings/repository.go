// Package settings owns per-user preferences stored as a JSONB blob.
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/isyll/go-grpc-starter/gen/db"
	"github.com/isyll/go-grpc-starter/internal/models"
	"github.com/isyll/go-grpc-starter/internal/store"
)

type Repository interface {
	GetByUserID(ctx context.Context, userID int64) (*models.Settings, error)
	Create(ctx context.Context, s *models.UserSettings) error
	Update(ctx context.Context, userID int64, s models.Settings) error
}

type repository struct {
	store *store.Store
}

func NewRepository(s *store.Store) Repository {
	return &repository{store: s}
}

func (r *repository) GetByUserID(ctx context.Context, userID int64) (*models.Settings, error) {
	var out *models.Settings
	err := r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		row, err := q.GetUserSettings(ctx, userID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrSettingsNotFound
			}
			return fmt.Errorf("get settings: %w", err)
		}
		var s models.Settings
		if err := json.Unmarshal(row.Settings, &s); err != nil {
			return fmt.Errorf("unmarshal settings: %w", err)
		}
		out = &s
		return nil
	})
	return out, err
}

func (r *repository) Create(ctx context.Context, us *models.UserSettings) error {
	return r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		b, err := json.Marshal(&us.Settings)
		if err != nil {
			return fmt.Errorf("marshal settings: %w", err)
		}
		if err := q.CreateUserSettings(ctx, db.CreateUserSettingsParams{
			UserID:   us.UserID,
			Settings: b,
		}); err != nil {
			return fmt.Errorf("create settings: %w", err)
		}
		return nil
	})
}

func (r *repository) Update(ctx context.Context, userID int64, s models.Settings) error {
	return r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		b, err := json.Marshal(&s)
		if err != nil {
			return fmt.Errorf("marshal settings: %w", err)
		}
		if err := q.UpdateUserSettings(ctx, db.UpdateUserSettingsParams{
			UserID:   userID,
			Settings: b,
		}); err != nil {
			return fmt.Errorf("update settings: %w", err)
		}
		return nil
	})
}
