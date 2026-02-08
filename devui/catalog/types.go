package catalog

import "time"

type ToolTemplate struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Schema      map[string]any `json:"schema,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

type ToolInstance struct {
	ID         string         `json:"id"`
	TemplateID string         `json:"templateId"`
	Name       string         `json:"name"`
	Config     map[string]any `json:"config,omitempty"`
	SecretRefs []string       `json:"secretRefs,omitempty"`
	Enabled    bool           `json:"enabled"`
	CreatedAt  time.Time      `json:"createdAt"`
	UpdatedAt  time.Time      `json:"updatedAt"`
}

type ToolBundle struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	ToolNames   []string  `json:"toolNames,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type WorkflowToolBinding struct {
	Workflow  string    `json:"workflow"`
	BundleIDs []string  `json:"bundleIds,omitempty"`
	ToolNames []string  `json:"toolNames,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type IntegrationProvider struct {
	Name         string         `json:"name"`
	DisplayName  string         `json:"displayName"`
	Description  string         `json:"description,omitempty"`
	Capabilities []string       `json:"capabilities,omitempty"`
	Schema       map[string]any `json:"schema,omitempty"`
	UpdatedAt    time.Time      `json:"updatedAt"`
}

type IntegrationCredentialMeta struct {
	ID          string    `json:"id"`
	Provider    string    `json:"provider"`
	Name        string    `json:"name"`
	SecretRef   string    `json:"secretRef"`
	Description string    `json:"description,omitempty"`
	UpdatedAt   time.Time `json:"updatedAt"`
}
