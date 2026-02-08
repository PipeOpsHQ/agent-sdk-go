package integrations

import (
	"sort"
	"sync"
	"time"
)

var (
	mu        sync.RWMutex
	providers = map[string]Provider{}
)

func init() {
	seed := []Provider{
		{Name: "http_webhook", DisplayName: "HTTP/Webhook", Description: "Issue HTTP requests and webhooks", Capabilities: []string{"request", "webhook"}},
		{Name: "slack", DisplayName: "Slack", Description: "Send messages to Slack channels/webhooks", Capabilities: []string{"message", "notify"}},
		{Name: "github", DisplayName: "GitHub", Description: "Create issues and comments", Capabilities: []string{"issue_create", "pr_comment"}},
		{Name: "jira", DisplayName: "Jira", Description: "Create and update Jira issues", Capabilities: []string{"issue_create", "issue_update"}},
		{Name: "postgresql", DisplayName: "PostgreSQL", Description: "Run SQL queries", Capabilities: []string{"query"}},
		{Name: "redis", DisplayName: "Redis", Description: "Execute Redis commands", Capabilities: []string{"command"}},
		{Name: "s3", DisplayName: "Amazon S3", Description: "Read and write S3 objects", Capabilities: []string{"object_read", "object_write"}},
		{Name: "gcs", DisplayName: "Google Cloud Storage", Description: "Read and write GCS objects", Capabilities: []string{"object_read", "object_write"}},
		{Name: "pagerduty", DisplayName: "PagerDuty", Description: "Trigger incident events", Capabilities: []string{"event_trigger"}},
		{Name: "smtp", DisplayName: "SMTP", Description: "Send email notifications", Capabilities: []string{"email_send"}},
	}
	for _, provider := range seed {
		_ = Register(provider)
	}
}

func Register(provider Provider) error {
	if provider.Name == "" {
		return nil
	}
	if provider.DisplayName == "" {
		provider.DisplayName = provider.Name
	}
	if provider.UpdatedAt.IsZero() {
		provider.UpdatedAt = time.Now().UTC()
	}
	if provider.Schema == nil {
		provider.Schema = map[string]any{}
	}
	mu.Lock()
	defer mu.Unlock()
	providers[provider.Name] = provider
	return nil
}

func List() []Provider {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Provider, 0, len(providers))
	for _, provider := range providers {
		out = append(out, provider)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func Get(name string) (Provider, bool) {
	mu.RLock()
	defer mu.RUnlock()
	provider, ok := providers[name]
	return provider, ok
}
