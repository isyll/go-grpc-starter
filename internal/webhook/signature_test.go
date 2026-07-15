package webhook

import (
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestHMACVerifier(t *testing.T) {
	body := []byte(`{"event":"payment.succeeded"}`)
	v := hmacVerifier{header: "Wave-Signature", secret: "shhh"}
	sig := hmacSHA256Hex("shhh", body)

	t.Run("valid", func(t *testing.T) {
		h := http.Header{"Wave-Signature": {sig}}
		if err := v.Verify(h, body); err != nil {
			t.Fatalf("want nil, got %v", err)
		}
	})

	t.Run("tampered body", func(t *testing.T) {
		h := http.Header{"Wave-Signature": {sig}}
		if err := v.Verify(h, []byte("tampered")); !errors.Is(err, errBadSignature) {
			t.Fatalf("want errBadSignature, got %v", err)
		}
	})

	t.Run("missing header", func(t *testing.T) {
		if err := v.Verify(http.Header{}, body); !errors.Is(err, errNoSignature) {
			t.Fatalf("want errNoSignature, got %v", err)
		}
	})

	t.Run("unset secret fails closed", func(t *testing.T) {
		empty := hmacVerifier{header: "Wave-Signature"}
		h := http.Header{"Wave-Signature": {sig}}
		if err := empty.Verify(h, body); !errors.Is(err, errNotConfigured) {
			t.Fatalf("want errNotConfigured, got %v", err)
		}
	})
}

func TestStripeVerifier(t *testing.T) {
	body := []byte(`{"id":"evt_1"}`)
	fixed := time.Unix(1_700_000_000, 0)
	v := stripeVerifier{secret: "whsec", tolerance: 5 * time.Minute, now: func() time.Time { return fixed }}

	sign := func(ts time.Time) string {
		t := strconv.FormatInt(ts.Unix(), 10)
		return "t=" + t + ",v1=" + hmacSHA256Hex("whsec", append([]byte(t+"."), body...))
	}

	t.Run("valid", func(t *testing.T) {
		h := http.Header{"Stripe-Signature": {sign(fixed)}}
		if err := v.Verify(h, body); err != nil {
			t.Fatalf("want nil, got %v", err)
		}
	})

	t.Run("stale", func(t *testing.T) {
		h := http.Header{"Stripe-Signature": {sign(fixed.Add(-10 * time.Minute))}}
		if err := v.Verify(h, body); !errors.Is(err, errStaleEvent) {
			t.Fatalf("want errStaleEvent, got %v", err)
		}
	})

	t.Run("bad signature", func(t *testing.T) {
		h := http.Header{"Stripe-Signature": {"t=" + strconv.FormatInt(fixed.Unix(), 10) + ",v1=deadbeef"}}
		if err := v.Verify(h, body); !errors.Is(err, errBadSignature) {
			t.Fatalf("want errBadSignature, got %v", err)
		}
	})
}

func TestPayDunyaVerifier(t *testing.T) {
	v := paydunyaVerifier{masterKey: "master"}
	sum := sha512.Sum512([]byte("master"))
	valid := hex.EncodeToString(sum[:])

	t.Run("valid", func(t *testing.T) {
		h := http.Header{"X-Paydunya-Signature": {valid}}
		if err := v.Verify(h, nil); err != nil {
			t.Fatalf("want nil, got %v", err)
		}
	})

	t.Run("wrong", func(t *testing.T) {
		h := http.Header{"X-Paydunya-Signature": {"nope"}}
		if err := v.Verify(h, nil); !errors.Is(err, errBadSignature) {
			t.Fatalf("want errBadSignature, got %v", err)
		}
	})
}
