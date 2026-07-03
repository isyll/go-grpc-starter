// Package logger wraps zap with the backend logging conventions.
package logger

import (
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Logger struct {
	appLogger *zap.SugaredLogger
	buf       *zapcore.BufferedWriteSyncer
}

func New(env string) *Logger {
	var core zapcore.Core
	var buf *zapcore.BufferedWriteSyncer

	if env == "production" {
		encoderCfg := zap.NewProductionEncoderConfig()
		encoder := zapcore.NewJSONEncoder(encoderCfg)

		buf = &zapcore.BufferedWriteSyncer{
			WS:            zapcore.AddSync(os.Stdout),
			Size:          256 * 1024,
			FlushInterval: time.Second,
		}

		core = zapcore.NewCore(encoder, buf, zapcore.InfoLevel)
	} else {
		encoderCfg := zap.NewDevelopmentEncoderConfig()
		encoderCfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
		encoderCfg.EncodeTime = zapcore.TimeEncoderOfLayout(
			"2006-01-02 15:04:05")
		encoderCfg.EncodeCaller = zapcore.ShortCallerEncoder
		encoderCfg.ConsoleSeparator = " "
		encoder := zapcore.NewConsoleEncoder(encoderCfg)

		core = zapcore.NewCore(
			encoder,
			zapcore.AddSync(os.Stdout),
			zapcore.DebugLevel,
		)
	}

	logger := zap.New(core,
		zap.AddCaller(),
		zap.AddCallerSkip(1),
	)

	return &Logger{appLogger: logger.Sugar(), buf: buf}
}

func (l *Logger) Info(msg string, keysAndValues ...any) {
	l.appLogger.Infow(msg, keysAndValues...)
}

func (l *Logger) Error(msg string, keysAndValues ...any) {
	l.appLogger.Errorw(msg, keysAndValues...)
}

func (l *Logger) Debug(msg string, keysAndValues ...any) {
	l.appLogger.Debugw(msg, keysAndValues...)
}

func (l *Logger) Warn(msg string, keysAndValues ...any) {
	l.appLogger.Warnw(msg, keysAndValues...)
}

func (l *Logger) Fatal(msg string, keysAndValues ...any) {
	l.appLogger.Fatalw(msg, keysAndValues...)
}

func (l *Logger) Printf(format string, args ...any) {
	l.appLogger.Infof(format, args...)
}

func (l *Logger) Sync() {
	if l.buf != nil {
		_ = l.buf.Sync()
	}
	if l.appLogger != nil {
		_ = l.appLogger.Sync()
	}
}
