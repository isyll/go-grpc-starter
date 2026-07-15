package httpsvc

import (
	"context"
	"strings"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"

	adminv1 "github.com/isyll/go-grpc-starter/gen/admin/v1"
	authv1 "github.com/isyll/go-grpc-starter/gen/auth/v1"
	healthv1 "github.com/isyll/go-grpc-starter/gen/health/v1"
	userv1 "github.com/isyll/go-grpc-starter/gen/user/v1"
)

type registrar func(context.Context, *runtime.ServeMux, *grpc.ClientConn) error

func newGatewayMux(ctx context.Context, conn *grpc.ClientConn) (*runtime.ServeMux, error) {
	mux := runtime.NewServeMux(
		runtime.WithIncomingHeaderMatcher(incomingHeaderMatcher),
		runtime.WithErrorHandler(errorHandler),
	)

	for _, register := range []registrar{
		authv1.RegisterAuthServiceHandler,
		userv1.RegisterUserServiceHandler,
		adminv1.RegisterAdminServiceHandler,
		healthv1.RegisterHealthServiceHandler,
	} {
		if err := register(ctx, mux, conn); err != nil {
			return nil, err
		}
	}
	return mux, nil
}

func incomingHeaderMatcher(key string) (string, bool) {
	switch strings.ToLower(key) {
	case "authorization", "accept-language", "lang", "x-request-id":
		return strings.ToLower(key), true
	default:
		return runtime.DefaultHeaderMatcher(key)
	}
}
