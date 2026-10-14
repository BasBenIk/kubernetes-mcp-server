package core

import (
	"testing"

	"github.com/containers/kubernetes-mcp-server/pkg/api"
	"github.com/stretchr/testify/suite"
)

type ClusterHealthCheckSuite struct {
	suite.Suite
}

func (s *ClusterHealthCheckSuite) TestPromptIsRegistered() {
	prompts := (&Toolset{}).GetPrompts(s.T().Context(), api.ToolsetContext{})
	s.Require().Len(prompts, 2)

	s.Run("cluster health check accepts optional event setting", func() {
		prompt := prompts[0]
		s.Equal("cluster-health-check", prompt.Prompt.Name)
		s.Equal("Cluster Health Check", prompt.Prompt.Title)
		s.Contains(prompt.Prompt.Description, "bounded")
		s.Require().Len(prompt.Prompt.Arguments, 1)
		s.Equal("check_events", prompt.Prompt.Arguments[0].Name)
		s.False(prompt.Prompt.Arguments[0].Required)
		s.NotNil(prompt.Handler)
	})

	s.Run("namespace health check requires a namespace", func() {
		prompt := prompts[1]
		s.Equal("namespace-health-check", prompt.Prompt.Name)
		s.Equal("Namespace Health Check", prompt.Prompt.Title)
		s.Require().Len(prompt.Prompt.Arguments, 2)
		s.Equal("namespace", prompt.Prompt.Arguments[0].Name)
		s.True(prompt.Prompt.Arguments[0].Required)
		s.Equal("check_events", prompt.Prompt.Arguments[1].Name)
		s.False(prompt.Prompt.Arguments[1].Required)
		s.NotNil(prompt.Handler)
	})
}

func (s *ClusterHealthCheckSuite) TestPromptRBACMetadata() {
	prompts := (&Toolset{}).GetPrompts(s.T().Context(), api.ToolsetContext{})
	for _, prompt := range prompts {
		s.Run(prompt.Prompt.Name, func() {
			s.Require().NoError(prompt.RBAC.Validate())
			namespaceScoped := prompt.Prompt.Name == "namespace-health-check"
			for _, requirement := range prompt.RBAC.Bounded.Requirements {
				resource := requirement.Target.Resource.Resource
				s.NotEqual("clusteroperators", resource, "bounded fork health checks do not query ClusterOperators")
				if namespaceScoped && resource == "namespaces" {
					s.Equal([]string{"get"}, requirement.Verbs)
					s.Equal(&api.RBACResourceName{Argument: "namespace"}, requirement.ResourceName)
					continue
				}
				if namespaceScoped {
					s.NotEqual("nodes", resource)
					s.Equal(&api.RBACNamespace{Argument: "namespace"}, requirement.Namespace)
				} else if resource != "nodes" && resource != "namespaces" {
					s.Equal(&api.RBACNamespace{AllNamespaces: true}, requirement.Namespace)
				}
			}
		})
	}
}

func TestClusterHealthCheckSuite(t *testing.T) {
	suite.Run(t, new(ClusterHealthCheckSuite))
}
