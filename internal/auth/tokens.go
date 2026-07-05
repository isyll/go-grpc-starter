package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/isyll/go-grpc-starter/internal/event"
	"github.com/isyll/go-grpc-starter/internal/reqctx"
	"github.com/isyll/go-grpc-starter/internal/settings"
	"github.com/isyll/go-grpc-starter/internal/users"
	"github.com/isyll/go-grpc-starter/pkg/id"
	apptoken "github.com/isyll/go-grpc-starter/pkg/token"
)

func (s *Service) RefreshTokens(
	ctx context.Context,
	refreshToken string,
) (*TokenPair, error) {
	tokenHash := hashToken(refreshToken)

	record, err := s.refresh.FindByTokenHash(ctx, tokenHash)
	if err != nil {
		s.recordAttempt(ctx, "", 0, "refresh", "not_found")
		return nil, ErrInvalidToken
	}

	if record.IsRevoked() {
		s.logger.Warn(
			"revoked refresh token reused; revoking family",
			"session_id", record.Session.ID,
			"token_family", record.TokenFamily,
		)
		_ = s.refresh.RevokeByTokenFamily(ctx, record.TokenFamily, "token_reuse")
		_, _ = s.sessions.Revoke(ctx, "token_reuse", record.Session.ID)
		s.recordAttempt(ctx, record.Session.User.Email, record.Session.UserID, "refresh", "blocked")
		return nil, ErrTokenRevoked
	}

	if record.IsExpired() {
		s.recordAttempt(ctx, record.Session.User.Email, record.Session.UserID, "refresh", "not_found")
		return nil, ErrInvalidToken
	}

	session := &record.Session
	if !session.IsValid(s.cfg.Security.Auth.MaxInactivityTimeout) {
		return nil, ErrSessionNotFound
	}

	access, rawRefresh, newHash, err := s.issueTokenPair(ctx, session)
	if err != nil {
		return nil, err
	}
	// Rotate atomically: the old token is revoked and the replacement created
	// in one transaction, so a crash in between cannot strand the session.
	err = s.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := s.refresh.RevokeByTokenHash(ctx, tokenHash, "rotated"); err != nil {
			return err
		}
		return s.refresh.Create(ctx, &RefreshToken{
			SessionID:   session.ID,
			TokenHash:   newHash,
			TokenFamily: record.TokenFamily,
			ExpiresAt:   time.Now().UTC().Add(s.cfg.Security.Auth.OAT.RefreshTokenExpiry),
		})
	})
	if err != nil {
		return nil, err
	}

	settings, _ := s.settings.GetByUserID(ctx, session.UserID)
	s.recordAttempt(ctx, session.User.Email, session.UserID, "refresh", "success")

	return &TokenPair{
		AccessToken:  access,
		RefreshToken: rawRefresh,
		ExpiresIn:    int64(s.cfg.Security.Auth.OAT.AccessTokenExpiry.Seconds()),
		User:         &session.User,
		Settings:     settings,
	}, nil
}

func (s *Service) generateTokenPair(
	ctx context.Context,
	user *users.User,
	session *DeviceSession,
	settings *settings.Settings,
) (*TokenPair, error) {
	access, rawRefresh, tokenHash, err := s.issueTokenPair(ctx, session)
	if err != nil {
		return nil, err
	}
	if err := s.refresh.Create(ctx, &RefreshToken{
		SessionID:   session.ID,
		TokenHash:   tokenHash,
		TokenFamily: id.NewUUIDNoDash(),
		ExpiresAt:   time.Now().UTC().Add(s.cfg.Security.Auth.OAT.RefreshTokenExpiry),
	}); err != nil {
		return nil, err
	}
	return &TokenPair{
		AccessToken:  access,
		RefreshToken: rawRefresh,
		ExpiresIn:    int64(s.cfg.Security.Auth.OAT.AccessTokenExpiry.Seconds()),
		User:         user,
		Settings:     settings,
	}, nil
}

func (s *Service) issueTokenPair(
	ctx context.Context,
	session *DeviceSession,
) (accessToken, rawRefresh, tokenHash string, err error) {
	accessToken, err = s.atManager.Generate(ctx, session.ID, session.UserID, session.DeviceID)
	if err != nil {
		return "", "", "", fmt.Errorf("generate access token: %w", err)
	}
	rawRefresh, tokenHash, err = apptoken.GenerateRefreshToken()
	if err != nil {
		return "", "", "", fmt.Errorf("generate refresh token: %w", err)
	}
	return accessToken, rawRefresh, tokenHash, nil
}

func (s *Service) recordAttempt(
	ctx context.Context,
	email string,
	userID int64,
	channel, outcome string,
) {
	if err := s.bus.Publish(ctx, &event.AuthAttemptRecorded{
		Email:      email,
		UserID:     userID,
		Channel:    channel,
		Outcome:    outcome,
		RequestID:  reqctx.RequestIDFromContext(ctx),
		OccurredAt: time.Now().UTC(),
	}); err != nil {
		s.logger.Warn("auth attempt publish failed", "error", err, "channel", channel)
	}
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
