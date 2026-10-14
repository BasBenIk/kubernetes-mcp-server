package metrics

import (
	"context"
	"errors"
	"fmt"

	"github.com/containers/kubernetes-mcp-server/pkg/api"
	"github.com/containers/kubernetes-mcp-server/pkg/config"
	"github.com/containers/kubernetes-mcp-server/pkg/toolsets"
	obsmetricsconfig "github.com/rhobs/obs-mcp/pkg/metrics"
	"github.com/rhobs/obs-mcp/pkg/toolcfg"
	"k8s.io/utils/ptr"
)

// Register only the Kubernetes-neutral Prometheus and Alertmanager tools.
// The obs-mcp aggregate package also registers Loki, Tempo, and Collector
// toolsets, which require separate endpoint and compatibility-policy work.
func init() {
	toolsets.Register(&Toolset{})
}

type Toolset struct{}

var _ api.Toolset = (*Toolset)(nil)

func (t *Toolset) GetName() string { return obsmetricsconfig.ToolsetName }

func (t *Toolset) GetDescription() string {
	return "Query explicitly configured Prometheus and Alertmanager endpoints with query safety guardrails."
}

func (t *Toolset) GetTools(ctx context.Context, toolsetContext api.ToolsetContext) []api.ServerTool {
	tools := (&obsmetricsconfig.Toolset{}).GetTools(ctx, toolsetContext)
	for i := range tools {
		// Unlike standalone obs-mcp, this embedder routes endpoints and
		// kubeconfig credentials by the selected Kubernetes target.
		tools[i].ClusterAware = ptr.To(true)
		handler := tools[i].Handler
		tools[i].Handler = func(params api.ToolHandlerParams) (*api.ToolCallResult, error) {
			metricsConfig, err := metricsConfigForTarget(params)
			if err != nil {
				return api.NewToolCallResult("", err), nil
			}
			// Use obs-mcp's request-scoped override rather than mutating the shared
			// server configuration, so concurrent targets cannot share endpoints.
			params.Context = toolcfg.With(params.Context, obsmetricsconfig.ToolsetName, metricsConfig)
			return handler(params)
		}
	}
	return tools
}

func (t *Toolset) GetPrompts(context.Context, api.ToolsetContext) []api.ServerPrompt { return nil }

func (t *Toolset) GetResources(context.Context, api.ToolsetContext) []api.ServerResource { return nil }

func (t *Toolset) GetResourceTemplates(context.Context, api.ToolsetContext) []api.ServerResourceTemplate {
	return nil
}

func metricsConfigForTarget(params api.ToolHandlerParams) (*obsmetricsconfig.Config, error) {
	if params.Config == nil {
		return nil, errors.New("observability metrics configuration is required")
	}
	extended, ok := params.GetToolsetConfig(obsmetricsconfig.ToolsetName)
	if !ok {
		return nil, errors.New("observability metrics configuration is required")
	}
	configured, ok := extended.(*obsmetricsconfig.Config)
	if !ok {
		return nil, errors.New("invalid observability metrics configuration")
	}
	metricsConfig := *configured
	if endpoint, found := params.Config.ObservabilityMetricsContexts.Get()[params.Target]; found {
		if endpoint.PrometheusURL != "" {
			metricsConfig.PrometheusURL = endpoint.PrometheusURL
		}
		if endpoint.AlertmanagerURL != "" {
			metricsConfig.AlertmanagerURL = endpoint.AlertmanagerURL
		}
	}
	if metricsConfig.PrometheusURL == "" {
		return nil, fmt.Errorf("observability metrics prometheus_url is required for target %q", params.Target)
	}
	if !params.Config.RequireTLS.Get() {
		return &metricsConfig, nil
	}
	if metricsConfig.Insecure {
		return nil, errors.New("require_tls is enabled but observability metrics insecure is true")
	}
	if err := config.ValidateURLsRequireTLS(map[string]string{
		"observability metrics prometheus_url":   metricsConfig.PrometheusURL,
		"observability metrics alertmanager_url": metricsConfig.AlertmanagerURL,
	}); err != nil {
		return nil, err
	}
	return &metricsConfig, nil
}
