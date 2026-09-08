package metrics

import (
	"errors"

	"github.com/containers/kubernetes-mcp-server/pkg/api"
	"github.com/containers/kubernetes-mcp-server/pkg/config"
	"github.com/containers/kubernetes-mcp-server/pkg/toolsets"
	obsmetricsconfig "github.com/rhobs/obs-mcp/pkg/metrics"
	obsmetricstoolset "github.com/rhobs/obs-mcp/pkg/metrics/toolset"
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

func (t *Toolset) GetTools(provider api.FilteringProvider) []api.ServerTool {
	tools := (&obsmetricstoolset.Toolset{}).GetTools(provider)
	for i := range tools {
		handler := tools[i].Handler
		tools[i].Handler = func(params api.ToolHandlerParams) (*api.ToolCallResult, error) {
			if err := validateMetricsConfig(params); err != nil {
				return api.NewToolCallResult("", err), nil
			}
			return handler(params)
		}
	}
	return tools
}

func (t *Toolset) GetPrompts() []api.ServerPrompt { return nil }

func (t *Toolset) GetResources() []api.ServerResource { return nil }

func (t *Toolset) GetResourceTemplates() []api.ServerResourceTemplate { return nil }

func validateMetricsConfig(params api.ToolHandlerParams) error {
	extended, ok := params.GetToolsetConfig(obsmetricsconfig.ToolsetName)
	if !ok {
		return errors.New("observability metrics configuration is required")
	}
	metricsConfig, ok := extended.(*obsmetricsconfig.Config)
	if !ok || metricsConfig.PrometheusURL == "" {
		return errors.New("observability metrics prometheus_url is required")
	}
	if !params.IsRequireTLS() {
		return nil
	}
	if metricsConfig.Insecure {
		return errors.New("require_tls is enabled but observability metrics insecure is true")
	}
	return config.ValidateURLsRequireTLS(map[string]string{
		"observability metrics prometheus_url":   metricsConfig.PrometheusURL,
		"observability metrics alertmanager_url": metricsConfig.AlertmanagerURL,
	})
}
