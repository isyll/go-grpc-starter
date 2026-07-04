package models

import (
	"time"
)

type DeviceSession struct {
	ID               int64  `gorm:"primaryKey;autoIncrement" json:"id"                msgpack:"id"`
	UserID           int64  `                                json:"user_id"            msgpack:"user_id"`
	Platform         string `                                json:"platform"           msgpack:"platform"`
	Manufacturer     string `                                json:"manufacturer"       msgpack:"manufacturer"`
	Model            string `                                json:"model"              msgpack:"model"`
	Version          string `                                json:"version"            msgpack:"version"`
	SDK              string `                                json:"sdk"                msgpack:"sdk"`
	Brand            string `                                json:"brand"              msgpack:"brand"`
	Hardware         string `                                json:"hardware"           msgpack:"hardware"`
	Board            string `                                json:"board"              msgpack:"board"`
	Device           string `                                json:"device"             msgpack:"device"`
	Product          string `                                json:"product"            msgpack:"product"`
	IsPhysicalDevice bool   `                                json:"is_physical_device" msgpack:"is_physical_device"`
	Name             string `                                json:"name"               msgpack:"name"`
	Identifier       string `                                json:"identifier"         msgpack:"identifier"`
	DeviceID         string `                                json:"device_id"          msgpack:"device_id"`

	LastActivity time.Time `json:"last_activity" msgpack:"last_activity"`
	IPAddress    string    `json:"ip_address"    msgpack:"ip_address"`
	UserAgent    string    `json:"user_agent"    msgpack:"user_agent"`
	Location     string    `json:"location"      msgpack:"location"`

	RevokedAt     *time.Time `json:"revoked_at,omitempty"     msgpack:"revoked_at,omitempty"`
	RevokedReason string     `json:"revoked_reason,omitempty" msgpack:"revoked_reason,omitempty"`

	CreatedAt time.Time `json:"created_at" msgpack:"created_at"`

	User User `gorm:"foreignKey:UserID" json:"user" msgpack:"user"`
}

func (DeviceSession) TableName() string {
	return "auth.device_sessions"
}

func (ds *DeviceSession) IsValid(timeout time.Duration) bool {
	return !ds.IsRevoked() && !ds.IsInactive(timeout)
}

func (ds *DeviceSession) IsRevoked() bool {
	return ds.RevokedAt != nil && !ds.RevokedAt.IsZero()
}

func (ds *DeviceSession) IsInactive(timeout time.Duration) bool {
	return time.Since(ds.LastActivity) > timeout
}

func (ds *DeviceSession) GetDeviceInfo() map[string]any {
	return map[string]any{
		"platform":           ds.Platform,
		"manufacturer":       ds.Manufacturer,
		"model":              ds.Model,
		"version":            ds.Version,
		"sdk":                ds.SDK,
		"brand":              ds.Brand,
		"hardware":           ds.Hardware,
		"board":              ds.Board,
		"device":             ds.Device,
		"product":            ds.Product,
		"is_physical_device": ds.IsPhysicalDevice,
		"name":               ds.Name,
		"identifier":         ds.Identifier,
		"device_id":          ds.DeviceID,
	}
}
