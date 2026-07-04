package models

import (
	"fmt"
	"time"

	"github.com/isyll/go-grpc-starter/pkg/idenc"
)

type UserStatus string

const (
	UserStatusActive    UserStatus = "active"
	UserStatusInactive  UserStatus = "inactive"
	UserStatusSuspended UserStatus = "suspended"
)

type UserRole string

const (
	UserRoleUser  UserRole = "user"
	UserRoleAdmin UserRole = "admin"
)

type User struct {
	ID           int64  `gorm:"primaryKey" json:"id"    msgpack:"id"`
	Email        string `                  json:"email" msgpack:"email"`
	PasswordHash string `json:"-" msgpack:"-"`

	FirstName string `json:"first_name" msgpack:"first_name"`
	LastName  string `json:"last_name"  msgpack:"last_name"`
	Avatar    string `json:"avatar"     msgpack:"avatar"`
	Bio       string `json:"bio"        msgpack:"bio"`

	Status UserStatus `json:"status" msgpack:"status"`
	Role   UserRole   `json:"role" msgpack:"role"`

	EmailVerifiedAt *time.Time `json:"email_verified_at" msgpack:"email_verified_at"`
	LastLoginAt     *time.Time `json:"last_login_at"     msgpack:"last_login_at"`

	CreatedAt time.Time `json:"created_at" msgpack:"created_at"`
	UpdatedAt time.Time `json:"updated_at" msgpack:"updated_at"`

	DeletedAt *time.Time `json:"-" msgpack:"-"`

	ActiveSuspension *AccountSuspension  `gorm:"foreignKey:UserID;references:ID" json:"active_suspension,omitempty" msgpack:"active_suspension,omitempty"`
	StatusHistory    []UserStatusHistory `gorm:"foreignKey:UserID"               json:"status_history,omitempty"    msgpack:"status_history,omitempty"`
	UserSettings     *UserSettings       `gorm:"foreignKey:UserID;references:ID" json:"user_settings,omitempty"     msgpack:"user_settings,omitempty"`
}

type UserList []*User

func (User) TableName() string {
	return "auth.users"
}

func (u *User) IsActive() bool {
	return u.Status == UserStatusActive
}

func (u *User) IsSuspended() bool {
	return u.Status == UserStatusSuspended
}

func (u *User) IsAdmin() bool {
	return u.Role == UserRoleAdmin
}

func (u *User) IsEmailVerified() bool {
	return u.EmailVerifiedAt != nil
}

func (u *User) HasActiveSuspension() bool {
	if !u.IsSuspended() || u.ActiveSuspension == nil {
		return false
	}
	return u.ActiveSuspension.IsActive()
}

func (u *User) GetFullName() string {
	return fmt.Sprintf("%s %s", u.FirstName, u.LastName)
}

func (u *User) ToResponse(encoder idenc.IDEncoder) *UserResponse {
	return &UserResponse{
		ID:            encoder.Encode(u.ID),
		Email:         u.Email,
		FirstName:     u.FirstName,
		LastName:      u.LastName,
		Avatar:        u.Avatar,
		Bio:           u.Bio,
		Status:        u.Status,
		Role:          u.Role,
		EmailVerified: u.IsEmailVerified(),
		CreatedAt:     u.CreatedAt,
		UpdatedAt:     u.UpdatedAt,
	}
}

func (u *User) ToPublicProfile(encoder idenc.IDEncoder) *UserPublicProfile {
	return &UserPublicProfile{
		ID:        encoder.Encode(u.ID),
		FirstName: u.FirstName,
		LastName:  u.LastName,
		Avatar:    u.Avatar,
		Bio:       u.Bio,
		CreatedAt: u.CreatedAt,
	}
}

type UserResponse struct {
	ID            string     `json:"id"             msgpack:"id"             example:"18n7q8765"`
	Email         string     `json:"email"          msgpack:"email"          example:"john@example.com"`
	FirstName     string     `json:"first_name"     msgpack:"first_name"     example:"John"`
	LastName      string     `json:"last_name"      msgpack:"last_name"      example:"Doe"`
	Avatar        string     `json:"avatar"         msgpack:"avatar"`
	Bio           string     `json:"bio"            msgpack:"bio"`
	Status        UserStatus `json:"status"         msgpack:"status"         example:"active"`
	Role          UserRole   `json:"role"           msgpack:"role"           example:"user"`
	EmailVerified bool       `json:"email_verified" msgpack:"email_verified"`
	CreatedAt     time.Time  `json:"created_at"     msgpack:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"     msgpack:"updated_at"`
}

type UserPublicProfile struct {
	ID        string    `json:"id"           msgpack:"id"           example:"18n7q8765"`
	FirstName string    `json:"first_name"   msgpack:"first_name"   example:"John"`
	LastName  string    `json:"last_name"    msgpack:"last_name"    example:"Doe"`
	Avatar    string    `json:"avatar"       msgpack:"avatar"`
	Bio       string    `json:"bio"          msgpack:"bio"`
	CreatedAt time.Time `json:"member_since" msgpack:"member_since"`
}
