// Package interceptor holds the unary gRPC interceptors: recovery, logging,
// locale resolution, domain-error mapping, request id, and authentication.
package interceptor

import (
	"context"
	"strings"
	"time"

	"github.com/isyll/go-grpc-starter/internal/auth"
	"github.com/isyll/go-grpc-starter/internal/reqctx"
	"github.com/isyll/go-grpc-starter/internal/users"
	"github.com/isyll/go-grpc-starter/pkg/config"
	idgen "github.com/isyll/go-grpc-starter/pkg/id"
	"github.com/isyll/go-grpc-starter/pkg/locale"
	"github.com/isyll/go-grpc-starter/pkg/logger"
	apptoken "github.com/isyll/go-grpc-starter/pkg/token"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// Config carries the dependencies the interceptors need.
type Config struct {
	Tokens   apptoken.AccessTokenManager
	Sessions auth.DeviceSessionRepository
	Cfg      *config.Configs
	Logger   *logger.Logger
	Locale   *locale.Bundle
}

// Set is the configured interceptor chain.
type Set struct {
	tokens        apptoken.AccessTokenManager
	sessions      auth.DeviceSessionRepository
	cfg           *config.Configs
	logger        *logger.Logger
	locale        translator
	localeDefLang string
}

// New builds the interceptor set. i18n is optional: without it, error message
// keys are emitted untranslated.
func New(c Config) *Set {
	s := &Set{
		tokens:        c.Tokens,
		sessions:      c.Sessions,
		cfg:           c.Cfg,
		logger:        c.Logger,
		localeDefLang: "en",
	}
	if c.Locale != nil {
		s.locale = c.Locale
		s.localeDefLang = c.Locale.DefaultLanguage()
	}
	return s
}

// Unary returns the interceptor chain in outermost-to-innermost order. The
// request id is resolved before logging so every log line can carry it.
func (i *Set) Unary() []grpc.UnaryServerInterceptor {
	return []grpc.UnaryServerInterceptor{
		i.recoveryUnary,
		i.requestIDUnary,
		i.loggingUnary,
		i.localeUnary,
		i.errorUnary,
		i.authUnary,
	}
}

var publicMethods = map[string]bool{
	"/health.v1.HealthService/Check":            true,
	"/health.v1.HealthService/Ready":            true,
	"/auth.v1.AuthService/Register":             true,
	"/auth.v1.AuthService/Login":                true,
	"/auth.v1.AuthService/RefreshToken":         true,
	"/auth.v1.AuthService/VerifyEmail":          true,
	"/auth.v1.AuthService/RequestPasswordReset": true,
	"/auth.v1.AuthService/ResetPassword":        true,
}

const adminServicePrefix = "/admin.v1.AdminService/"

// errorUnary is the single error-mapping interceptor: it turns domain errors
// returned by any handler into a localized gRPC status with rich details.
func (i *Set) errorUnary(
	ctx context.Context,
	req any,
	_ *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	resp, err := handler(ctx, req)
	if err != nil {
		return resp, mapError(ctx, err, i.locale)
	}
	return resp, nil
}

// localeUnary resolves the request language from metadata (accept-language
// style) once and stores it in the context for the error mapper and handlers.
func (i *Set) localeUnary(
	ctx context.Context,
	req any,
	_ *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	return handler(reqctx.WithLanguage(ctx, i.resolveLanguage(ctx)), req)
}

func (i *Set) resolveLanguage(ctx context.Context) string {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		for _, key := range []string{"accept-language", "lang"} {
			if v := md.Get(key); len(v) > 0 && v[0] != "" {
				return parseAcceptLanguage(v[0])
			}
		}
	}
	return i.localeDefLang
}

// parseAcceptLanguage takes the highest-priority tag from an Accept-Language
// value ("fr-CA,fr;q=0.9,en;q=0.8" -> "fr").
func parseAcceptLanguage(v string) string {
	first, _, _ := strings.Cut(v, ",")
	first, _, _ = strings.Cut(first, ";")
	first, _, _ = strings.Cut(strings.TrimSpace(first), "-")
	return strings.ToLower(first)
}

func (i *Set) authUnary(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	if publicMethods[info.FullMethod] {
		return handler(ctx, req)
	}

	user, session, err := i.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(info.FullMethod, adminServicePrefix) && !user.IsAdmin() {
		return nil, status.Error(codes.PermissionDenied, "auth.admin_required")
	}

	ctx = reqctx.WithSubject(ctx, reqctx.Subject{
		UserID:    user.ID,
		Role:      reqctx.Role(user.Role),
		SessionID: session.ID,
		DeviceID:  session.DeviceID,
		IsAdmin:   user.IsAdmin(),
	})
	return handler(ctx, req)
}

func (i *Set) authenticate(ctx context.Context) (*users.User, *auth.DeviceSession, error) {
	token, err := BearerToken(ctx)
	if err != nil {
		return nil, nil, err
	}
	claims, err := i.tokens.Validate(ctx, token)
	if err != nil {
		return nil, nil, status.Error(codes.Unauthenticated, "auth.invalid_token")
	}
	session, err := i.sessions.FindByID(ctx, claims.SessionID)
	if err != nil || session.IsRevoked() {
		return nil, nil, status.Error(codes.Unauthenticated, "auth.session_invalid")
	}
	if session.IsInactive(i.cfg.Security.Auth.MaxInactivityTimeout) {
		return nil, nil, status.Error(codes.Unauthenticated, "auth.session_expired")
	}
	if !session.User.IsActive() {
		return nil, nil, status.Error(codes.PermissionDenied, "auth.account_inactive")
	}
	return &session.User, session, nil
}

func BearerToken(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "auth.missing_metadata")
	}
	values := md.Get("authorization")
	if len(values) == 0 {
		return "", status.Error(codes.Unauthenticated, "auth.missing_token")
	}
	parts := strings.SplitN(values[0], " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return "", status.Error(codes.Unauthenticated, "auth.invalid_token_format")
	}
	return parts[1], nil
}

func (i *Set) recoveryUnary(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (resp any, err error) {
	defer func() {
		if r := recover(); r != nil {
			i.logger.Error("panic in handler", "method", info.FullMethod, "panic", r)
			err = status.Error(codes.Internal, "internal error")
		}
	}()
	return handler(ctx, req)
}

func (i *Set) requestIDUnary(
	ctx context.Context,
	req any,
	_ *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	id := incomingRequestID(ctx)
	if id == "" {
		id = idgen.NewUUIDNoDash()
	}
	return handler(reqctx.WithRequestID(ctx, id), req)
}

func (i *Set) loggingUnary(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	start := time.Now()
	resp, err := handler(ctx, req)

	if isHealthMethod(info.FullMethod) {
		return resp, err
	}

	code := status.Code(err)
	fields := []any{
		"method", info.FullMethod,
		"code", code.String(),
		"duration", time.Since(start).String(),
		"request_id", reqctx.RequestIDFromContext(ctx),
	}
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		fields = append(fields, "peer", p.Addr.String())
	}
	if isServerFailure(code) {
		i.logger.Error("grpc", fields...)
	} else {
		i.logger.Info("grpc", fields...)
	}
	return resp, err
}

// isHealthMethod filters liveness probes out of the request log.
func isHealthMethod(fullMethod string) bool {
	return strings.HasPrefix(fullMethod, "/grpc.health.v1.Health/") ||
		strings.HasPrefix(fullMethod, "/health.v1.HealthService/")
}

// isServerFailure reports whether the code indicates a server-side fault
// worth alerting on, as opposed to a client mistake.
func isServerFailure(code codes.Code) bool {
	switch code {
	case codes.Internal, codes.Unknown, codes.DataLoss, codes.Unavailable:
		return true
	default:
		return false
	}
}

func incomingRequestID(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	if v := md.Get("x-request-id"); len(v) > 0 {
		return v[0]
	}
	return ""
}
