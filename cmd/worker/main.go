// Command worker runs every background worker in one process: the email
// sender, the push-notification sender, and the event dispatcher (outbox drain
// + async event handlers). Asynq isolates work per queue, so a single binary is
// simpler to operate than three at template stage.
package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"firebase.google.com/go/v4/messaging"
	"github.com/hibiken/asynq"

	"github.com/isyll/go-grpc-starter/internal/app"
	"github.com/isyll/go-grpc-starter/internal/event"
	"github.com/isyll/go-grpc-starter/internal/platform/cache"
	database "github.com/isyll/go-grpc-starter/internal/platform/db"
	"github.com/isyll/go-grpc-starter/internal/store"
	emailworker "github.com/isyll/go-grpc-starter/internal/worker/emails"
	notifworker "github.com/isyll/go-grpc-starter/internal/worker/notifications"
	"github.com/isyll/go-grpc-starter/pkg/config"
	appenv "github.com/isyll/go-grpc-starter/pkg/env"
	"github.com/isyll/go-grpc-starter/pkg/firebase"
	"github.com/isyll/go-grpc-starter/pkg/locale"
	"github.com/isyll/go-grpc-starter/pkg/logger"
)

type startStopper interface {
	Start() error
	Shutdown()
}

func main() {
	env := appenv.InitApp()

	cfg, err := config.LoadAllConfigs()
	if err != nil {
		log.Fatal("failed to load configs:", err)
	}

	logx := logger.New(env)
	logx.Info("starting worker", "env", env)

	pool, err := database.InitPool(cfg.Database, database.RoleAdmin, logx)
	if err != nil {
		logx.Fatal("worker: database init failed", "error", err)
	}
	st := store.New(pool)
	defer st.Pool().Close()

	rdb, err := cache.InitRedis(cfg.Redis)
	if err != nil {
		logx.Fatal("worker: redis init failed", "error", err)
	}
	defer func() { _ = rdb.Close() }()
	cm := cache.NewCacheManager(rdb, cfg.Redis.Cache.Prefix)

	redisAddr, redisPassword := cfg.Redis.Credentials()

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var workers []startStopper

	// Email worker.
	localizer, err := locale.New(cfg.App)
	if err != nil {
		logx.Fatal("worker: i18n init failed", "error", err)
	}
	emailProc := emailworker.NewProcessor(cfg.Email, logx, localizer)
	workers = append(workers, emailworker.NewWorker(redisAddr, redisPassword, emailProc, cfg.Email, logx))

	// Push-notification worker (skipped when FCM is unavailable).
	if fcmClient := initFCM(cfg, logx); fcmClient != nil {
		proc := notifworker.NewProcessor(
			fcmClient,
			notifworker.NewFCMTokenRepository(st),
			notifworker.NewNotificationPreferencesRepository(st),
			notifworker.NewTemplateRepository(st),
			notifworker.NewLogRepository(st),
			cfg.Notifications,
			logx,
		)
		workers = append(workers, notifworker.NewWorker(redisAddr, redisPassword, proc, cfg.Notifications, logx))
	}

	// Event dispatcher: outbox drain + async event handlers.
	asynqClient := asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr, Password: redisPassword})
	defer func() { _ = asynqClient.Close() }()
	dispatcher := event.NewAsynqDispatcher(asynqClient, logx)
	outboxRepo := event.NewOutboxRepository(st, logx)
	bus := event.NewWithOutbox(dispatcher, outboxRepo, logx)
	app.WireEventSubscriptions(bus, &app.EventHandlerDeps{Store: st, CacheManager: cm, Logger: logx})
	workers = append(workers, event.NewWorker(redisAddr, redisPassword, bus, event.DefaultWorkerConfig(), logx))

	for _, w := range workers {
		if err := w.Start(); err != nil {
			logx.Fatal("worker: start failed", "error", err)
		}
	}

	drainInterval := cfg.Events.Outbox.Interval
	if drainInterval <= 0 {
		drainInterval = 5 * time.Second
	}
	go bus.DrainOutbox(rootCtx, drainInterval)
	go runOutboxMetrics(rootCtx, outboxRepo, cfg.Events.Outbox.MetricsInterval, logx)

	logx.Info("worker ready", "redis", redisAddr)
	<-rootCtx.Done()

	logx.Info("worker shutting down")
	for _, w := range workers {
		w.Shutdown()
	}
}

func initFCM(cfg *config.Configs, logx *logger.Logger) *messaging.Client {
	fb, err := firebase.NewClient(cfg.Firebase)
	if err != nil {
		logx.Warn("push worker disabled: firebase init failed", "error", err)
		return nil
	}
	client, err := fb.GetMessagingClient(context.Background())
	if err != nil {
		logx.Warn("push worker disabled: fcm messaging unavailable", "error", err)
		return nil
	}
	return client
}

func runOutboxMetrics(
	ctx context.Context, repo *event.OutboxRepository, interval time.Duration, logx *logger.Logger,
) {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := repo.UpdateMetrics(ctx); err != nil {
				logx.Warn("outbox metrics", "error", err)
			}
		}
	}
}
