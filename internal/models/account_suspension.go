package models

import (
	"time"

	"github.com/isyll/go-grpc-starter/pkg/idenc"
)

type SuspensionReason string

const (
	SuspensionReasonTermsViolation     SuspensionReason = "terms_violation"
	SuspensionReasonFraudulentActivity SuspensionReason = "fraudulent_activity"
	SuspensionReasonHarassment         SuspensionReason = "harassment"
	SuspensionReasonSpam               SuspensionReason = "spam"
	SuspensionReasonSecurityBreach     SuspensionReason = "security_breach"
	SuspensionReasonLegalRequest       SuspensionReason = "legal_request"
	SuspensionReasonOther              SuspensionReason = "other"
)

type AccountSuspension struct {
	ID             int64            `gorm:"primaryKey" json:"id"              msgpack:"id"`
	UserID         int64            `                  json:"user_id"         msgpack:"user_id"`
	Reason         SuspensionReason `                  json:"reason"          msgpack:"reason"`
	Details        string           `                  json:"details"         msgpack:"details"`
	SuspendedAt    time.Time        `                  json:"suspended_at"    msgpack:"suspended_at"`
	SuspendedUntil *time.Time       `                  json:"suspended_until" msgpack:"suspended_until"`
	IsPermanent    bool             `                  json:"is_permanent"    msgpack:"is_permanent"`
	CreatedAt      time.Time        `                  json:"created_at"      msgpack:"created_at"`
	UpdatedAt      time.Time        `                  json:"updated_at"      msgpack:"updated_at"`

	User *User `gorm:"foreignKey:UserID" json:"user,omitempty" msgpack:"user,omitempty"`
}

func (AccountSuspension) TableName() string {
	return "auth.account_suspensions"
}

func (s *AccountSuspension) IsActive() bool {
	if s.IsPermanent {
		return true
	}
	if s.SuspendedUntil != nil && s.SuspendedUntil.After(time.Now()) {
		return true
	}
	return false
}

func (s *AccountSuspension) ToResponse(
	encoder idenc.IDEncoder,
) *AccountSuspensionResponse {
	return &AccountSuspensionResponse{
		ID:             encoder.Encode(s.ID),
		Reason:         s.Reason,
		Details:        s.Details,
		SuspendedAt:    s.SuspendedAt,
		SuspendedUntil: s.SuspendedUntil,
		IsPermanent:    s.IsPermanent,
	}
}

type AccountSuspensionResponse struct {
	ID             string           `json:"id"                        msgpack:"id"                        example:"18n7q8765"`
	Reason         SuspensionReason `json:"reason"                    msgpack:"reason"                    example:"terms_violation"`
	Details        string           `json:"details"                   msgpack:"details"                   example:"Multiple violations of community guidelines"`
	SuspendedAt    time.Time        `json:"suspended_at"              msgpack:"suspended_at"              example:"2023-01-01T10:00:00Z"`
	SuspendedUntil *time.Time       `json:"suspended_until,omitempty" msgpack:"suspended_until,omitempty" example:"2023-02-01T10:00:00Z"`
	IsPermanent    bool             `json:"is_permanent"              msgpack:"is_permanent"              example:"false"`
}
