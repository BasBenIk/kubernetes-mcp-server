package metrics

import (
	"errors"
	"fmt"

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
			metricsConfig, err := metricsConfigForTarget(params)
			if err != nil {
				return api.NewToolCallResult("", err), nil
			}
			params.BaseConfig = metricsConfig
			return handler(params)
		}
	}
	return tools
}

func (t *Toolset) GetPrompts() []api.ServerPrompt { return nil }

func (t *Toolset) GetResources() []api.ServerResource { return nil }

func (t *Toolset) GetResourceTemplates() []api.ServerResourceTemplate { return nil }

type metricsConfigOverride struct {
	api.BaseConfig
	metricsConfig *obsmetricsconfig.Config
}

func (c metricsConfigOverride) GetToolsetConfig(name string) (api.ExtendedConfig, bool) {
	if name == obsmetricsconfig.ToolsetName {
		return c.metricsConfig, true
	}
	return c.BaseConfig.GetToolsetConfig(name)
}

func metricsConfigForTarget(params api.ToolHandlerParams) (metricsConfigOverride, error) {
	extended, ok := params.GetToolsetConfig(obsmetricsconfig.ToolsetName)
	if !ok {
		return metricsConfigOverride{}, errors.New("observability metrics configuration is required")
	}
	configured, ok := extended.(*obsmetricsconfig.Config)
	if !ok {
		return metricsConfigOverride{}, errors.New("invalid observability metrics configuration")
	}
	metricsConfig := *configured
	target := ""
	if provider, ok := params.FilteringProvider.(api.TargetProvider); ok {
		target = provider.GetDefaultTarget()
		if value, found := params.GetArguments()[provider.GetTargetParameterName()]; found {
			selected, ok := value.(string)
			if !ok {
				return metricsConfigOverride{}, fmt.Errorf("%s parameter must be a string", provider.GetTargetParameterName())
			}
			target = selected
		}
	}
	if provider, ok := params.BaseConfig.(api.ObservabilityMetricsEndpointsProvider); ok {
		if endpoint, found := provider.GetObservabilityMetricsEndpoints()[target]; found {
			if endpoint.PrometheusURL != "" {
				metricsConfig.PrometheusURL = endpoint.PrometheusURL
			}
			if endpoint.AlertmanagerURL != "" {
				metricsConfig.AlertmanagerURL = endpoint.AlertmanagerURL
			}
		}
	}
	if metricsConfig.PrometheusURL == "" {
		return metricsConfigOverride{}, fmt.Errorf("observability metrics prometheus_url is required for target %q", target)
	}
	if !params.IsRequireTLS() {
		return metricsConfigOverride{BaseConfig: params.BaseConfig, metricsConfig: &metricsConfig}, nil
	}
	if metricsConfig.Insecure {
		return metricsConfigOverride{}, errors.New("require_tls is enabled but observability metrics insecure is true")
	}
	if err := config.ValidateURLsRequireTLS(map[string]string{
		"observability metrics prometheus_url":   metricsConfig.PrometheusURL,
		"observability metrics alertmanager_url": metricsConfig.AlertmanagerURL,
	}); err != nil {
		return metricsConfigOverride{}, err
	}
	return metricsConfigOverride{BaseConfig: params.BaseConfig, metricsConfig: &metricsConfig}, nil
}
