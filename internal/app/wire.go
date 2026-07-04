package app

import (
	"os"

	"github.com/isyll/go-grpc-starter/internal/domain/auth"
	"github.com/isyll/go-grpc-starter/internal/domain/notifications"
	"github.com/isyll/go-grpc-starter/internal/domain/settings"
	"github.com/isyll/go-grpc-starter/internal/domain/suspension"
	"github.com/isyll/go-grpc-starter/internal/domain/users"
	grpcserver "github.com/isyll/go-grpc-starter/internal/grpc"
)

func (a *App) buildGRPCDeps() grpcserver.Deps {
	infra := a.Infra

	userRepo := users.NewRepository(infra.Store)
	settingsRepo := settings.NewRepository(infra.Store)
	suspensionRepo := suspension.NewRepository(infra.Store)
	sessionRepo := auth.NewDeviceSessionRepository(infra.Store)
	refreshRepo := auth.NewRefreshTokenRepository(infra.Store)
	tokenRepo := notifications.NewTokenRepository(infra.Store)
	prefRepo := notifications.NewPreferencesRepository(infra.Store)

	webURL := os.Getenv("APP_WEB_URL")
	if webURL == "" {
		webURL = "http://localhost:3000"
	}
	sender := newEmailSender(infra.Emails, webURL)

	authSvc := auth.NewService(
		infra.Config,
		infra.Logger,
		infra.AccessTokenManager,
		infra.CacheManager,
		userRepo,
		sessionRepo,
		settingsRepo,
		refreshRepo,
		sender,
		infra.EventBus,
	)
	usersSvc := users.NewService(userRepo, authSvc, infra.EventBus, infra.Logger)
	settingsSvc := settings.NewService(settingsRepo)
	suspensionSvc := suspension.NewService(suspensionRepo)
	notifSvc := notifications.NewService(tokenRepo, prefRepo, infra.FCM, infra.Logger)

	return grpcserver.Deps{
		Logger:   infra.Logger,
		Config:   infra.Config,
		Tokens:   infra.AccessTokenManager,
		Sessions: sessionRepo,
		Auth:     grpcserver.NewAuthServer(authSvc, infra.IDEncoder),
		User:     grpcserver.NewUserServer(usersSvc, settingsSvc, notifSvc, infra.IDEncoder),
		Admin:    grpcserver.NewAdminServer(usersSvc, suspensionSvc, infra.EventBus, infra.IDEncoder),
		Health:   grpcserver.NewHealthServer(infra.Store, infra.Cache, a.version()),
	}
}

func (a *App) version() string {
	return a.Infra.Config.App.Info.Version
}
