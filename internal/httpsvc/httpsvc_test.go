package httpsvc

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/isyll/go-grpc-starter/pkg/config"
)

func TestErrorHandlerEnvelope(t *testing.T) {
	st := status.New(codes.InvalidArgument, "raw message")
	st, err := st.WithDetails(
		&errdetails.ErrorInfo{Reason: "auth.invalid_token", Domain: "go-grpc-starter"},
		&errdetails.LocalizedMessage{Locale: "en", Message: "The token is invalid."},
		&errdetails.BadRequest{FieldViolations: []*errdetails.BadRequest_FieldViolation{
			{Field: "email", Description: "must be a valid email"},
		}},
	)
	if err != nil {
		t.Fatalf("with details: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/users/me", nil)
	req.Header.Set("X-Request-Id", "req-123")

	errorHandler(req.Context(), nil, nil, rec, req, st.Err())

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type: want application/json, got %q", ct)
	}

	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if env.Error.Code != "auth.invalid_token" {
		t.Errorf("code: want app code, got %q", env.Error.Code)
	}
	if env.Error.Status != "InvalidArgument" {
		t.Errorf("status: got %q", env.Error.Status)
	}
	if env.Error.Message != "The token is invalid." {
		t.Errorf("message: want localized, got %q", env.Error.Message)
	}
	if env.Error.RequestID != "req-123" {
		t.Errorf("request_id: got %q", env.Error.RequestID)
	}
	if len(env.Error.Fields) != 1 || env.Error.Fields[0].Field != "email" {
		t.Errorf("fields: got %+v", env.Error.Fields)
	}
}

func TestCORSPreflight(t *testing.T) {
	cfg := config.CORSConfig{
		AllowedOrigins:   "http://localhost:3000",
		AllowedMethods:   "GET,POST,OPTIONS",
		AllowedHeaders:   "Authorization,Content-Type",
		AllowCredentials: true,
		MaxAge:           time.Hour,
	}
	handler := withCORS(http.NotFoundHandler(), cfg)

	req := httptest.NewRequest(http.MethodOptions, "/v1/users/me", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "GET")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status: want 204, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Errorf("allow-origin: got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("allow-credentials: got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("allow-methods: empty")
	}
}

func TestCORSDisallowedOrigin(t *testing.T) {
	cfg := config.CORSConfig{AllowedOrigins: "http://localhost:3000"}
	handler := withCORS(http.NotFoundHandler(), cfg)

	req := httptest.NewRequest(http.MethodGet, "/v1/users/me", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("allow-origin: want empty for disallowed origin, got %q", got)
	}
}
