package webhook

import (
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/isyll/go-grpc-starter/internal/worker/webhooks"
)

func (h *Handler) oauthCallback(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	q := r.URL.Query()

	if providerErr := q.Get("error"); providerErr != "" {
		h.redirectResult(w, r, provider, "denied")
		return
	}

	code := q.Get("code")
	state := q.Get("state")
	if code == "" || state == "" {
		http.Error(w, "missing code or state", http.StatusBadRequest)
		return
	}
	if !validState(r, state) {
		http.Error(w, "invalid state", http.StatusUnauthorized)
		return
	}

	payload, _ := json.Marshal(map[string]string{"code": code, "state": state})
	ev := webhooks.ReceivedEvent{
		Provider:   "oauth:" + provider,
		Payload:    payload,
		ReceivedAt: time.Now(),
		RequestID:  r.Header.Get("X-Request-Id"),
	}
	if err := h.dispatcher.Enqueue(r.Context(), ev); err != nil {
		h.logger.Error("oauth enqueue failed", "provider", provider, "error", err)
		http.Error(w, "cannot complete sign-in", http.StatusBadGateway)
		return
	}

	h.redirectResult(w, r, provider, "ok")
}

func validState(r *http.Request, state string) bool {
	cookie, err := r.Cookie("oauth_state")
	if err != nil {
		return false
	}
	return subtleEqual(cookie.Value, state)
}

func (h *Handler) redirectResult(w http.ResponseWriter, r *http.Request, provider, status string) {
	target := h.webURL + "/oauth/" + url.PathEscape(provider) + "/complete?status=" + url.QueryEscape(status)
	http.Redirect(w, r, target, http.StatusFound)
}
