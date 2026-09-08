package argocd

import (
	"github.com/containers/kubernetes-mcp-server/pkg/api"
	"github.com/containers/kubernetes-mcp-server/pkg/toolsets"
)

type Toolset struct{}

var _ api.Toolset = (*Toolset)(nil)

func (t *Toolset) GetName() string { return "argocd" }

func (t *Toolset) GetDescription() string {
	return "Read-only Argo CD Application fleet status and drift diagnostics."
}

func (t *Toolset) GetTools(p api.FilteringProvider) []api.ServerTool {
	return applicationsTools(p)
}

func (t *Toolset) GetPrompts() []api.ServerPrompt { return nil }

func (t *Toolset) GetResources() []api.ServerResource { return nil }

func (t *Toolset) GetResourceTemplates() []api.ServerResourceTemplate { return nil }

func init() { toolsets.Register(&Toolset{}) }
