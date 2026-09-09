package metrics

import (
	"context"
	"testing"

	"github.com/containers/kubernetes-mcp-server/pkg/api"
	serverconfig "github.com/containers/kubernetes-mcp-server/pkg/config"
	obsmetricsconfig "github.com/rhobs/obs-mcp/pkg/metrics"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type targetProvider struct {
	defaultTarget string
	targetName    string
}

func (p targetProvider) IsMultiTarget() bool { return true }

func (p targetProvider) GetTargets(context.Context) ([]string, error) { return nil, nil }

func (p targetProvider) GetDefaultTarget() string { return p.defaultTarget }

func (p targetProvider) GetTargetParameterName() string { return p.targetName }

func (p targetProvider) IsTargetCompatibilityToolFiltersEnabled() bool { return false }

func (p targetProvider) AnyTargetHasGVKs(context.Context, []schema.GroupVersionKind) bool {
	return true
}

func TestMetricsConfigForTarget(t *testing.T) {
	cfg, err := serverconfig.ReadToml([]byte(`
        [toolset_configs."observability/metrics"]
        prometheus_url = "http://127.0.0.1:9090"
        alertmanager_url = "http://127.0.0.1:9093"

        [observability_metrics_contexts.production]
        prometheus_url = "http://127.0.0.1:19090"
        alertmanager_url = "http://127.0.0.1:19093"
    `))
	if err != nil {
		t.Fatal(err)
	}
	provider := targetProvider{defaultTarget: "default", targetName: "context"}
	tests := []struct {
		name         string
		arguments    map[string]interface{}
		prometheus   string
		alertmanager string
	}{
		{name: "uses defaults", arguments: map[string]interface{}{}, prometheus: "http://127.0.0.1:9090", alertmanager: "http://127.0.0.1:9093"},
		{name: "uses mapped context", arguments: map[string]interface{}{"context": "production"}, prometheus: "http://127.0.0.1:19090", alertmanager: "http://127.0.0.1:19093"},
		{name: "uses defaults for unmapped context", arguments: map[string]interface{}{"context": "unknown"}, prometheus: "http://127.0.0.1:9090", alertmanager: "http://127.0.0.1:9093"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			override, err := metricsConfigForTarget(api.ToolHandlerParams{BaseConfig: cfg, FilteringProvider: provider, ToolCallRequest: request{arguments: test.arguments}})
			if err != nil {
				t.Fatal(err)
			}
			extended, ok := override.GetToolsetConfig(obsmetricsconfig.ToolsetName)
			if !ok {
				t.Fatal("missing metrics configuration")
			}
			metricsConfig := extended.(*obsmetricsconfig.Config)
			if metricsConfig.PrometheusURL != test.prometheus || metricsConfig.AlertmanagerURL != test.alertmanager {
				t.Fatalf("unexpected endpoints: %#v", metricsConfig)
			}
		})
	}
}

type request struct{ arguments map[string]interface{} }

func (r request) GetArguments() map[string]interface{} { return r.arguments }
