package auth

import (
	"context"
	"strings"
	"time"

	"github.com/isyll/go-grpc-starter/internal/event"
	"github.com/isyll/go-grpc-starter/internal/platform/cache"
	"github.com/isyll/go-grpc-starter/internal/settings"
	"github.com/isyll/go-grpc-starter/internal/users"
	"github.com/isyll/go-grpc-starter/pkg/config"
	"github.com/isyll/go-grpc-starter/pkg/id"
	"github.com/isyll/go-grpc-starter/pkg/logger"
	apptoken "github.com/isyll/go-grpc-starter/pkg/token"
)

var (
	verifyTokenTTL = cache.CacheOptions{TTL: 24 * time.Hour}
	resetTokenTTL  = cache.CacheOptions{TTL: 1 * time.Hour}
)

type Service struct {
	cfg          *config.Configs
	logger       *logger.Logger
	atManager    apptoken.AccessTokenManager
	cacheManager *cache.CacheManager
	tx           TxRunner
	users        UserStore
	sessions     DeviceSessionRepository
	settings     SettingsStore
	refresh      RefreshTokenRepository
	email        EmailSender
	bus          *event.Bus
	hasher       passwordHasher
}

func NewService(
	cfg *config.Configs,
	logx *logger.Logger,
	atManager apptoken.AccessTokenManager,
	cacheManager *cache.CacheManager,
	tx TxRunner,
	users UserStore,
	sessions DeviceSessionRepository,
	settings SettingsStore,
	refresh RefreshTokenRepository,
	email EmailSender,
	bus *event.Bus,
) *Service {
	ph := cfg.Security.PasswordHash
	return &Service{
		cfg:          cfg,
		logger:       logx,
		atManager:    atManager,
		cacheManager: cacheManager,
		tx:           tx,
		users:        users,
		sessions:     sessions,
		settings:     settings,
		refresh:      refresh,
		email:        email,
		bus:          bus,
		hasher: newPasswordHasher(
			ph.Memory, ph.Iterations, ph.Parallelism, ph.SaltLength, ph.KeyLength,
		),
	}
}

type tokenData struct {
	UserID int64 `json:"user_id"`
}

func (s *Service) Register(ctx context.Context, in RegisterInput) (*TokenPair, error) {
	if err := validatePassword(in.Password); err != nil {
		return nil, err
	}
	email := normalizeEmail(in.Email)
	if s.users.ExistsByEmail(ctx, email) {
		return nil, ErrEmailExists
	}

	hash, err := s.hasher.hash(in.Password)
	if err != nil {
		return nil, err
	}
	user := &users.User{
		Email:        email,
		PasswordHash: hash,
		FirstName:    in.FirstName,
		LastName:     in.LastName,
		Status:       users.UserStatusActive,
		Role:         users.UserRoleUser,
	}

	defaults := settings.DefaultSettings()
	var tokens *TokenPair
	err = s.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := s.users.Create(ctx, user); err != nil {
			return err
		}
		if err := s.settings.Create(ctx, &settings.UserSettings{
			UserID:   user.ID,
			Settings: defaults,
		}); err != nil {
			return err
		}
		tokens, err = s.createSessionAndTokens(ctx, user, in.Device, &defaults)
		return err
	})
	if err != nil {
		return nil, err
	}

	s.sendVerification(ctx, user)

	return tokens, nil
}

func (s *Service) Login(ctx context.Context, in LoginInput) (*TokenPair, error) {
	email := normalizeEmail(in.Email)
	user, err := s.users.FindByEmail(ctx, email)
	if err != nil {
		s.recordAttempt(ctx, email, 0, "login", "not_found")
		return nil, ErrInvalidCredentials
	}
	if !verifyPassword(user.PasswordHash, in.Password) {
		s.recordAttempt(ctx, email, user.ID, "login", "wrong_password")
		return nil, ErrInvalidCredentials
	}
	if !user.IsActive() {
		s.recordAttempt(ctx, email, user.ID, "login", "blocked")
		return nil, ErrAccountInactive
	}

	settings, _ := s.settings.GetByUserID(ctx, user.ID)
	tokens, err := s.createSessionAndTokens(ctx, user, in.Device, settings)
	if err != nil {
		return nil, err
	}
	if err := s.users.UpdateLastLogin(ctx, user.ID); err != nil {
		s.logger.Warn("update last login failed", "error", err, "user_id", user.ID)
	}
	s.recordAttempt(ctx, email, user.ID, "login", "success")
	return tokens, nil
}

func (s *Service) VerifyEmail(ctx context.Context, token string) error {
	var data tokenData
	found, err := s.cacheManager.Get(ctx, cache.VerificationTokenKey(token), &data)
	if err != nil || !found {
		return ErrInvalidVerificationToken
	}
	if err := s.users.MarkEmailVerified(ctx, data.UserID); err != nil {
		return err
	}
	_ = s.cacheManager.Delete(ctx, cache.VerificationTokenKey(token))
	return nil
}

func (s *Service) ResendVerification(ctx context.Context, userID int64) error {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return err
	}
	if user.IsEmailVerified() {
		return nil
	}
	s.sendVerification(ctx, user)
	return nil
}

func (s *Service) RequestPasswordReset(ctx context.Context, email string) error {
	email = normalizeEmail(email)
	user, err := s.users.FindByEmail(ctx, email)
	if err != nil {
		return nil
	}
	token := id.NewUUIDNoDash()
	if err := s.cacheManager.Set(
		ctx, cache.PasswordResetKey(token), tokenData{UserID: user.ID}, resetTokenTTL,
	); err != nil {
		return err
	}
	if err := s.email.SendPasswordResetEmail(ctx, user.Email, token); err != nil {
		s.logger.Warn("send reset email failed", "error", err, "user_id", user.ID)
	}
	return nil
}

func (s *Service) ResetPassword(ctx context.Context, token, newPassword string) error {
	if err := validatePassword(newPassword); err != nil {
		return err
	}
	var data tokenData
	found, err := s.cacheManager.Get(ctx, cache.PasswordResetKey(token), &data)
	if err != nil || !found {
		return ErrInvalidResetToken
	}
	hash, err := s.hasher.hash(newPassword)
	if err != nil {
		return err
	}
	if err := s.users.UpdatePasswordHash(ctx, data.UserID, hash); err != nil {
		return err
	}
	_ = s.cacheManager.Delete(ctx, cache.PasswordResetKey(token))
	if err := s.sessions.RevokeAllByUserID(ctx, data.UserID, "password_reset"); err != nil {
		s.logger.Warn("revoke sessions after reset failed", "error", err)
	}
	return nil
}

func (s *Service) ChangePassword(ctx context.Context, userID int64, current, next string) error {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return err
	}
	if !verifyPassword(user.PasswordHash, current) {
		return ErrIncorrectPassword
	}
	if err := validatePassword(next); err != nil {
		return err
	}
	hash, err := s.hasher.hash(next)
	if err != nil {
		return err
	}
	return s.users.UpdatePasswordHash(ctx, userID, hash)
}

func (s *Service) sendVerification(ctx context.Context, user *users.User) {
	token := id.NewUUIDNoDash()
	if err := s.cacheManager.Set(
		ctx, cache.VerificationTokenKey(token), tokenData{UserID: user.ID}, verifyTokenTTL,
	); err != nil {
		s.logger.Warn("store verification token failed", "error", err)
		return
	}
	if err := s.email.SendVerificationEmail(ctx, user.Email, token); err != nil {
		s.logger.Warn("send verification email failed", "error", err, "user_id", user.ID)
	}
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
