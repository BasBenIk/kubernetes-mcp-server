package mcp

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/suite"
)

type ObservabilityMetricsSuite struct{ BaseMcpSuite }

func (s *ObservabilityMetricsSuite) SetupTest() {
	s.BaseMcpSuite.SetupTest()
	s.Cfg.Toolsets = append(s.Cfg.Toolsets, "observability/metrics")
}

func (s *ObservabilityMetricsSuite) TestMetricsToolsRequireExplicitConfiguration() {
	s.InitMcpClient()
	result, err := s.CallTool("list_metrics", map[string]interface{}{"name_regex": "up"})
	s.Require().NoError(err)
	s.True(result.IsError)
	s.Contains(result.Content[0].(*mcp.TextContent).Text, "configuration is required")
}

func TestObservabilityMetrics(t *testing.T) {
	suite.Run(t, new(ObservabilityMetricsSuite))
}
