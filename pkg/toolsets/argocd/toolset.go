package argocd

import (
	"context"

	"github.com/containers/kubernetes-mcp-server/pkg/api"
	"github.com/containers/kubernetes-mcp-server/pkg/toolsets"
)

type Toolset struct{}

var _ api.Toolset = (*Toolset)(nil)

func (t *Toolset) GetName() string { return "argocd" }

func (t *Toolset) GetDescription() string {
	return "Read-only Argo CD Application fleet status and drift diagnostics."
}

func (t *Toolset) GetTools(ctx context.Context, toolsetContext api.ToolsetContext) []api.ServerTool {
	return applicationsTools(ctx, toolsetContext)
}

func (t *Toolset) GetPrompts(context.Context, api.ToolsetContext) []api.ServerPrompt { return nil }

func (t *Toolset) GetResources(context.Context, api.ToolsetContext) []api.ServerResource { return nil }

func (t *Toolset) GetResourceTemplates(context.Context, api.ToolsetContext) []api.ServerResourceTemplate {
	return nil
}

func init() { toolsets.Register(&Toolset{}) }
