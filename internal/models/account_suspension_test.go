package models_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/isyll/go-grpc-starter/internal/models"
)

func TestAccountSuspension_IsActive(t *testing.T) {
	past := time.Now().Add(-2 * time.Hour)
	future := time.Now().Add(2 * time.Hour)

	tests := []struct {
		name     string
		susp     models.AccountSuspension
		expected bool
	}{
		{
			name: "permanent suspension is always active",
			susp: models.AccountSuspension{
				IsPermanent: true,
			},
			expected: true,
		},
		{
			name: "temporary suspension active if until is in future",
			susp: models.AccountSuspension{
				IsPermanent:    false,
				SuspendedUntil: &future,
			},
			expected: true,
		},
		{
			name: "temporary suspension inactive if until is in past",
			susp: models.AccountSuspension{
				IsPermanent:    false,
				SuspendedUntil: &past,
			},
			expected: false,
		},
		{
			name: "temporary suspension inactive if until is null",
			susp: models.AccountSuspension{
				IsPermanent:    false,
				SuspendedUntil: nil,
			},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.susp.IsActive())
		})
	}
}
