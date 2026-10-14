package mcp

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/containers/kubernetes-mcp-server/internal/test"
	"github.com/containers/kubernetes-mcp-server/pkg/config/configtest"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/suite"
	"k8s.io/client-go/tools/clientcmd"
)

type ObservabilityMetricsSuite struct{ BaseMcpSuite }

func (s *ObservabilityMetricsSuite) SetupTest() {
	s.BaseMcpSuite.SetupTest()
	s.Cfg.Toolsets.SetForTest(append(s.Cfg.Toolsets.Get(), "observability/metrics"))
}

func (s *ObservabilityMetricsSuite) TestMetricsToolsRequireExplicitConfiguration() {
	s.InitMcpClient()
	result, err := s.CallTool("list_metrics", map[string]interface{}{"name_regex": "up"})
	s.Require().NoError(err)
	s.True(result.IsError)
	s.Contains(result.Content[0].(*mcp.TextContent).Text, "configuration is required")
}

func (s *ObservabilityMetricsSuite) TestMetricsEndpointsFollowSelectedContext() {
	backend := func(marker string) *httptest.Server {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"status":"success","data":[%q]}`, marker)
		}))
		s.T().Cleanup(server.Close)
		return server
	}
	fallback, production := backend("fallback_metric"), backend("production_metric")
	kubeconfig, err := clientcmd.LoadFromFile(s.Cfg.KubeConfig.Get())
	s.Require().NoError(err)
	kubeconfig.Contexts["production"] = kubeconfig.Contexts[kubeconfig.CurrentContext].DeepCopy()
	s.Cfg.KubeConfig.SetForTest(test.KubeconfigFile(s.T(), kubeconfig))
	configtest.OverlayTOML(s.T(), &s.Cfg, fmt.Sprintf(`
		[toolset_configs."observability/metrics"]
		prometheus_url = %q
		[observability_metrics_contexts.production]
		prometheus_url = %q
	`, fallback.URL, production.URL))
	s.InitMcpClient()

	for _, arguments := range []map[string]any{{}, {"context": ""}, {"context": " "}, {"context": 42}} {
		s.Run(fmt.Sprintf("rejects missing or invalid context %v", arguments), func() {
			result, err := s.CallTool("get_label_names", arguments)
			s.Require().NoError(err)
			s.Require().True(result.IsError)
			s.Contains(result.Content[0].(*mcp.TextContent).Text, "context parameter is required")
		})
	}
	for _, tc := range []struct{ target, marker string }{
		{kubeconfig.CurrentContext, "fallback_metric"},
		{"production", "production_metric"},
		{kubeconfig.CurrentContext, "fallback_metric"},
	} {
		s.Run(tc.target+tc.marker, func() {
			result, err := s.CallTool("get_label_names", map[string]any{"context": tc.target})
			s.Require().NoError(err)
			s.Require().False(result.IsError)
			s.Contains(result.Content[0].(*mcp.TextContent).Text, tc.marker)
		})
	}
}

func TestObservabilityMetrics(t *testing.T) {
	suite.Run(t, new(ObservabilityMetricsSuite))
}
