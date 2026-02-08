package integrations

import "time"

type Provider struct {
	Name         string         `json:"name"`
	DisplayName  string         `json:"displayName"`
	Description  string         `json:"description,omitempty"`
	Capabilities []string       `json:"capabilities,omitempty"`
	Schema       map[string]any `json:"schema,omitempty"`
	UpdatedAt    time.Time      `json:"updatedAt"`
}

type CredentialRef struct {
	ID          string    `json:"id"`
	Provider    string    `json:"provider"`
	Name        string    `json:"name"`
	SecretRef   string    `json:"secretRef"`
	Description string    `json:"description,omitempty"`
	UpdatedAt   time.Time `json:"updatedAt"`
}
