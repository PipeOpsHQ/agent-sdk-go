package catalog

import "context"

type Store interface {
	ListTemplates(ctx context.Context) ([]ToolTemplate, error)
	SaveTemplate(ctx context.Context, template ToolTemplate) (ToolTemplate, error)
	DeleteTemplate(ctx context.Context, id string) error

	ListInstances(ctx context.Context) ([]ToolInstance, error)
	SaveInstance(ctx context.Context, instance ToolInstance) (ToolInstance, error)
	DeleteInstance(ctx context.Context, id string) error

	ListBundles(ctx context.Context) ([]ToolBundle, error)
	SaveBundle(ctx context.Context, bundle ToolBundle) (ToolBundle, error)
	DeleteBundle(ctx context.Context, id string) error

	ListWorkflowBindings(ctx context.Context) ([]WorkflowToolBinding, error)
	GetWorkflowBinding(ctx context.Context, workflow string) (WorkflowToolBinding, error)
	SaveWorkflowBinding(ctx context.Context, binding WorkflowToolBinding) (WorkflowToolBinding, error)

	ListIntegrationProviders(ctx context.Context) ([]IntegrationProvider, error)
	SaveIntegrationProvider(ctx context.Context, provider IntegrationProvider) (IntegrationProvider, error)
	ListCredentialMeta(ctx context.Context, provider string) ([]IntegrationCredentialMeta, error)
	SaveCredentialMeta(ctx context.Context, meta IntegrationCredentialMeta) (IntegrationCredentialMeta, error)

	Close() error
}
