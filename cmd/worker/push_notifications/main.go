// Command push_notifications is the standalone Asynq worker
package main

import (
	"context"
	"fmt"
	"log"

	database "github.com/isyll/go-grpc-starter/internal/infra/db"
	"github.com/isyll/go-grpc-starter/internal/store"
	notifWorker "github.com/isyll/go-grpc-starter/internal/worker/notifications"
	"github.com/isyll/go-grpc-starter/pkg/config"
	appenv "github.com/isyll/go-grpc-starter/pkg/env"
	"github.com/isyll/go-grpc-starter/pkg/firebase"
	"github.com/isyll/go-grpc-starter/pkg/logger"
)

func main() {
	env := appenv.InitApp()

	cfg, err := config.LoadAllConfigs()
	if err != nil {
		log.Fatal("failed to load configurations:", err)
	}

	logx := logger.New(env)

	logx.Info("Starting push notification worker", "env", env)

	pool, err := database.InitPool(
		cfg.Database, database.RoleAdmin, logx,
	)
	if err != nil {
		logx.Fatal("Failed to initialize database", "error", err)
	}
	st := store.New(pool)
	defer st.Pool().Close()

	firebaseClient, err := firebase.InitFirebase(env, cfg, logx)
	if err != nil {
		logx.Fatal("Failed to initialize Firebase client", "error", err)
	}

	fcmClient, err := firebaseClient.GetMessagingClient(
		context.Background(),
	)
	if err != nil {
		logx.Fatal("Failed to get FCM messaging client", "error", err)
	}

	fcmTokenRepo := notifWorker.NewFCMTokenRepository(st)
	preferencesRepo := notifWorker.NewNotificationPreferencesRepository(
		st,
	)
	templateRepo := notifWorker.NewTemplateRepository(st)
	logRepo := notifWorker.NewLogRepository(st)

	redisAddr := fmt.Sprintf(
		"%s:%d",
		cfg.Redis.Connection.Host,
		cfg.Redis.Connection.Port,
	)
	redisPassword := cfg.Redis.Connection.Password

	processor := notifWorker.NewProcessor(
		fcmClient,
		fcmTokenRepo,
		preferencesRepo,
		templateRepo,
		logRepo,
		cfg.Notifications,
		logx,
	)

	worker := notifWorker.NewWorker(
		redisAddr,
		redisPassword,
		processor,
		cfg.Notifications,
		logx,
	)

	logx.Info("Push notification worker starting",
		"redis", redisAddr,
		"concurrency", cfg.Notifications.Worker.Concurrency,
	)

	if err := worker.Run(); err != nil {
		logx.Fatal("Worker failed", "error", err)
	}

	logx.Info("Push notification worker stopped")
}
