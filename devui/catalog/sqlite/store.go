package sqlite

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PipeOpsHQ/agent-sdk-go/devui/catalog"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

type Store struct {
	db *sql.DB
}

func New(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("catalog sqlite path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create catalog db dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open catalog sqlite db: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.ExecContext(context.Background(), "PRAGMA journal_mode=WAL;"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to enable wal: %w", err)
	}
	if _, err := db.ExecContext(context.Background(), schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to initialize catalog schema: %w", err)
	}
	if err := ensureCompatibilityMigrations(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func ensureCompatibilityMigrations(ctx context.Context, db *sql.DB) error {
	if err := ensureColumn(ctx, db, "tool_instances", "secret_refs_json", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
		return err
	}
	return nil
}

func ensureColumn(ctx context.Context, db *sql.DB, table string, column string, ddl string) error {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s);", table))
	if err != nil {
		return fmt.Errorf("read table info for %s: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid      int
			name     string
			typeName string
			notNull  int
			defValue sql.NullString
			primary  int
		)
		if err := rows.Scan(&cid, &name, &typeName, &notNull, &defValue, &primary); err != nil {
			return fmt.Errorf("scan table info for %s: %w", table, err)
		}
		if strings.EqualFold(name, column) {
			return nil
		}
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s;", table, column, ddl)); err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}

func (s *Store) ListTemplates(ctx context.Context) ([]catalog.ToolTemplate, error) {
	const q = `
SELECT id, name, description, schema_json, created_at, updated_at
FROM tool_templates
ORDER BY updated_at DESC;
`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}
	defer rows.Close()
	out := []catalog.ToolTemplate{}
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) SaveTemplate(ctx context.Context, template catalog.ToolTemplate) (catalog.ToolTemplate, error) {
	now := time.Now().UTC()
	if template.ID == "" {
		template.ID = uuid.NewString()
	}
	if strings.TrimSpace(template.Name) == "" {
		return catalog.ToolTemplate{}, fmt.Errorf("template name is required")
	}
	if template.CreatedAt.IsZero() {
		template.CreatedAt = now
	}
	template.UpdatedAt = now
	if template.Schema == nil {
		template.Schema = map[string]any{}
	}
	schemaRaw, err := json.Marshal(template.Schema)
	if err != nil {
		return catalog.ToolTemplate{}, fmt.Errorf("encode template schema: %w", err)
	}

	const q = `
INSERT INTO tool_templates (id, name, description, schema_json, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  name=excluded.name,
  description=excluded.description,
  schema_json=excluded.schema_json,
  updated_at=excluded.updated_at;
`
	if _, err := s.db.ExecContext(
		ctx,
		q,
		template.ID,
		template.Name,
		template.Description,
		string(schemaRaw),
		template.CreatedAt.Format(time.RFC3339Nano),
		template.UpdatedAt.Format(time.RFC3339Nano),
	); err != nil {
		return catalog.ToolTemplate{}, fmt.Errorf("save template: %w", err)
	}
	if _, err := s.db.ExecContext(
		ctx,
		`INSERT INTO tool_template_revisions (template_id, schema_json, created_at) VALUES (?, ?, ?);`,
		template.ID,
		string(schemaRaw),
		now.Format(time.RFC3339Nano),
	); err != nil {
		return catalog.ToolTemplate{}, fmt.Errorf("save template revision: %w", err)
	}
	return template, nil
}

func (s *Store) DeleteTemplate(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("template id is required")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM tool_templates WHERE id = ?;`, id)
	if err != nil {
		return fmt.Errorf("delete template: %w", err)
	}
	return nil
}

func (s *Store) ListInstances(ctx context.Context) ([]catalog.ToolInstance, error) {
	const q = `
SELECT id, template_id, name, config_json, secret_refs_json, enabled, created_at, updated_at
FROM tool_instances
ORDER BY updated_at DESC;
`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list instances: %w", err)
	}
	defer rows.Close()
	out := []catalog.ToolInstance{}
	for rows.Next() {
		i, err := scanInstance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Store) SaveInstance(ctx context.Context, instance catalog.ToolInstance) (catalog.ToolInstance, error) {
	now := time.Now().UTC()
	if instance.ID == "" {
		instance.ID = uuid.NewString()
	}
	if strings.TrimSpace(instance.TemplateID) == "" {
		return catalog.ToolInstance{}, fmt.Errorf("templateId is required")
	}
	if strings.TrimSpace(instance.Name) == "" {
		return catalog.ToolInstance{}, fmt.Errorf("instance name is required")
	}
	if instance.CreatedAt.IsZero() {
		instance.CreatedAt = now
	}
	instance.UpdatedAt = now
	if instance.Config == nil {
		instance.Config = map[string]any{}
	}
	if instance.SecretRefs == nil {
		instance.SecretRefs = []string{}
	}
	cfgRaw, err := json.Marshal(instance.Config)
	if err != nil {
		return catalog.ToolInstance{}, fmt.Errorf("encode instance config: %w", err)
	}
	secretRefsRaw, err := json.Marshal(instance.SecretRefs)
	if err != nil {
		return catalog.ToolInstance{}, fmt.Errorf("encode secret refs: %w", err)
	}

	const q = `
INSERT INTO tool_instances (id, template_id, name, config_json, secret_refs_json, enabled, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  template_id=excluded.template_id,
  name=excluded.name,
  config_json=excluded.config_json,
  secret_refs_json=excluded.secret_refs_json,
  enabled=excluded.enabled,
  updated_at=excluded.updated_at;
`
	enabled := 0
	if instance.Enabled {
		enabled = 1
	}
	if _, err := s.db.ExecContext(
		ctx,
		q,
		instance.ID,
		instance.TemplateID,
		instance.Name,
		string(cfgRaw),
		string(secretRefsRaw),
		enabled,
		instance.CreatedAt.Format(time.RFC3339Nano),
		instance.UpdatedAt.Format(time.RFC3339Nano),
	); err != nil {
		return catalog.ToolInstance{}, fmt.Errorf("save instance: %w", err)
	}
	return instance, nil
}

func (s *Store) DeleteInstance(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("instance id is required")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM tool_instances WHERE id = ?;`, id)
	if err != nil {
		return fmt.Errorf("delete instance: %w", err)
	}
	return nil
}

func (s *Store) ListBundles(ctx context.Context) ([]catalog.ToolBundle, error) {
	const q = `
SELECT id, name, description, tool_names_json, created_at, updated_at
FROM tool_bundles
ORDER BY updated_at DESC;
`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list bundles: %w", err)
	}
	defer rows.Close()
	out := []catalog.ToolBundle{}
	for rows.Next() {
		b, err := scanBundle(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) SaveBundle(ctx context.Context, bundle catalog.ToolBundle) (catalog.ToolBundle, error) {
	now := time.Now().UTC()
	if bundle.ID == "" {
		bundle.ID = uuid.NewString()
	}
	if strings.TrimSpace(bundle.Name) == "" {
		return catalog.ToolBundle{}, fmt.Errorf("bundle name is required")
	}
	if bundle.CreatedAt.IsZero() {
		bundle.CreatedAt = now
	}
	bundle.UpdatedAt = now
	if bundle.ToolNames == nil {
		bundle.ToolNames = []string{}
	}
	toolRaw, err := json.Marshal(bundle.ToolNames)
	if err != nil {
		return catalog.ToolBundle{}, fmt.Errorf("encode bundle tools: %w", err)
	}

	const q = `
INSERT INTO tool_bundles (id, name, description, tool_names_json, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  name=excluded.name,
  description=excluded.description,
  tool_names_json=excluded.tool_names_json,
  updated_at=excluded.updated_at;
`
	if _, err := s.db.ExecContext(
		ctx,
		q,
		bundle.ID,
		bundle.Name,
		bundle.Description,
		string(toolRaw),
		bundle.CreatedAt.Format(time.RFC3339Nano),
		bundle.UpdatedAt.Format(time.RFC3339Nano),
	); err != nil {
		return catalog.ToolBundle{}, fmt.Errorf("save bundle: %w", err)
	}
	return bundle, nil
}

func (s *Store) DeleteBundle(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("bundle id is required")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM tool_bundles WHERE id = ?;`, id)
	if err != nil {
		return fmt.Errorf("delete bundle: %w", err)
	}
	return nil
}

func (s *Store) ListWorkflowBindings(ctx context.Context) ([]catalog.WorkflowToolBinding, error) {
	const q = `
SELECT workflow, bundle_ids_json, tool_names_json, updated_at
FROM workflow_tool_bindings
ORDER BY workflow ASC;
`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list workflow bindings: %w", err)
	}
	defer rows.Close()
	out := []catalog.WorkflowToolBinding{}
	for rows.Next() {
		b, err := scanBinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) GetWorkflowBinding(ctx context.Context, workflow string) (catalog.WorkflowToolBinding, error) {
	if strings.TrimSpace(workflow) == "" {
		return catalog.WorkflowToolBinding{}, fmt.Errorf("workflow is required")
	}
	const q = `
SELECT workflow, bundle_ids_json, tool_names_json, updated_at
FROM workflow_tool_bindings
WHERE workflow = ?;
`
	row := s.db.QueryRowContext(ctx, q, workflow)
	binding, err := scanBinding(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return catalog.WorkflowToolBinding{Workflow: workflow, BundleIDs: []string{}, ToolNames: []string{}}, nil
		}
		return catalog.WorkflowToolBinding{}, err
	}
	return binding, nil
}

func (s *Store) SaveWorkflowBinding(ctx context.Context, binding catalog.WorkflowToolBinding) (catalog.WorkflowToolBinding, error) {
	if strings.TrimSpace(binding.Workflow) == "" {
		return catalog.WorkflowToolBinding{}, fmt.Errorf("workflow is required")
	}
	now := time.Now().UTC()
	binding.UpdatedAt = now
	if binding.BundleIDs == nil {
		binding.BundleIDs = []string{}
	}
	if binding.ToolNames == nil {
		binding.ToolNames = []string{}
	}
	bundleRaw, err := json.Marshal(binding.BundleIDs)
	if err != nil {
		return catalog.WorkflowToolBinding{}, fmt.Errorf("encode bundle ids: %w", err)
	}
	toolRaw, err := json.Marshal(binding.ToolNames)
	if err != nil {
		return catalog.WorkflowToolBinding{}, fmt.Errorf("encode tool names: %w", err)
	}
	const q = `
INSERT INTO workflow_tool_bindings (workflow, bundle_ids_json, tool_names_json, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(workflow) DO UPDATE SET
  bundle_ids_json=excluded.bundle_ids_json,
  tool_names_json=excluded.tool_names_json,
  updated_at=excluded.updated_at;
`
	if _, err := s.db.ExecContext(
		ctx,
		q,
		binding.Workflow,
		string(bundleRaw),
		string(toolRaw),
		binding.UpdatedAt.Format(time.RFC3339Nano),
	); err != nil {
		return catalog.WorkflowToolBinding{}, fmt.Errorf("save workflow binding: %w", err)
	}
	return binding, nil
}

func (s *Store) ListIntegrationProviders(ctx context.Context) ([]catalog.IntegrationProvider, error) {
	const q = `
SELECT name, display_name, description, capabilities_json, schema_json, updated_at
FROM integration_providers
ORDER BY name ASC;
`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list integration providers: %w", err)
	}
	defer rows.Close()
	out := []catalog.IntegrationProvider{}
	for rows.Next() {
		var (
			p            catalog.IntegrationProvider
			capsRaw      string
			schemaRaw    string
			updatedAtRaw string
		)
		if err := rows.Scan(&p.Name, &p.DisplayName, &p.Description, &capsRaw, &schemaRaw, &updatedAtRaw); err != nil {
			return nil, fmt.Errorf("scan integration provider: %w", err)
		}
		_ = json.Unmarshal([]byte(capsRaw), &p.Capabilities)
		_ = json.Unmarshal([]byte(schemaRaw), &p.Schema)
		p.UpdatedAt = mustParseTime(updatedAtRaw)
		if p.Capabilities == nil {
			p.Capabilities = []string{}
		}
		if p.Schema == nil {
			p.Schema = map[string]any{}
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) SaveIntegrationProvider(ctx context.Context, provider catalog.IntegrationProvider) (catalog.IntegrationProvider, error) {
	if strings.TrimSpace(provider.Name) == "" {
		return catalog.IntegrationProvider{}, fmt.Errorf("provider name is required")
	}
	if strings.TrimSpace(provider.DisplayName) == "" {
		provider.DisplayName = provider.Name
	}
	if provider.Capabilities == nil {
		provider.Capabilities = []string{}
	}
	if provider.Schema == nil {
		provider.Schema = map[string]any{}
	}
	provider.UpdatedAt = time.Now().UTC()
	capsRaw, err := json.Marshal(provider.Capabilities)
	if err != nil {
		return catalog.IntegrationProvider{}, fmt.Errorf("encode provider capabilities: %w", err)
	}
	schemaRaw, err := json.Marshal(provider.Schema)
	if err != nil {
		return catalog.IntegrationProvider{}, fmt.Errorf("encode provider schema: %w", err)
	}
	const q = `
INSERT INTO integration_providers (name, display_name, description, capabilities_json, schema_json, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
  display_name=excluded.display_name,
  description=excluded.description,
  capabilities_json=excluded.capabilities_json,
  schema_json=excluded.schema_json,
  updated_at=excluded.updated_at;
`
	_, err = s.db.ExecContext(ctx, q, provider.Name, provider.DisplayName, provider.Description, string(capsRaw), string(schemaRaw), provider.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return catalog.IntegrationProvider{}, fmt.Errorf("save integration provider: %w", err)
	}
	return provider, nil
}

func (s *Store) ListCredentialMeta(ctx context.Context, provider string) ([]catalog.IntegrationCredentialMeta, error) {
	base := `
SELECT id, provider, name, secret_ref, description, updated_at
FROM integration_credentials_meta
`
	args := []any{}
	if strings.TrimSpace(provider) != "" {
		base += "WHERE provider = ?\n"
		args = append(args, provider)
	}
	base += "ORDER BY updated_at DESC;"
	rows, err := s.db.QueryContext(ctx, base, args...)
	if err != nil {
		return nil, fmt.Errorf("list credential meta: %w", err)
	}
	defer rows.Close()
	out := []catalog.IntegrationCredentialMeta{}
	for rows.Next() {
		var (
			m            catalog.IntegrationCredentialMeta
			updatedAtRaw string
		)
		if err := rows.Scan(&m.ID, &m.Provider, &m.Name, &m.SecretRef, &m.Description, &updatedAtRaw); err != nil {
			return nil, fmt.Errorf("scan credential meta: %w", err)
		}
		m.UpdatedAt = mustParseTime(updatedAtRaw)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) SaveCredentialMeta(ctx context.Context, meta catalog.IntegrationCredentialMeta) (catalog.IntegrationCredentialMeta, error) {
	if strings.TrimSpace(meta.ID) == "" {
		meta.ID = uuid.NewString()
	}
	if strings.TrimSpace(meta.Provider) == "" {
		return catalog.IntegrationCredentialMeta{}, fmt.Errorf("provider is required")
	}
	if strings.TrimSpace(meta.Name) == "" {
		return catalog.IntegrationCredentialMeta{}, fmt.Errorf("name is required")
	}
	if strings.TrimSpace(meta.SecretRef) == "" {
		return catalog.IntegrationCredentialMeta{}, fmt.Errorf("secretRef is required")
	}
	meta.UpdatedAt = time.Now().UTC()
	const q = `
INSERT INTO integration_credentials_meta (id, provider, name, secret_ref, description, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  provider=excluded.provider,
  name=excluded.name,
  secret_ref=excluded.secret_ref,
  description=excluded.description,
  updated_at=excluded.updated_at;
`
	_, err := s.db.ExecContext(ctx, q, meta.ID, meta.Provider, meta.Name, meta.SecretRef, meta.Description, meta.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return catalog.IntegrationCredentialMeta{}, fmt.Errorf("save credential meta: %w", err)
	}
	return meta, nil
}

func scanTemplate(scanner interface{ Scan(dest ...any) error }) (catalog.ToolTemplate, error) {
	var (
		t         catalog.ToolTemplate
		schemaRaw string
		created   string
		updated   string
	)
	if err := scanner.Scan(&t.ID, &t.Name, &t.Description, &schemaRaw, &created, &updated); err != nil {
		return catalog.ToolTemplate{}, err
	}
	_ = json.Unmarshal([]byte(schemaRaw), &t.Schema)
	t.CreatedAt = mustParseTime(created)
	t.UpdatedAt = mustParseTime(updated)
	if t.Schema == nil {
		t.Schema = map[string]any{}
	}
	return t, nil
}

func scanInstance(scanner interface{ Scan(dest ...any) error }) (catalog.ToolInstance, error) {
	var (
		i             catalog.ToolInstance
		cfgRaw        string
		secretRefsRaw string
		enabled       int
		createdAt     string
		updatedAt     string
	)
	if err := scanner.Scan(&i.ID, &i.TemplateID, &i.Name, &cfgRaw, &secretRefsRaw, &enabled, &createdAt, &updatedAt); err != nil {
		return catalog.ToolInstance{}, err
	}
	_ = json.Unmarshal([]byte(cfgRaw), &i.Config)
	_ = json.Unmarshal([]byte(secretRefsRaw), &i.SecretRefs)
	i.Enabled = enabled != 0
	i.CreatedAt = mustParseTime(createdAt)
	i.UpdatedAt = mustParseTime(updatedAt)
	if i.Config == nil {
		i.Config = map[string]any{}
	}
	if i.SecretRefs == nil {
		i.SecretRefs = []string{}
	}
	return i, nil
}

func scanBundle(scanner interface{ Scan(dest ...any) error }) (catalog.ToolBundle, error) {
	var (
		b         catalog.ToolBundle
		toolRaw   string
		createdAt string
		updatedAt string
	)
	if err := scanner.Scan(&b.ID, &b.Name, &b.Description, &toolRaw, &createdAt, &updatedAt); err != nil {
		return catalog.ToolBundle{}, err
	}
	_ = json.Unmarshal([]byte(toolRaw), &b.ToolNames)
	b.CreatedAt = mustParseTime(createdAt)
	b.UpdatedAt = mustParseTime(updatedAt)
	if b.ToolNames == nil {
		b.ToolNames = []string{}
	}
	return b, nil
}

func scanBinding(scanner interface{ Scan(dest ...any) error }) (catalog.WorkflowToolBinding, error) {
	var (
		b         catalog.WorkflowToolBinding
		bundleRaw string
		toolRaw   string
		updatedAt string
	)
	if err := scanner.Scan(&b.Workflow, &bundleRaw, &toolRaw, &updatedAt); err != nil {
		return catalog.WorkflowToolBinding{}, err
	}
	_ = json.Unmarshal([]byte(bundleRaw), &b.BundleIDs)
	_ = json.Unmarshal([]byte(toolRaw), &b.ToolNames)
	if b.BundleIDs == nil {
		b.BundleIDs = []string{}
	}
	if b.ToolNames == nil {
		b.ToolNames = []string{}
	}
	b.UpdatedAt = mustParseTime(updatedAt)
	return b, nil
}

func mustParseTime(raw string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}
	}
	return t
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

var _ catalog.Store = (*Store)(nil)
