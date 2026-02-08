package auth

import "time"

type Role string

const (
	RoleViewer   Role = "viewer"
	RoleOperator Role = "operator"
	RoleAdmin    Role = "admin"
)

type APIKey struct {
	ID         string     `json:"id"`
	Role       Role       `json:"role"`
	CreatedAt  time.Time  `json:"createdAt"`
	RotatedAt  *time.Time `json:"rotatedAt,omitempty"`
	DisabledAt *time.Time `json:"disabledAt,omitempty"`
}

type KeyWithSecret struct {
	APIKey
	Secret string `json:"secret"`
}

func (r Role) Rank() int {
	switch r {
	case RoleAdmin:
		return 3
	case RoleOperator:
		return 2
	case RoleViewer:
		return 1
	default:
		return 0
	}
}

func (r Role) Valid() bool {
	return r == RoleViewer || r == RoleOperator || r == RoleAdmin
}
