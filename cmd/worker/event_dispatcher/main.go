// Command event_dispatcher is the standalone Asynq worker that drains
package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"

	"github.com/isyll/go-grpc-starter/internal/app"
	"github.com/isyll/go-grpc-starter/internal/events"
	"github.com/isyll/go-grpc-starter/internal/infra/cache"
	database "github.com/isyll/go-grpc-starter/internal/infra/db"
	"github.com/isyll/go-grpc-starter/internal/store"
	"github.com/isyll/go-grpc-starter/pkg/config"
	appenv "github.com/isyll/go-grpc-starter/pkg/env"
	"github.com/isyll/go-grpc-starter/pkg/logger"
)

func main() {
	env := appenv.InitApp()

	cfg, err := config.LoadAllConfigs()
	if err != nil {
		log.Fatal("failed to load configs:", err)
	}

	logx := logger.New(env)
	logx.Info("starting event-dispatcher worker", "env", env)

	redisAddr, redisPassword := cfg.Redis.Credentials()

	rdb := redis.NewClient(&redis.Options{Addr: redisAddr, Password: redisPassword})
	defer func() { _ = rdb.Close() }()

	cm := cache.NewCacheManager(rdb, cfg.Redis.Cache.Prefix)

	pool, err := database.InitPool(cfg.Database, database.RoleAdmin, logx)
	if err != nil {
		logx.Fatal("event-dispatcher: database init failed", "error", err)
	}
	st := store.New(pool)
	defer st.Pool().Close()

	outboxRepo := events.NewOutboxRepository(st, logx)

	asynqClient := asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr, Password: redisPassword})
	defer func() { _ = asynqClient.Close() }()
	asyncDisp := events.NewAsynqDispatcher(asynqClient, logx)

	bus := events.NewWithOutbox(asyncDisp, outboxRepo, logx)
	app.WireEventSubscriptions(bus, &app.EventHandlerDeps{
		Store:        st,
		CacheManager: cm,
		Logger:       logx,
	})

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	drainInterval := cfg.Events.Outbox.Interval
	if drainInterval <= 0 {
		drainInterval = 5 * time.Second
	}
	metricsInterval := cfg.Events.Outbox.MetricsInterval
	if metricsInterval <= 0 {
		metricsInterval = 15 * time.Second
	}

	go bus.DrainOutbox(rootCtx, drainInterval)

	go func() {
		ticker := time.NewTicker(metricsInterval)
		defer ticker.Stop()
		for {
			select {
			case <-rootCtx.Done():
				return
			case <-ticker.C:
				if err := outboxRepo.UpdateMetrics(rootCtx); err != nil {
					logx.Warn("outbox metrics", "error", err)
				}
			}
		}
	}()

	worker := events.NewWorker(redisAddr, redisPassword, bus, events.DefaultWorkerConfig(), logx)
	logx.Info("event-dispatcher ready", "redis", redisAddr)
	if err := worker.Run(); err != nil {
		logx.Fatal("event-dispatcher worker stopped", "error", err)
	}
}
