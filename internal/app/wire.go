package app

import (
	"os"

	"github.com/isyll/go-grpc-starter/internal/auth"
	grpcserver "github.com/isyll/go-grpc-starter/internal/grpcsvc"
	"github.com/isyll/go-grpc-starter/internal/notifications"
	"github.com/isyll/go-grpc-starter/internal/settings"
	"github.com/isyll/go-grpc-starter/internal/suspension"
	"github.com/isyll/go-grpc-starter/internal/users"
	"github.com/isyll/go-grpc-starter/pkg/locale"
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

	localeBundle, err := locale.New(infra.Config.App)
	if err != nil {
		infra.Logger.Warn("i18n disabled (untranslated error keys)", "error", err)
	}

	authSvc := auth.NewService(
		infra.Config,
		infra.Logger,
		infra.AccessTokenManager,
		infra.CacheManager,
		infra.Store,
		userRepo,
		sessionRepo,
		settingsRepo,
		refreshRepo,
		sender,
		infra.EventBus,
	)
	usersSvc := users.NewService(userRepo, authSvc, infra.EventBus, infra.Storage, infra.Logger)
	settingsSvc := settings.NewService(settingsRepo)
	suspensionSvc := suspension.NewService(suspensionRepo)
	notifSvc := notifications.NewService(tokenRepo, prefRepo, infra.FCM, infra.Logger)

	return grpcserver.Deps{
		Logger:   infra.Logger,
		Config:   infra.Config,
		Tokens:   infra.AccessTokenManager,
		Sessions: sessionRepo,
		Locale:   localeBundle,
		Auth:     grpcserver.NewAuthServer(authSvc, infra.IDEncoder),
		User:     grpcserver.NewUserServer(usersSvc, settingsSvc, notifSvc, infra.IDEncoder),
		Admin:    grpcserver.NewAdminServer(usersSvc, suspensionSvc, infra.EventBus, infra.IDEncoder),
		Health:   grpcserver.NewHealthServer(infra.Store, infra.Cache, a.version()),
	}
}

func (a *App) version() string {
	return a.Infra.Config.App.Info.Version
}
