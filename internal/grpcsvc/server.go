package grpcsvc

import (
	"net"
	"time"

	adminv1 "github.com/isyll/go-grpc-starter/gen/admin/v1"
	authv1 "github.com/isyll/go-grpc-starter/gen/auth/v1"
	healthv1 "github.com/isyll/go-grpc-starter/gen/health/v1"
	userv1 "github.com/isyll/go-grpc-starter/gen/user/v1"
	"github.com/isyll/go-grpc-starter/internal/auth"
	"github.com/isyll/go-grpc-starter/internal/interceptor"
	"github.com/isyll/go-grpc-starter/pkg/config"
	"github.com/isyll/go-grpc-starter/pkg/locale"
	"github.com/isyll/go-grpc-starter/pkg/logger"
	apptoken "github.com/isyll/go-grpc-starter/pkg/token"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

type Deps struct {
	Logger   *logger.Logger
	Config   *config.Configs
	Tokens   apptoken.AccessTokenManager
	Sessions auth.DeviceSessionRepository
	Locale   *locale.Bundle
	Auth     *AuthServer
	User     *UserServer
	Admin    *AdminServer
	Health   *HealthServer
}

type Server struct {
	grpc   *grpc.Server
	health *health.Server
	logger *logger.Logger
}

func New(d Deps) *Server {
	ic := interceptor.New(interceptor.Config{
		Tokens:   d.Tokens,
		Sessions: d.Sessions,
		Cfg:      d.Config,
		Logger:   d.Logger,
		Locale:   d.Locale,
	})

	opts := append(
		serverOptions(d.Config),
		grpc.ChainUnaryInterceptor(ic.Unary()...),
	)
	srv := grpc.NewServer(opts...)

	authv1.RegisterAuthServiceServer(srv, d.Auth)
	userv1.RegisterUserServiceServer(srv, d.User)
	adminv1.RegisterAdminServiceServer(srv, d.Admin)
	healthv1.RegisterHealthServiceServer(srv, d.Health)

	hs := health.NewServer()
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(srv, hs)

	if d.Config.App.Server.Reflection {
		reflection.Register(srv)
	}

	return &Server{grpc: srv, health: hs, logger: d.Logger}
}

// serverOptions maps config onto grpc.ServerOptions. Zero config values keep
// the grpc-go defaults.
func serverOptions(cfg *config.Configs) []grpc.ServerOption {
	sc := cfg.App.Server
	var opts []grpc.ServerOption

	if sc.MaxRecvMsgSizeBytes > 0 {
		opts = append(opts, grpc.MaxRecvMsgSize(sc.MaxRecvMsgSizeBytes))
	}
	if sc.MaxSendMsgSizeBytes > 0 {
		opts = append(opts, grpc.MaxSendMsgSize(sc.MaxSendMsgSizeBytes))
	}
	if sc.ConnectionTimeout > 0 {
		opts = append(opts, grpc.ConnectionTimeout(sc.ConnectionTimeout))
	}

	ka := sc.Keepalive
	kp := keepalive.ServerParameters{
		Time:                  ka.Time,
		Timeout:               ka.Timeout,
		MaxConnectionIdle:     ka.MaxConnectionIdle,
		MaxConnectionAge:      ka.MaxConnectionAge,
		MaxConnectionAgeGrace: ka.MaxConnectionAgeGrace,
	}
	if kp != (keepalive.ServerParameters{}) {
		opts = append(opts, grpc.KeepaliveParams(kp))
	}
	if ka.MinClientInterval > 0 {
		opts = append(opts, grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             ka.MinClientInterval,
			PermitWithoutStream: true,
		}))
	}
	return opts
}

func (s *Server) Serve(lis net.Listener) error {
	return s.grpc.Serve(lis)
}

// Shutdown flips the health service to NOT_SERVING so load balancers stop
// routing new work, then drains in-flight RPCs. If draining exceeds grace the
// remaining connections are closed forcibly.
func (s *Server) Shutdown(grace time.Duration) {
	s.health.Shutdown()

	if grace <= 0 {
		grace = 20 * time.Second
	}
	done := make(chan struct{})
	go func() {
		s.grpc.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(grace):
		s.logger.Warn("graceful stop exceeded grace period; forcing stop", "grace", grace.String())
		s.grpc.Stop()
		<-done
	}
}
