package mcp

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"

	"github.com/containers/kubernetes-mcp-server/internal/test"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/suite"
)

type HealthCheckPromptSuite struct {
	BaseMcpSuite
}

const (
	healthCheckIssueRows   = 20
	healthCheckPromptRunes = 24000
)

func (s *HealthCheckPromptSuite) SetupTest() {
	s.BaseMcpSuite.SetupTest()
	s.InitMcpClient()
}

func (s *HealthCheckPromptSuite) TestClusterHealthCheck() {
	s.Run("reports unhealthy workloads from every namespace", func() {
		admin := kubernetes.NewForConfigOrDie(test.EnvTestRestConfig())
		_, err := admin.CoreV1().Namespaces().Create(s.T().Context(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "health-cluster-scope"}}, metav1.CreateOptions{})
		s.Require().NoError(err)
		s.T().Cleanup(func() {
			_ = admin.CoreV1().Namespaces().Delete(s.T().Context(), "health-cluster-scope", metav1.DeleteOptions{})
		})
		deployment, err := admin.AppsV1().Deployments("health-cluster-scope").Create(s.T().Context(), &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "unavailable"},
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr.To(int32(1)),
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "unavailable"}},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "unavailable"}},
					Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "registry.k8s.io/pause:3.10"}}},
				},
			},
			Status: appsv1.DeploymentStatus{Replicas: 1, UnavailableReplicas: 1},
		}, metav1.CreateOptions{})
		s.Require().NoError(err)
		deployment.Status = appsv1.DeploymentStatus{Replicas: 1, UnavailableReplicas: 1}
		_, err = admin.AppsV1().Deployments("health-cluster-scope").UpdateStatus(s.T().Context(), deployment, metav1.UpdateOptions{})
		s.Require().NoError(err)

		started := time.Now()
		result, getErr := s.GetPrompt("cluster-health-check", map[string]string{"check_events": "false"})
		s.Require().NoError(getErr)
		s.Less(time.Since(started), 5*time.Second)

		text := healthPromptText(s.T(), result)
		s.Contains(text, "health-cluster-scope/unavailable")
		s.Contains(text, "disabled by check_events=false")
	})

	s.Run("uses the bounded event workflow for an invalid event argument", func() {
		result, err := s.GetPrompt("cluster-health-check", map[string]string{"check_events": "invalid"})
		s.Require().NoError(err)
		s.Contains(healthPromptText(s.T(), result), "## Recent Warning and Error Events")
	})
}

func (s *HealthCheckPromptSuite) TestNamespaceHealthCheck() {
	admin := kubernetes.NewForConfigOrDie(test.EnvTestRestConfig())
	_, err := admin.CoreV1().Namespaces().Create(s.T().Context(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "health-namespace-scope"}}, metav1.CreateOptions{})
	s.Require().NoError(err)
	s.T().Cleanup(func() {
		_ = admin.CoreV1().Namespaces().Delete(s.T().Context(), "health-namespace-scope", metav1.DeleteOptions{})
	})
	_, err = admin.CoreV1().Pods("health-namespace-scope").Create(s.T().Context(), &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "failing-pod"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "registry.k8s.io/pause:3.10"}}},
		Status:     corev1.PodStatus{Phase: corev1.PodPending},
	}, metav1.CreateOptions{})
	s.Require().NoError(err)

	s.Run("limits output to the requested namespace", func() {
		result, getErr := s.GetPrompt("namespace-health-check", map[string]string{
			"namespace":    "health-namespace-scope",
			"check_events": "false",
		})
		s.Require().NoError(getErr)

		text := healthPromptText(s.T(), result)
		s.Contains(text, "health-namespace-scope/failing-pod")
		s.NotContains(text, "## Nodes")
		s.NotContains(text, "## Namespaces")
	})

	s.Run("does not fall back to cluster scope for a nonexistent namespace", func() {
		result, getErr := s.GetPrompt("namespace-health-check", map[string]string{"namespace": "does-not-exist"})
		s.Require().NoError(getErr)

		text := healthPromptText(s.T(), result)
		s.Contains(text, "namespaces \"does-not-exist\" not found")
		s.Contains(text, "namespace could not be verified")
		s.NotContains(text, "## Nodes")
	})
}

func (s *HealthCheckPromptSuite) TestHealthCheckOutputIsBoundedAndOrdered() {
	admin := kubernetes.NewForConfigOrDie(test.EnvTestRestConfig())
	const namespace = "health-output-limit"
	_, err := admin.CoreV1().Namespaces().Create(s.T().Context(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}, metav1.CreateOptions{})
	s.Require().NoError(err)
	s.T().Cleanup(func() {
		_ = admin.CoreV1().Namespaces().Delete(s.T().Context(), namespace, metav1.DeleteOptions{})
	})

	for index := 0; index < healthCheckIssueRows+1; index++ {
		name := fmt.Sprintf("pod-%02d", healthCheckIssueRows-index)
		_, err = admin.CoreV1().Pods(namespace).Create(s.T().Context(), &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "registry.k8s.io/pause:3.10"}}},
			Status:     corev1.PodStatus{Phase: corev1.PodPending},
		}, metav1.CreateOptions{})
		s.Require().NoError(err)
	}

	result, getErr := s.GetPrompt("namespace-health-check", map[string]string{
		"namespace":    namespace,
		"check_events": "false",
	})
	s.Require().NoError(getErr)
	text := healthPromptText(s.T(), result)

	s.Run("does not exceed the prompt size budget", func() {
		s.LessOrEqual(len([]rune(text)), healthCheckPromptRunes)
	})
	s.Run("sorts issue details and reports omissions", func() {
		s.Less(strings.Index(text, namespace+"/pod-00"), strings.Index(text, namespace+"/pod-01"))
		s.Contains(text, "1 additional issue(s) omitted")
	})
}

func (s *HealthCheckPromptSuite) TestHealthCheckReportsPartialCollection() {
	s.Close()
	s.McpClient = nil
	s.Cfg.DeniedResources = nil
	s.Require().NoError(toml.Unmarshal([]byte(`
[[denied_resources]]
group = ""
version = "v1"
kind = "Pod"
`), s.Cfg))
	s.InitMcpClient()

	result, err := s.GetPrompt("cluster-health-check", map[string]string{"check_events": "false"})
	s.Require().NoError(err)
	text := healthPromptText(s.T(), result)

	s.Contains(text, "**Pods:** collected 0 object(s); unavailable")
	s.Contains(text, "## Deployments")
}

func healthPromptText(t *testing.T, result *mcp.GetPromptResult) string {
	t.Helper()
	if result == nil || len(result.Messages) == 0 {
		t.Fatal("expected health prompt messages")
	}
	text, ok := result.Messages[0].Content.(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected text content, got %T", result.Messages[0].Content)
	}
	return strings.TrimSpace(text.Text)
}

func TestHealthCheckPromptSuite(t *testing.T) {
	suite.Run(t, new(HealthCheckPromptSuite))
}
