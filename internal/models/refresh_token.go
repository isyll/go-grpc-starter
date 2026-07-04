package models

import (
	"time"
)

type RefreshToken struct {
	ID            string     `gorm:"primaryKey" json:"id"                       msgpack:"id"`
	SessionID     int64      `                  json:"session_id"               msgpack:"session_id"`
	TokenHash     string     `                  json:"token_hash"               msgpack:"token_hash"`
	TokenPrefix   string     `gorm:"column:token_prefix" json:"token_prefix" msgpack:"token_prefix"`
	TokenFamily   string     `                  json:"token_family"             msgpack:"token_family"`
	ExpiresAt     time.Time  `                  json:"expires_at"               msgpack:"expires_at"`
	RevokedAt     *time.Time `                  json:"revoked_at,omitempty"     msgpack:"revoked_at,omitempty"`
	RevokedReason string     `                  json:"revoked_reason,omitempty" msgpack:"revoked_reason,omitempty"`
	CreatedAt     time.Time  `                  json:"created_at"               msgpack:"created_at"`

	Session DeviceSession `gorm:"foreignKey:SessionID" json:"session,omitempty" msgpack:"session,omitempty"`
}

func (RefreshToken) TableName() string {
	return "auth.refresh_tokens"
}

func (rt *RefreshToken) IsValid() bool {
	return rt.RevokedAt == nil &&
		rt.ExpiresAt.After(time.Now())
}

func (rt *RefreshToken) IsRevoked() bool {
	return rt.RevokedAt != nil && !rt.RevokedAt.IsZero()
}

func (rt *RefreshToken) IsExpired() bool {
	return rt.ExpiresAt.Before(time.Now())
}
