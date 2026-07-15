// Package httpsvc serves the HTTP surface that runs alongside the gRPC server:
// the grpc-gateway REST/JSON mirror and the raw webhook endpoints, on a
// listener separate from gRPC. The gateway proxies to the in-process gRPC
// server over loopback, so authentication, validation, and error mapping run
// once, in the gRPC interceptors.
package httpsvc

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/isyll/go-grpc-starter/internal/webhook"
	"github.com/isyll/go-grpc-starter/pkg/config"
	"github.com/isyll/go-grpc-starter/pkg/logger"
)

type Deps struct {
	Config  *config.Configs
	Logger  *logger.Logger
	Webhook *webhook.Handler
}

type Server struct {
	http   *http.Server
	conn   *grpc.ClientConn
	logger *logger.Logger
	addr   string
}

func New(d Deps) (*Server, error) {
	target := "localhost:" + d.Config.App.Server.Port
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("httpsvc: dial gRPC: %w", err)
	}

	mux, err := newGatewayMux(context.Background(), conn)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("httpsvc: register gateway: %w", err)
	}

	root := http.NewServeMux()
	d.Webhook.RegisterRoutes(root)
	root.Handle("/", mux)

	srv := &http.Server{
		Addr:              d.Config.HTTP.ListenAddr,
		Handler:           withCORS(root, d.Config.HTTP.CORS),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return &Server{http: srv, conn: conn, logger: d.Logger, addr: d.Config.HTTP.ListenAddr}, nil
}

func (s *Server) Addr() string { return s.addr }

func (s *Server) Serve() error {
	return s.http.ListenAndServe()
}

func (s *Server) Shutdown(grace time.Duration) {
	if grace <= 0 {
		grace = 20 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	_ = s.http.Shutdown(ctx)
	_ = s.conn.Close()
}
