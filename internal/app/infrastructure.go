package app

import (
	"time"

	"firebase.google.com/go/v4/messaging"

	"github.com/isyll/go-grpc-starter/internal/events"
	"github.com/isyll/go-grpc-starter/internal/infra/cache"
	"github.com/isyll/go-grpc-starter/internal/store"
	"github.com/isyll/go-grpc-starter/internal/worker/emails"
	"github.com/isyll/go-grpc-starter/internal/worker/notifications"
	"github.com/isyll/go-grpc-starter/pkg/config"
	"github.com/isyll/go-grpc-starter/pkg/idenc"
	"github.com/isyll/go-grpc-starter/pkg/logger"
	apptoken "github.com/isyll/go-grpc-starter/pkg/token"

	"github.com/redis/go-redis/v9"
)

type Infrastructure struct {
	StartTime time.Time

	Store  *store.Store
	Cache  *redis.Client
	Config *config.Configs
	Logger *logger.Logger

	IDEncoder          idenc.IDEncoder
	AccessTokenManager apptoken.AccessTokenManager
	CacheManager       *cache.CacheManager
	FCM                *messaging.Client

	Notifications notifications.Dispatcher
	Emails        emails.Dispatcher

	EventBus           *events.Bus
	EventBusDispatcher *events.AsynqDispatcher
	OutboxRepo         *events.OutboxRepository
}
