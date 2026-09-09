package mcp

import (
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/containers/kubernetes-mcp-server/internal/test"
)

// McpToolProcessingSuite tests MCP tool processing (isToolApplicable)
type McpToolProcessingSuite struct {
	BaseMcpSuite
}

func (s *McpToolProcessingSuite) TestUnrestricted() {
	s.InitMcpClient()

	tools, err := s.ListTools()
	s.Require().NotNil(tools)

	s.Run("ListTools returns tools", func() {
		s.NoError(err, "call ListTools failed")
		s.NotNilf(tools, "list tools failed")
	})

	s.Run("Destructive tools ARE NOT read only", func() {
		for _, tool := range tools.Tools {
			readOnly := tool.Annotations.ReadOnlyHint
			destructive := tool.Annotations.DestructiveHint != nil && *tool.Annotations.DestructiveHint
			s.Falsef(readOnly && destructive, "Tool %s is read-only and destructive, which is not allowed", tool.Name)
		}
	})
}

func (s *McpToolProcessingSuite) TestReadOnly() {
	s.Require().NoError(toml.Unmarshal([]byte(`
		read_only = true
	`), s.Cfg), "Expected to parse read only server config")
	s.InitMcpClient()

	tools, err := s.ListTools()
	s.Require().NotNil(tools)

	s.Run("ListTools returns tools", func() {
		s.NoError(err, "call ListTools failed")
		s.NotNilf(tools, "list tools failed")
	})

	s.Run("ListTools returns only read-only tools", func() {
		for _, tool := range tools.Tools {
			s.Truef(tool.Annotations.ReadOnlyHint,
				"Tool %s is not read-only but should be", tool.Name)
			s.Falsef(tool.Annotations.DestructiveHint != nil && *tool.Annotations.DestructiveHint,
				"Tool %s is destructive but should not be in read-only mode", tool.Name)
		}
	})
	s.Run("ListTools does not expose credential-bearing kubeconfig output", func() {
		for _, tool := range tools.Tools {
			s.NotEqual("configuration_view", tool.Name,
				"configuration_view can expose reusable kubeconfig credentials")
		}
	})
}

func (s *McpToolProcessingSuite) TestReadOnlyBlocksDirectWriteToolInvocation() {
	s.Require().NoError(toml.Unmarshal([]byte(`
		read_only = true
	`), s.Cfg), "Expected to parse read only server config")

	admin := kubernetes.NewForConfigOrDie(test.EnvTestRestConfig())
	_, err := admin.CoreV1().ConfigMaps("default").Create(s.T().Context(), &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "read-only-write-blocked"},
		Data:       map[string]string{"value": "original"},
	}, metav1.CreateOptions{})
	s.Require().NoError(err, "Expected to create ConfigMap test fixture")
	s.T().Cleanup(func() {
		_ = admin.CoreV1().ConfigMaps("default").Delete(s.T().Context(), "read-only-write-blocked", metav1.DeleteOptions{})
	})

	s.InitMcpClient()
	result, err := s.CallTool("resources_create_or_update", map[string]any{
		"resource": `apiVersion: v1
kind: ConfigMap
metadata:
  name: read-only-write-blocked
  namespace: default
data:
  value: mutated
`,
	})

	s.Run("direct write invocation is rejected", func() {
		s.True(err != nil || result.IsError, "write tool must be rejected even when invoked without listing tools first")
	})

	s.Run("target resource remains unchanged", func() {
		configMap, getErr := admin.CoreV1().ConfigMaps("default").Get(s.T().Context(), "read-only-write-blocked", metav1.GetOptions{})
		s.Require().NoError(getErr, "Expected ConfigMap test fixture to remain available")
		s.Equal("original", configMap.Data["value"], "read-only mode must not mutate cluster state")
	})
}

func (s *McpToolProcessingSuite) TestDisableDestructive() {
	s.Require().NoError(toml.Unmarshal([]byte(`
		disable_destructive = true
	`), s.Cfg), "Expected to parse disable destructive server config")
	s.InitMcpClient()

	tools, err := s.ListTools()
	s.Require().NotNil(tools)

	s.Run("ListTools returns tools", func() {
		s.NoError(err, "call ListTools failed")
		s.NotNilf(tools, "list tools failed")
	})

	s.Run("ListTools does not return destructive tools", func() {
		for _, tool := range tools.Tools {
			s.Falsef(tool.Annotations.DestructiveHint != nil && *tool.Annotations.DestructiveHint,
				"Tool %s is destructive but should not be in disable_destructive mode", tool.Name)
		}
	})
}

func (s *McpToolProcessingSuite) TestEnabledTools() {
	s.Require().NoError(toml.Unmarshal([]byte(`
		enabled_tools = [ "namespaces_list", "events_list" ]
	`), s.Cfg), "Expected to parse enabled tools server config")
	s.InitMcpClient()

	tools, err := s.ListTools()
	s.Require().NotNil(tools)

	s.Run("ListTools returns tools", func() {
		s.NoError(err, "call ListTools failed")
		s.NotNilf(tools, "list tools failed")
	})

	s.Run("ListTools returns only explicitly enabled tools", func() {
		s.Len(tools.Tools, 2, "ListTools should return exactly 2 tools")
		for _, tool := range tools.Tools {
			s.Falsef(tool.Name != "namespaces_list" && tool.Name != "events_list",
				"Tool %s is not enabled but should be", tool.Name)
		}
	})
}

func (s *McpToolProcessingSuite) TestDisabledTools() {
	s.Require().NoError(toml.Unmarshal([]byte(`
		disabled_tools = [ "namespaces_list", "events_list" ]
	`), s.Cfg), "Expected to parse disabled tools server config")
	s.InitMcpClient()

	tools, err := s.ListTools()
	s.Require().NotNil(tools)

	s.Run("ListTools returns tools", func() {
		s.NoError(err, "call ListTools failed")
		s.NotNilf(tools, "list tools failed")
	})

	s.Run("ListTools does not return disabled tools", func() {
		for _, tool := range tools.Tools {
			s.Falsef(tool.Name == "namespaces_list" || tool.Name == "events_list",
				"Tool %s is not disabled but should be", tool.Name)
		}
	})
}

func TestMcpToolProcessing(t *testing.T) {
	suite.Run(t, new(McpToolProcessingSuite))
}
