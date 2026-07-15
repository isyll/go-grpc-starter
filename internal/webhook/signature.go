package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var (
	errNotConfigured = errors.New("webhook verifier not configured")
	errNoSignature   = errors.New("missing signature")
	errBadSignature  = errors.New("signature mismatch")
	errStaleEvent    = errors.New("timestamp outside tolerance")
)

// Verifier authenticates a raw webhook body against a provider signature.
// Implementations fail closed: an unset secret rejects every request.
type Verifier interface {
	Verify(h http.Header, body []byte) error
}

type hmacVerifier struct {
	header string
	secret string
}

func (v hmacVerifier) Verify(h http.Header, body []byte) error {
	if v.secret == "" {
		return errNotConfigured
	}
	got := strings.TrimSpace(h.Get(v.header))
	if got == "" {
		return errNoSignature
	}
	want := hmacSHA256Hex(v.secret, body)
	if !hmac.Equal([]byte(strings.ToLower(got)), []byte(want)) {
		return errBadSignature
	}
	return nil
}

type stripeVerifier struct {
	secret    string
	tolerance time.Duration
	now       func() time.Time
}

func (v stripeVerifier) Verify(h http.Header, body []byte) error {
	if v.secret == "" {
		return errNotConfigured
	}
	header := h.Get("Stripe-Signature")
	if header == "" {
		return errNoSignature
	}

	var timestamp string
	var signatures []string
	for _, part := range strings.Split(header, ",") {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "t":
			timestamp = strings.TrimSpace(value)
		case "v1":
			signatures = append(signatures, strings.TrimSpace(value))
		}
	}
	if timestamp == "" || len(signatures) == 0 {
		return errNoSignature
	}

	secs, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return errNoSignature
	}
	if v.tolerance > 0 {
		age := v.clock().Sub(time.Unix(secs, 0))
		if age < 0 {
			age = -age
		}
		if age > v.tolerance {
			return errStaleEvent
		}
	}

	want := hmacSHA256Hex(v.secret, append([]byte(timestamp+"."), body...))
	for _, sig := range signatures {
		if hmac.Equal([]byte(strings.ToLower(sig)), []byte(want)) {
			return nil
		}
	}
	return errBadSignature
}

func (v stripeVerifier) clock() time.Time {
	if v.now != nil {
		return v.now()
	}
	return time.Now()
}

type paydunyaVerifier struct {
	masterKey string
}

func (v paydunyaVerifier) Verify(h http.Header, _ []byte) error {
	if v.masterKey == "" {
		return errNotConfigured
	}
	got := strings.TrimSpace(h.Get("X-Paydunya-Signature"))
	if got == "" {
		return errNoSignature
	}
	sum := sha512.Sum512([]byte(v.masterKey))
	want := hex.EncodeToString(sum[:])
	if !hmac.Equal([]byte(strings.ToLower(got)), []byte(want)) {
		return errBadSignature
	}
	return nil
}

func hmacSHA256Hex(secret string, msg []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(msg)
	return hex.EncodeToString(mac.Sum(nil))
}

func subtleEqual(a, b string) bool {
	return hmac.Equal([]byte(a), []byte(b))
}
