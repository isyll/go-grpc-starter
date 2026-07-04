package grpcsvc

import (
	"context"
	"strings"
	"time"

	"github.com/isyll/go-grpc-starter/internal/auth"
	"github.com/isyll/go-grpc-starter/internal/reqctx"
	"github.com/isyll/go-grpc-starter/internal/users"
	"github.com/isyll/go-grpc-starter/pkg/config"
	idgen "github.com/isyll/go-grpc-starter/pkg/id"
	"github.com/isyll/go-grpc-starter/pkg/logger"
	apptoken "github.com/isyll/go-grpc-starter/pkg/token"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var publicMethods = map[string]bool{
	"/api.v1.HealthService/Check":              true,
	"/api.v1.HealthService/Ready":              true,
	"/api.v1.AuthService/Register":             true,
	"/api.v1.AuthService/Login":                true,
	"/api.v1.AuthService/RefreshToken":         true,
	"/api.v1.AuthService/VerifyEmail":          true,
	"/api.v1.AuthService/RequestPasswordReset": true,
	"/api.v1.AuthService/ResetPassword":        true,
}

const adminServicePrefix = "/api.v1.AdminService/"

type interceptors struct {
	tokens        apptoken.AccessTokenManager
	sessions      auth.DeviceSessionRepository
	cfg           *config.Configs
	logger        *logger.Logger
	locale        translator
	localeDefLang string
}

// errorUnary is the single error-mapping interceptor: it turns domain errors
// returned by any handler into a localized gRPC status with rich details.
func (i *interceptors) errorUnary(
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
func (i *interceptors) localeUnary(
	ctx context.Context,
	req any,
	_ *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	return handler(reqctx.WithLanguage(ctx, i.resolveLanguage(ctx)), req)
}

func (i *interceptors) resolveLanguage(ctx context.Context) string {
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

func (i *interceptors) authUnary(
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

func (i *interceptors) authenticate(ctx context.Context) (*users.User, *auth.DeviceSession, error) {
	token, err := bearerToken(ctx)
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

func bearerToken(ctx context.Context) (string, error) {
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

func (i *interceptors) recoveryUnary(
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

func (i *interceptors) requestIDUnary(
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

func (i *interceptors) loggingUnary(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	start := time.Now()
	resp, err := handler(ctx, req)
	i.logger.Info(
		"grpc",
		"method", info.FullMethod,
		"code", status.Code(err).String(),
		"duration", time.Since(start).String(),
	)
	return resp, err
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
