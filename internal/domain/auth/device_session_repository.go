package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/isyll/go-grpc-starter/gen/db"
	apperrors "github.com/isyll/go-grpc-starter/internal/errors"
	"github.com/isyll/go-grpc-starter/internal/models"
	"github.com/isyll/go-grpc-starter/internal/store"
)

type DeviceSessionRepository interface {
	Create(ctx context.Context, session *models.DeviceSession)
	FindByID(ctx context.Context, id int64) (*models.DeviceSession, error)
	FindByUserAndDeviceID(ctx context.Context, userID int64, deviceID string) *models.DeviceSession
	Revoke(ctx context.Context, reason string, id int64) (*models.DeviceSession, error)
	FindActiveDevicesByUser(ctx context.Context, userID int64, inactivityTimeout time.Duration) []models.DeviceSession
	RevokeAllByUserID(ctx context.Context, userID int64, reason string) error
}

type deviceSessionRepository struct {
	store *store.Store
}

func NewDeviceSessionRepository(s *store.Store) DeviceSessionRepository {
	return &deviceSessionRepository{store: s}
}

func toDeviceSession(r db.AuthDeviceSession) *models.DeviceSession {
	return &models.DeviceSession{
		ID:               r.ID,
		UserID:           r.UserID,
		Platform:         r.Platform,
		Manufacturer:     store.Str(r.Manufacturer),
		Model:            store.Str(r.Model),
		Version:          store.Str(r.Version),
		SDK:              store.Str(r.Sdk),
		Brand:            store.Str(r.Brand),
		Hardware:         store.Str(r.Hardware),
		Board:            store.Str(r.Board),
		Device:           store.Str(r.Device),
		Product:          store.Str(r.Product),
		IsPhysicalDevice: store.Bool(r.IsPhysicalDevice),
		Name:             store.Str(r.Name),
		Identifier:       store.Str(r.Identifier),
		DeviceID:         r.DeviceID,
		LastActivity:     store.Time(r.LastActivity),
		IPAddress:        store.Str(r.IpAddress),
		UserAgent:        store.Str(r.UserAgent),
		Location:         store.Str(r.Location),
		RevokedAt:        store.TimePtr(r.RevokedAt),
		RevokedReason:    store.Str(r.RevokedReason),
		CreatedAt:        store.Time(r.CreatedAt),
	}
}

func rowToUser(r db.AuthUser) models.User {
	return models.User{
		ID:              r.ID,
		Email:           r.Email,
		PasswordHash:    r.PasswordHash,
		FirstName:       r.FirstName,
		LastName:        r.LastName,
		Avatar:          r.Avatar,
		Bio:             r.Bio,
		Status:          models.UserStatus(r.Status),
		Role:            models.UserRole(r.Role),
		EmailVerifiedAt: store.TimePtr(r.EmailVerifiedAt),
		LastLoginAt:     store.TimePtr(r.LastLoginAt),
		CreatedAt:       store.Time(r.CreatedAt),
		UpdatedAt:       store.Time(r.UpdatedAt),
		DeletedAt:       store.TimePtr(r.DeletedAt),
	}
}

func (r *deviceSessionRepository) Create(ctx context.Context, session *models.DeviceSession) {
	err := r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		row, err := q.CreateDeviceSession(ctx, db.CreateDeviceSessionParams{
			UserID:       session.UserID,
			Platform:     session.Platform,
			Manufacturer: store.NullStr(session.Manufacturer),
			Model:        store.NullStr(session.Model),
			DeviceID:     session.DeviceID,
			Name:         store.NullStr(session.Name),
			IpAddress:    store.NullStr(session.IPAddress),
			UserAgent:    store.NullStr(session.UserAgent),
		})
		if err != nil {
			return err
		}
		*session = *toDeviceSession(row)
		return nil
	})
	if err != nil {
		panic(fmt.Errorf("create device session: %w", err))
	}
}

func (r *deviceSessionRepository) FindByID(ctx context.Context, id int64) (*models.DeviceSession, error) {
	var out *models.DeviceSession
	err := r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		row, err := q.GetDeviceSessionByID(ctx, id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apperrors.ErrSessionNotFound
			}
			return fmt.Errorf("get session %d: %w", id, err)
		}
		session := toDeviceSession(row)
		user, err := q.GetUserByID(ctx, session.UserID)
		if err != nil {
			return fmt.Errorf("load session user %d: %w", session.UserID, err)
		}
		session.User = rowToUser(user)
		out = session
		return nil
	})
	return out, err
}

func (r *deviceSessionRepository) FindByUserAndDeviceID(
	ctx context.Context, userID int64, deviceID string,
) *models.DeviceSession {
	var out *models.DeviceSession
	err := r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		row, err := q.GetActiveDeviceSessionByUserAndDevice(ctx, db.GetActiveDeviceSessionByUserAndDeviceParams{
			UserID:   userID,
			DeviceID: deviceID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return fmt.Errorf("get user device session: %w", err)
		}
		out = toDeviceSession(row)
		return nil
	})
	if err != nil {
		panic(err)
	}
	return out
}

func (r *deviceSessionRepository) Revoke(
	ctx context.Context, reason string, id int64,
) (*models.DeviceSession, error) {
	var out *models.DeviceSession
	err := r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		row, err := q.RevokeDeviceSession(ctx, db.RevokeDeviceSessionParams{
			ID:            id,
			RevokedReason: store.Ptr(reason),
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apperrors.ErrSessionNotFound
			}
			return fmt.Errorf("revoke session %d: %w", id, err)
		}
		out = toDeviceSession(row)
		return nil
	})
	return out, err
}

func (r *deviceSessionRepository) FindActiveDevicesByUser(
	ctx context.Context, userID int64, inactivityTimeout time.Duration,
) []models.DeviceSession {
	threshold := time.Now().UTC().Add(-inactivityTimeout)
	var sessions []models.DeviceSession
	err := r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		rows, err := q.ListActiveDevicesByUser(ctx, db.ListActiveDevicesByUserParams{
			UserID:       userID,
			LastActivity: store.TS(threshold),
		})
		if err != nil {
			return fmt.Errorf("find active devices: %w", err)
		}
		sessions = make([]models.DeviceSession, len(rows))
		for i, row := range rows {
			sessions[i] = *toDeviceSession(row)
		}
		return nil
	})
	if err != nil {
		panic(err)
	}
	return sessions
}

func (r *deviceSessionRepository) RevokeAllByUserID(
	ctx context.Context, userID int64, reason string,
) error {
	return r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		return q.RevokeAllDeviceSessionsByUser(ctx, db.RevokeAllDeviceSessionsByUserParams{
			UserID:        userID,
			RevokedReason: store.Ptr(reason),
		})
	})
}
