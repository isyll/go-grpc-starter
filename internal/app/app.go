// Package app owns the application lifecycle and dependency wiring.
package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"firebase.google.com/go/v4/messaging"
	"github.com/hibiken/asynq"

	"github.com/isyll/go-grpc-starter/internal/events"
	grpcserver "github.com/isyll/go-grpc-starter/internal/grpc"
	"github.com/isyll/go-grpc-starter/internal/infra/cache"
	database "github.com/isyll/go-grpc-starter/internal/infra/db"
	"github.com/isyll/go-grpc-starter/internal/monitor"
	"github.com/isyll/go-grpc-starter/internal/store"
	"github.com/isyll/go-grpc-starter/internal/worker/emails"
	"github.com/isyll/go-grpc-starter/internal/worker/notifications"
	"github.com/isyll/go-grpc-starter/pkg/config"
	appenv "github.com/isyll/go-grpc-starter/pkg/env"
	"github.com/isyll/go-grpc-starter/pkg/firebase"
	"github.com/isyll/go-grpc-starter/pkg/idenc"
	"github.com/isyll/go-grpc-starter/pkg/logger"
	apptoken "github.com/isyll/go-grpc-starter/pkg/token"
)

type App struct {
	StartTime time.Time
	Infra     *Infrastructure

	server   *grpcserver.Server
	listener net.Listener
	obs      *http.Server

	bgCtx    context.Context
	bgCancel context.CancelFunc
}

func New() *App {
	bgCtx, bgCancel := context.WithCancel(context.Background())
	return &App{StartTime: time.Now(), bgCtx: bgCtx, bgCancel: bgCancel}
}

func (a *App) Initialize() error {
	cfgs, err := config.LoadAllConfigs()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	envName := appenv.InitApp()
	logx := logger.New(envName)
	logx.Info("initializing "+cfgs.App.Info.Name, "version", cfgs.App.Info.Version, "env", envName)

	pool, err := database.InitPool(cfgs.Database, database.RoleApp, logx)
	if err != nil {
		return fmt.Errorf("database init: %w", err)
	}
	st := store.New(pool)

	rdb, err := cache.InitRedis(cfgs.Redis)
	if err != nil {
		return fmt.Errorf("redis init: %w", err)
	}

	idCfg := cfgs.Security.IDObfuscation
	idEncoder := idenc.NewSqidsEncoder(idCfg.Alphabet, idCfg.MinLength)
	idenc.SetGlobalEncoder(idEncoder)

	accessTokenManager := apptoken.NewRedisAccessTokenManager(
		rdb, cfgs.Security.Auth.OAT.AccessTokenExpiry,
	)
	cacheManager := cache.NewCacheManager(rdb, cfgs.Redis.Cache.Prefix)

	fcm := a.initFCM(envName, cfgs, logx)
	d := buildDispatchers(cfgs, st, logx)

	a.Infra = &Infrastructure{
		StartTime:          a.StartTime,
		Store:              st,
		Cache:              rdb,
		Config:             cfgs,
		Logger:             logx,
		IDEncoder:          idEncoder,
		AccessTokenManager: accessTokenManager,
		CacheManager:       cacheManager,
		FCM:                fcm,
		Notifications:      d.notif,
		Emails:             d.email,
		EventBus:           d.eventBus,
		EventBusDispatcher: d.eventAsynq,
		OutboxRepo:         d.outboxRepo,
	}
	return nil
}

func (a *App) initFCM(env string, cfgs *config.Configs, logx *logger.Logger) *messaging.Client {
	fb, err := firebase.InitFirebase(env, cfgs, logx)
	if err != nil {
		logx.Warn("firebase disabled (push notifications off)", "error", err)
		return nil
	}
	client, err := fb.GetMessagingClient(context.Background())
	if err != nil {
		logx.Warn("fcm messaging unavailable", "error", err)
		return nil
	}
	return client
}

type dispatcherBundle struct {
	notif      notifications.Dispatcher
	email      emails.Dispatcher
	eventAsynq *events.AsynqDispatcher
	outboxRepo *events.OutboxRepository
	eventBus   *events.Bus
}

func buildDispatchers(cfgs *config.Configs, st *store.Store, logx *logger.Logger) dispatcherBundle {
	addr, password := cfgs.Redis.Credentials()

	asynqClient := asynq.NewClient(asynq.RedisClientOpt{Addr: addr, Password: password})
	eventDispatcher := events.NewAsynqDispatcher(asynqClient, logx)
	outboxRepo := events.NewOutboxRepository(st, logx)

	return dispatcherBundle{
		notif:      notifications.NewDispatcher(addr, password, cfgs.Notifications, logx),
		email:      emails.NewDispatcher(addr, password, cfgs.Email, logx),
		eventAsynq: eventDispatcher,
		outboxRepo: outboxRepo,
		eventBus:   events.NewWithOutbox(eventDispatcher, outboxRepo, logx),
	}
}

func (a *App) Bootstrap() error {
	deps := a.buildGRPCDeps()

	WireEventSubscriptions(a.Infra.EventBus, &EventHandlerDeps{
		Store:        a.Infra.Store,
		CacheManager: a.Infra.CacheManager,
		Logger:       a.Infra.Logger,
	})

	a.startBackground()

	a.server = grpcserver.New(deps)

	addr := a.Infra.Config.App.GetServerAddress()
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	a.listener = lis
	return nil
}

func (a *App) startBackground() {
	if a.Infra.Config.Events.Outbox.DrainOnAPI {
		go a.Infra.EventBus.DrainOutbox(a.bgCtx, a.Infra.Config.Events.Outbox.Interval)
	}

	go func(ctx context.Context) {
		ticker := time.NewTicker(a.Infra.Config.Events.Outbox.MetricsInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := a.Infra.OutboxRepo.UpdateMetrics(ctx); err != nil {
					a.Infra.Logger.Warn("outbox metrics", "error", err)
				}
			}
		}
	}(a.bgCtx)

	addr, password := a.Infra.Config.Redis.Credentials()
	deadMon := monitor.NewDeadQueueMonitor(
		addr, password, 5*time.Minute,
		[]string{"high", "normal", "low", "events:dispatch"},
		a.Infra.Logger,
	)
	go deadMon.Run(a.bgCtx)
}

func (a *App) Start() {
	a.Infra.Logger.Info(
		"gRPC server starting",
		"address", a.listener.Addr().String(),
		"startup", time.Since(a.StartTime).String(),
	)
	a.obs = a.startObservability()
	go func() {
		if err := a.server.Serve(a.listener); err != nil {
			a.Infra.Logger.Fatal("gRPC serve failed", "error", err)
		}
	}()
}

func (a *App) AwaitShutdown() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigChan
	a.Infra.Logger.Info("shutdown signal received", "signal", sig.String())

	a.bgCancel()
	a.server.GracefulStop()

	if a.obs != nil {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.obs.Shutdown(shutCtx)
	}

	if a.Infra.Store != nil {
		a.Infra.Store.Pool().Close()
	}
	if a.Infra.Cache != nil {
		_ = a.Infra.Cache.Close()
	}
	if a.Infra.Notifications != nil {
		_ = a.Infra.Notifications.Close()
	}
	if a.Infra.Emails != nil {
		_ = a.Infra.Emails.Close()
	}
	if a.Infra.EventBusDispatcher != nil {
		_ = a.Infra.EventBusDispatcher.Close()
	}
	a.Infra.Logger.Sync()
	a.Infra.Logger.Info("shutdown complete")
}
