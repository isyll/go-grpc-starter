package auth

import (
	"context"
	"crypto/subtle"
	"fmt"

	"github.com/isyll/go-grpc-starter/gen/db"
	"github.com/isyll/go-grpc-starter/internal/store"
)

type RefreshTokenRepository interface {
	Create(ctx context.Context, token *RefreshToken) error
	FindByTokenHash(ctx context.Context, tokenHash string) (*RefreshToken, error)
	RevokeByTokenHash(ctx context.Context, tokenHash, reason string) error
	RevokeBySessionID(ctx context.Context, sessionID int64, reason string) error
	RevokeByTokenFamily(ctx context.Context, tokenFamily, reason string) error
	CleanupExpired(ctx context.Context) (int64, error)
}

type refreshTokenRepository struct {
	store *store.Store
}

func NewRefreshTokenRepository(s *store.Store) RefreshTokenRepository {
	return &refreshTokenRepository{store: s}
}

func toRefreshToken(r db.AuthRefreshToken) *RefreshToken {
	return &RefreshToken{
		ID:            store.UUIDString(r.ID),
		SessionID:     r.SessionID,
		TokenHash:     r.TokenHash,
		TokenPrefix:   r.TokenPrefix,
		TokenFamily:   store.UUIDString(r.TokenFamily),
		ExpiresAt:     store.Time(r.ExpiresAt),
		RevokedAt:     store.TimePtr(r.RevokedAt),
		RevokedReason: store.Str(r.RevokedReason),
		CreatedAt:     store.Time(r.CreatedAt),
	}
}

func (r *refreshTokenRepository) Create(ctx context.Context, token *RefreshToken) error {
	prefix := token.TokenPrefix
	if prefix == "" && len(token.TokenHash) >= 8 {
		prefix = token.TokenHash[:8]
	}
	err := r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		row, err := q.CreateRefreshToken(ctx, db.CreateRefreshTokenParams{
			SessionID:   token.SessionID,
			TokenHash:   token.TokenHash,
			TokenPrefix: prefix,
			TokenFamily: store.ParseUUID(token.TokenFamily),
			ExpiresAt:   store.TS(token.ExpiresAt),
		})
		if err != nil {
			return err
		}
		*token = *toRefreshToken(row)
		return nil
	})
	if err != nil {
		return fmt.Errorf("create refresh token: %w", err)
	}
	return nil
}

func (r *refreshTokenRepository) FindByTokenHash(
	ctx context.Context, tokenHash string,
) (*RefreshToken, error) {
	if len(tokenHash) < 8 {
		return nil, ErrInvalidToken
	}
	var out *RefreshToken
	err := r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		rows, err := q.ListRefreshTokensByPrefix(ctx, tokenHash[:8])
		if err != nil {
			return fmt.Errorf("find refresh token: %w", err)
		}
		for _, row := range rows {
			if subtle.ConstantTimeCompare([]byte(row.TokenHash), []byte(tokenHash)) != 1 {
				continue
			}
			record := toRefreshToken(row)
			sessionRow, err := q.GetDeviceSessionByID(ctx, record.SessionID)
			if err != nil {
				return fmt.Errorf("load token session: %w", err)
			}
			session := toDeviceSession(sessionRow)
			userRow, err := q.GetUserByID(ctx, session.UserID)
			if err != nil {
				return fmt.Errorf("load token user: %w", err)
			}
			session.User = rowToUser(userRow)
			record.Session = *session
			out = record
			return nil
		}
		return ErrInvalidToken
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *refreshTokenRepository) RevokeByTokenHash(ctx context.Context, tokenHash, reason string) error {
	return r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		return q.RevokeRefreshTokenByHash(ctx, db.RevokeRefreshTokenByHashParams{
			TokenHash:     tokenHash,
			RevokedReason: store.Ptr(reason),
		})
	})
}

func (r *refreshTokenRepository) RevokeBySessionID(ctx context.Context, sessionID int64, reason string) error {
	return r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		return q.RevokeRefreshTokensBySession(ctx, db.RevokeRefreshTokensBySessionParams{
			SessionID:     sessionID,
			RevokedReason: store.Ptr(reason),
		})
	})
}

func (r *refreshTokenRepository) RevokeByTokenFamily(ctx context.Context, tokenFamily, reason string) error {
	return r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		return q.RevokeRefreshTokensByFamily(ctx, db.RevokeRefreshTokensByFamilyParams{
			TokenFamily:   store.ParseUUID(tokenFamily),
			RevokedReason: store.Ptr(reason),
		})
	})
}

func (r *refreshTokenRepository) CleanupExpired(ctx context.Context) (int64, error) {
	var n int64
	err := r.store.Run(ctx, func(ctx context.Context, q *db.Queries) error {
		var err error
		n, err = q.DeleteExpiredRefreshTokens(ctx)
		return err
	})
	return n, err
}
