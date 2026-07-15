// Package webhook serves raw net/http endpoints for payment provider callbacks
// and OAuth redirects. These bypass the gRPC gateway and the token interceptor:
// their trust comes from a provider signature, and verified requests are handed
// off to the background worker rather than processed inline.
package webhook

import (
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/isyll/go-grpc-starter/internal/worker/webhooks"
	"github.com/isyll/go-grpc-starter/pkg/config"
	"github.com/isyll/go-grpc-starter/pkg/logger"
)

const (
	ProviderWave        = "wave"
	ProviderOrangeMoney = "orange_money"
	ProviderPayDunya    = "paydunya"
	ProviderStripe      = "stripe"
)

const maxBodyBytes = 1 << 20

type Handler struct {
	dispatcher *webhooks.Dispatcher
	verifiers  map[string]Verifier
	webURL     string
	logger     *logger.Logger
}

func NewHandler(
	cfg config.WebhooksConfig,
	dispatcher *webhooks.Dispatcher,
	webURL string,
	logx *logger.Logger,
) *Handler {
	tolerance := cfg.Stripe.Tolerance
	if tolerance <= 0 {
		tolerance = 5 * time.Minute
	}
	orangeHeader := cfg.OrangeMoney.Header
	if orangeHeader == "" {
		orangeHeader = "X-Signature"
	}

	return &Handler{
		dispatcher: dispatcher,
		webURL:     webURL,
		logger:     logx,
		verifiers: map[string]Verifier{
			ProviderWave:        hmacVerifier{header: "Wave-Signature", secret: cfg.Wave.Secret},
			ProviderOrangeMoney: hmacVerifier{header: orangeHeader, secret: cfg.OrangeMoney.Secret},
			ProviderPayDunya:    paydunyaVerifier{masterKey: cfg.PayDunya.MasterKey},
			ProviderStripe:      stripeVerifier{secret: cfg.Stripe.SigningSecret, tolerance: tolerance},
		},
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /webhooks/wave", h.payment(ProviderWave))
	mux.HandleFunc("POST /webhooks/orange-money", h.payment(ProviderOrangeMoney))
	mux.HandleFunc("POST /webhooks/paydunya", h.payment(ProviderPayDunya))
	mux.HandleFunc("POST /webhooks/stripe", h.payment(ProviderStripe))
	mux.HandleFunc("GET /oauth/{provider}/callback", h.oauthCallback)
}

func (h *Handler) payment(provider string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
		if err != nil {
			http.Error(w, "cannot read body", http.StatusBadRequest)
			return
		}

		if err := h.verifiers[provider].Verify(r.Header, body); err != nil {
			h.rejectVerification(w, provider, err)
			return
		}

		ev := webhooks.ReceivedEvent{
			Provider:   provider,
			Payload:    body,
			Headers:    selectedHeaders(r),
			ReceivedAt: time.Now(),
			RequestID:  r.Header.Get("X-Request-Id"),
		}
		if err := h.dispatcher.Enqueue(r.Context(), ev); err != nil {
			h.logger.Error("webhook enqueue failed", "provider", provider, "error", err)
			http.Error(w, "cannot accept webhook", http.StatusBadGateway)
			return
		}

		w.WriteHeader(http.StatusAccepted)
	}
}

func (h *Handler) rejectVerification(w http.ResponseWriter, provider string, err error) {
	switch {
	case errors.Is(err, errNotConfigured):
		h.logger.Warn("webhook secret not configured", "provider", provider)
		http.Error(w, "webhook not configured", http.StatusServiceUnavailable)
	case errors.Is(err, errNoSignature):
		http.Error(w, "missing signature", http.StatusBadRequest)
	default:
		h.logger.Warn("webhook signature rejected", "provider", provider, "error", err)
		http.Error(w, "invalid signature", http.StatusUnauthorized)
	}
}

func selectedHeaders(r *http.Request) map[string]string {
	out := make(map[string]string, 2)
	for _, key := range []string{"Content-Type", "Idempotency-Key"} {
		if v := r.Header.Get(key); v != "" {
			out[key] = v
		}
	}
	return out
}
