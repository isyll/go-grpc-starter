package notifications

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/isyll/go-grpc-starter/gen/db"
	"github.com/isyll/go-grpc-starter/internal/models"
	"github.com/isyll/go-grpc-starter/internal/store"
)

type TokenRepository interface {
	Upsert(ctx context.Context, token *models.FCMToken) error
	ListByUserID(
		ctx context.Context, userID int64,
	) ([]*models.FCMToken, error)
	FindByUserIDAndDeviceID(
		ctx context.Context,
		userID int64,
		deviceID string,
	) (*models.FCMToken, error)
	DeleteByDeviceID(
		ctx context.Context, userID int64, deviceID string,
	) error
}

type PreferencesRepository interface {
	FindByUserID(
		ctx context.Context, userID int64,
	) (*models.NotificationPreferences, error)
	Upsert(
		ctx context.Context,
		prefs *models.NotificationPreferences,
	) error
	Create(
		ctx context.Context,
		prefs *models.NotificationPreferences,
	) error
}

func toFCMToken(r db.AuthFcmToken) *models.FCMToken {
	return &models.FCMToken{
		ID:         r.ID,
		UserID:     r.UserID,
		DeviceID:   r.DeviceID,
		Token:      r.Token,
		Platform:   models.NotificationPlatform(r.Platform),
		AppVersion: store.Str(r.AppVersion),
		IsActive:   r.IsActive,
		LastUsedAt: store.TimePtr(r.LastUsedAt),
		CreatedAt:  store.Time(r.CreatedAt),
		UpdatedAt:  store.Time(r.UpdatedAt),
	}
}

func toPreferences(r db.NotificationsNotificationPreference) *models.NotificationPreferences {
	return &models.NotificationPreferences{
		UserID:            r.UserID,
		Push:              r.Push,
		Email:             r.Email,
		Marketing:         r.Marketing,
		QuietHoursEnabled: r.QuietHoursEnabled,
		QuietHoursStart:   store.TimeOfDayStr(r.QuietHoursStart),
		QuietHoursEnd:     store.TimeOfDayStr(r.QuietHoursEnd),
		Timezone:          r.Timezone,
		CreatedAt:         store.Time(r.CreatedAt),
		UpdatedAt:         store.Time(r.UpdatedAt),
	}
}

type tokenRepository struct {
	store *store.Store
}

func NewTokenRepository(s *store.Store) TokenRepository {
	return &tokenRepository{store: s}
}

func (r *tokenRepository) Upsert(
	ctx context.Context, token *models.FCMToken,
) error {
	return r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		row, err := q.UpsertFCMToken(ctx, db.UpsertFCMTokenParams{
			UserID:     token.UserID,
			DeviceID:   token.DeviceID,
			Token:      token.Token,
			Platform:   db.AuthNotificationPlatform(token.Platform),
			AppVersion: store.NullStr(token.AppVersion),
		})
		if err != nil {
			return fmt.Errorf("upsert FCM token: %w", err)
		}
		*token = *toFCMToken(row)
		return nil
	})
}

func (r *tokenRepository) ListByUserID(
	ctx context.Context, userID int64,
) ([]*models.FCMToken, error) {
	var tokens []*models.FCMToken
	err := r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		rows, err := q.ListFCMTokensByUserID(ctx, userID)
		if err != nil {
			return fmt.Errorf("list FCM tokens for user %d: %w", userID, err)
		}
		tokens = make([]*models.FCMToken, len(rows))
		for i, row := range rows {
			tokens[i] = toFCMToken(row)
		}
		return nil
	})
	return tokens, err
}

func (r *tokenRepository) FindByUserIDAndDeviceID(
	ctx context.Context,
	userID int64,
	deviceID string,
) (*models.FCMToken, error) {
	var out *models.FCMToken
	err := r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		row, err := q.GetFCMTokenByUserAndDevice(ctx, db.GetFCMTokenByUserAndDeviceParams{
			UserID:   userID,
			DeviceID: deviceID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrTokenNotFound
			}
			return fmt.Errorf("find FCM token: %w", err)
		}
		out = toFCMToken(row)
		return nil
	})
	return out, err
}

func (r *tokenRepository) DeleteByDeviceID(
	ctx context.Context, userID int64, deviceID string,
) error {
	return r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		return q.DeleteFCMTokenByDevice(ctx, db.DeleteFCMTokenByDeviceParams{
			UserID:   userID,
			DeviceID: deviceID,
		})
	})
}

type preferencesRepository struct {
	store *store.Store
}

func NewPreferencesRepository(s *store.Store) PreferencesRepository {
	return &preferencesRepository{store: s}
}

func (r *preferencesRepository) FindByUserID(
	ctx context.Context, userID int64,
) (*models.NotificationPreferences, error) {
	var out *models.NotificationPreferences
	err := r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		row, err := q.GetNotificationPreferences(ctx, userID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrPrefNotFound
			}
			return fmt.Errorf("find notification preferences: %w", err)
		}
		out = toPreferences(row)
		return nil
	})
	return out, err
}

func (r *preferencesRepository) Create(
	ctx context.Context,
	prefs *models.NotificationPreferences,
) error {
	return r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		row, err := q.CreateNotificationPreferences(ctx, db.CreateNotificationPreferencesParams{
			UserID:            prefs.UserID,
			Push:              prefs.Push,
			Email:             prefs.Email,
			Marketing:         prefs.Marketing,
			QuietHoursEnabled: prefs.QuietHoursEnabled,
			QuietHoursStart:   store.TimeOfDay(prefs.QuietHoursStart),
			QuietHoursEnd:     store.TimeOfDay(prefs.QuietHoursEnd),
			Timezone:          prefs.Timezone,
		})
		if err != nil {
			return fmt.Errorf("create notification preferences: %w", err)
		}
		*prefs = *toPreferences(row)
		return nil
	})
}

func (r *preferencesRepository) Upsert(
	ctx context.Context,
	prefs *models.NotificationPreferences,
) error {
	return r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		row, err := q.UpsertNotificationPreferences(ctx, db.UpsertNotificationPreferencesParams{
			UserID:            prefs.UserID,
			Push:              prefs.Push,
			Email:             prefs.Email,
			Marketing:         prefs.Marketing,
			QuietHoursEnabled: prefs.QuietHoursEnabled,
			QuietHoursStart:   store.TimeOfDay(prefs.QuietHoursStart),
			QuietHoursEnd:     store.TimeOfDay(prefs.QuietHoursEnd),
			Timezone:          prefs.Timezone,
		})
		if err != nil {
			return fmt.Errorf("upsert notification preferences: %w", err)
		}
		*prefs = *toPreferences(row)
		return nil
	})
}
