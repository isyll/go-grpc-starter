package app

import (
	"github.com/isyll/go-grpc-starter/internal/infra/cache"
	"github.com/isyll/go-grpc-starter/internal/store"
	"github.com/isyll/go-grpc-starter/pkg/logger"
)

type EventHandlerDeps struct {
	Store        *store.Store
	CacheManager *cache.CacheManager
	Logger       *logger.Logger
}
