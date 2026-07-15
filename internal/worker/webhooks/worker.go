package webhooks

import (
	"context"
	"fmt"
	"time"

	"github.com/hibiken/asynq"

	"github.com/isyll/go-grpc-starter/pkg/logger"
)

const defaultConcurrency = 5

type Worker struct {
	server *asynq.Server
	mux    *asynq.ServeMux
	proc   *Processor
	logger *logger.Logger
}

func NewWorker(redisAddr, redisPassword string, concurrency int, logx *logger.Logger) *Worker {
	if concurrency <= 0 {
		concurrency = defaultConcurrency
	}
	server := asynq.NewServer(
		asynq.RedisClientOpt{Addr: redisAddr, Password: redisPassword},
		asynq.Config{
			Concurrency:     concurrency,
			ShutdownTimeout: 30 * time.Second,
			Queues:          map[string]int{queueWebhooks: 1},
			ErrorHandler: asynq.ErrorHandlerFunc(
				func(_ context.Context, t *asynq.Task, err error) {
					logx.Error("webhook task failed", "type", t.Type(), "error", err)
				},
			),
			Logger: &asynqLogger{logger: logx},
		},
	)
	return &Worker{
		server: server,
		mux:    asynq.NewServeMux(),
		proc:   NewProcessor(logx),
		logger: logx,
	}
}

func (w *Worker) Start() error {
	w.mux.HandleFunc(TaskWebhookReceived, w.proc.ProcessTask)
	w.logger.Info("webhook worker starting")
	return w.server.Start(w.mux)
}

func (w *Worker) Shutdown() {
	w.server.Shutdown()
}

type asynqLogger struct {
	logger *logger.Logger
}

func (l *asynqLogger) Debug(args ...any) { l.logger.Debug(fmt.Sprint(args...)) }
func (l *asynqLogger) Info(args ...any)  { l.logger.Info(fmt.Sprint(args...)) }
func (l *asynqLogger) Warn(args ...any)  { l.logger.Warn(fmt.Sprint(args...)) }
func (l *asynqLogger) Error(args ...any) { l.logger.Error(fmt.Sprint(args...)) }
func (l *asynqLogger) Fatal(args ...any) { l.logger.Fatal(fmt.Sprint(args...)) }
