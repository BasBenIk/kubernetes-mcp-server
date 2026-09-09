package core

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type ClusterHealthCheckSuite struct {
	suite.Suite
}

func (s *ClusterHealthCheckSuite) TestPromptIsRegistered() {
	prompts := (&Toolset{}).GetPrompts()
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

func TestClusterHealthCheckSuite(t *testing.T) {
	suite.Run(t, new(ClusterHealthCheckSuite))
}
