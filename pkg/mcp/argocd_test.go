package mcp

import (
	"testing"

	"github.com/containers/kubernetes-mcp-server/internal/test"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/suite"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

type ArgoCDSuite struct{ BaseMcpSuite }

func (s *ArgoCDSuite) SetupTest() {
	s.BaseMcpSuite.SetupTest()
	s.Cfg.Toolsets = append(s.Cfg.Toolsets, "argocd")
	dynamicClient := dynamic.NewForConfigOrDie(test.EnvTestRestConfig())
	_, err := dynamicClient.Resource(schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications"}).Namespace("default").Create(s.T().Context(), &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1", "kind": "Application",
		"metadata": map[string]any{"name": "drifted-app", "labels": map[string]any{"team": "platform"}},
		"spec":     map[string]any{"project": "default", "source": map[string]any{"repoURL": "https://example.test/platform.git", "targetRevision": "main", "path": "deploy"}, "destination": map[string]any{"server": "https://kubernetes.default.svc", "namespace": "platform"}},
		"status":   map[string]any{"sync": map[string]any{"status": "OutOfSync", "revision": "deadbeef"}, "health": map[string]any{"status": "Healthy"}, "operationState": map[string]any{"phase": "Succeeded"}},
	}}, metav1.CreateOptions{})
	s.Require().NoError(err)
}

func (s *ArgoCDSuite) TestApplicationStatuses() {
	s.InitMcpClient()
	result, err := s.CallTool("argocd_applications_status", map[string]interface{}{"namespace": "default", "labelSelector": "team=platform"})
	s.Require().NoError(err)
	s.False(result.IsError)
	structured, ok := result.StructuredContent.(map[string]interface{})
	s.Require().True(ok)
	applications, ok := structured["applications"].([]interface{})
	s.Require().True(ok)
	s.Require().Len(applications, 1)
	application := applications[0].(map[string]interface{})
	s.Equal("drifted-app", application["name"])
	s.Equal("OutOfSync", application["sync_status"])
	s.Equal("Healthy", application["health_status"])
	s.NotContains(result.Content[0].(*mcp.TextContent).Text, "resourceVersion")
}

func TestArgoCD(t *testing.T) { suite.Run(t, new(ArgoCDSuite)) }
