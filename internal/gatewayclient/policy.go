package gatewayclient

import (
	"time"
)

// PolicyRevisionMeta is metadata for policy list (no YAML body).
type PolicyRevisionMeta struct {
	Rev       int       `json:"rev"`
	UpdatedAt time.Time `json:"updated_at"`
	Bytes     int       `json:"bytes"`
	Status    string    `json:"status"`
}
