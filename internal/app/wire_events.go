package app

import (
	"fmt"
	"time"

	"github.com/isyll/go-grpc-starter/internal/events"
	"github.com/isyll/go-grpc-starter/internal/events/handlers"
)

func WireEventSubscriptions(bus *events.Bus, deps *EventHandlerDeps) {
	cacheInv := handlers.NewCacheInvalidator(deps.CacheManager, deps.Logger)
	events.Subscribe(bus, cacheInv.OnUserAccountDeleted)

	audit := handlers.NewAuditLogHandler(deps.Store, deps.Logger)
	events.SubscribeAsync(
		bus, audit.OnAuditLogWritten,
		events.WithQueue("high"),
		events.WithUniqueWindow(5*time.Minute),
		events.WithTaskIDFn(func(e events.Event) string {
			if evt, ok := e.(*events.AuditLogWritten); ok {
				return fmt.Sprintf("audit:%s:%s", evt.RequestID, evt.Action)
			}
			return ""
		}),
	)

	attempts := handlers.NewAuthAttemptHandler(deps.Store, deps.Logger)
	events.SubscribeAsync(
		bus, attempts.OnAuthAttemptRecorded,
		events.WithQueue("normal"),
		events.WithUniqueWindow(5*time.Minute),
		events.WithTaskIDFn(func(e events.Event) string {
			if evt, ok := e.(*events.AuthAttemptRecorded); ok {
				return fmt.Sprintf("auth:%s:%s", evt.RequestID, evt.Channel)
			}
			return ""
		}),
	)
}
