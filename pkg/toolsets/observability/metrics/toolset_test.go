package metrics_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/containers/kubernetes-mcp-server/pkg/api"
	"github.com/containers/kubernetes-mcp-server/pkg/config"
	"github.com/containers/kubernetes-mcp-server/pkg/kubernetes"
	"github.com/containers/kubernetes-mcp-server/pkg/toolsets/observability/metrics"
	obsmetrics "github.com/rhobs/obs-mcp/pkg/metrics"
	"github.com/stretchr/testify/suite"
	"k8s.io/client-go/rest"
)

type MetricsSuite struct{ suite.Suite }

func (s *MetricsSuite) labelTool() api.ServerTool {
	s.T().Helper()
	for _, tool := range (&metrics.Toolset{}).GetTools(s.T().Context(), api.ToolsetContext{}) {
		if tool.Tool.Name == "get_label_names" {
			return tool
		}
	}
	s.T().Fatal("get_label_names was not registered")
	return api.ServerTool{}
}

func (s *MetricsSuite) TestTargetRouting() {
	backend := func(marker string) *httptest.Server {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"status":"success","data":[%q]}`, marker)
		}))
		s.T().Cleanup(server.Close)
		return server
	}
	fallback := backend("fallback_metric")
	production := backend("production_metric")
	cfg, err := config.ReadToml(s.T().Context(), []byte(fmt.Sprintf(`
		[toolset_configs."observability/metrics"]
		prometheus_url = %q
		[observability_metrics_contexts.production]
		prometheus_url = %q
	`, fallback.URL, production.URL)))
	s.Require().NoError(err)
	k8s, err := kubernetes.NewKubernetes(s.T().Context(), cfg, nil, &rest.Config{Host: fallback.URL})
	s.Require().NoError(err)
	tool := s.labelTool()

	for _, tc := range []struct{ target, marker string }{
		{"", "fallback_metric"},
		{"production", "production_metric"},
		{"unmapped", "fallback_metric"},
	} {
		s.Run(tc.target+tc.marker, func() {
			result, err := tool.Handler(api.ToolHandlerParams{
				Context: s.T().Context(), Config: cfg, Target: tc.target,
				KubernetesClient: k8s, Request: request{},
			})
			s.Require().NoError(err)
			s.Require().NoError(result.Error)
			s.Contains(result.Content, tc.marker)
		})
	}

	s.Run("isolates concurrent targets", func() {
		type observation struct {
			marker string
			result *api.ToolCallResult
			err    error
		}
		const requests = 16
		observations := make(chan observation, requests)
		ctx := s.T().Context()
		for i := range requests {
			go func() {
				target, marker := "unmapped", "fallback_metric"
				if i%2 == 0 {
					target, marker = "production", "production_metric"
				}
				result, err := tool.Handler(api.ToolHandlerParams{
					Context: ctx, Config: cfg, Target: target,
					KubernetesClient: k8s, Request: request{},
				})
				observations <- observation{marker: marker, result: result, err: err}
			}()
		}
		for range requests {
			observed := <-observations
			s.Require().NoError(observed.err)
			s.Require().NoError(observed.result.Error)
			s.Contains(observed.result.Content, observed.marker)
		}
	})

	s.Run("does not mutate shared configuration", func() {
		extended, ok := cfg.GetToolsetConfig(obsmetrics.ToolsetName)
		s.Require().True(ok)
		s.Equal(fallback.URL, extended.(*obsmetrics.Config).PrometheusURL)
	})
	s.Run("supports cluster-aware endpoint selection", func() {
		s.True(tool.IsClusterAware())
	})
}

func (s *MetricsSuite) TestEndpointSafety() {
	tool := s.labelTool()
	for _, tc := range []struct{ name, toml, want string }{
		{"missing config", "", "configuration is required"},
		{"missing URL", `[toolset_configs."observability/metrics"]`, "prometheus_url is required"},
		{"TLS rejects HTTP", `require_tls = true
		[toolset_configs."observability/metrics"]
		prometheus_url = "http://localhost:9090"`, "secure scheme required"},
		{"TLS rejects insecure", `require_tls = true
		[toolset_configs."observability/metrics"]
		prometheus_url = "https://localhost:9090"
		insecure = true`, "insecure is true"},
		{"TLS checks context override", `require_tls = true
		[toolset_configs."observability/metrics"]
		prometheus_url = "https://localhost:9090"
		[observability_metrics_contexts.production]
		prometheus_url = "http://localhost:19090"`, "secure scheme required"},
	} {
		s.Run(tc.name, func() {
			cfg, err := config.ReadToml(s.T().Context(), []byte(tc.toml))
			s.Require().NoError(err)
			result, err := tool.Handler(api.ToolHandlerParams{
				Context: s.T().Context(), Config: cfg, Target: "production", Request: request{},
			})
			s.Require().NoError(err)
			s.Require().Error(result.Error)
			s.Contains(result.Error.Error(), tc.want)
		})
	}
}

type request struct{}

func (request) GetArguments() map[string]any { return map[string]any{} }

func TestMetrics(t *testing.T) { suite.Run(t, new(MetricsSuite)) }
