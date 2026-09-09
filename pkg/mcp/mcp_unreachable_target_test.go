package mcp

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/containers/kubernetes-mcp-server/internal/test"
	"github.com/containers/kubernetes-mcp-server/pkg/config"
	"github.com/containers/kubernetes-mcp-server/pkg/kubernetes"
	"github.com/stretchr/testify/suite"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// UnreachableKubeconfigTargetSuite verifies that optional kubeconfig contexts
// do not delay server startup until a request explicitly selects one.
type UnreachableKubeconfigTargetSuite struct {
	suite.Suite
}

func (s *UnreachableKubeconfigTargetSuite) TestServerStartsWithoutContactingUnselectedTarget() {
	defaultServer := test.NewMockServer()
	defaultServer.Handle(test.NewDiscoveryClientHandler())
	s.T().Cleanup(defaultServer.Close)

	unreachableRequests := make(chan struct{}, 1)
	unreachableServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case unreachableRequests <- struct{}{}:
		default:
		}
		http.Error(w, "unreachable target was contacted during startup", http.StatusServiceUnavailable)
	}))
	s.T().Cleanup(unreachableServer.Close)

	kubeconfig := defaultServer.Kubeconfig()
	kubeconfig.Clusters["unreachable"] = &clientcmdapi.Cluster{Server: unreachableServer.URL}
	kubeconfig.AuthInfos["unreachable"] = &clientcmdapi.AuthInfo{}
	kubeconfig.Contexts["unreachable"] = &clientcmdapi.Context{
		Cluster:  "unreachable",
		AuthInfo: "unreachable",
	}

	cfg := config.BaseDefault()
	cfg.KubeConfig = test.KubeconfigFile(s.T(), kubeconfig)
	provider, err := kubernetes.NewProvider(s.T().Context(), cfg)
	s.Require().NoError(err)
	s.T().Cleanup(provider.Close)

	start := time.Now()
	server, err := NewServer(s.T().Context(), Configuration{StaticConfig: cfg}, provider)
	s.Require().NoError(err)
	s.T().Cleanup(server.Close)
	s.Less(time.Since(start), 2*time.Second, "unselected kubeconfig contexts must not delay server startup")

	select {
	case <-unreachableRequests:
		s.Fail("unselected target must not be contacted during startup")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestUnreachableKubeconfigTargetSuite(t *testing.T) {
	suite.Run(t, new(UnreachableKubeconfigTargetSuite))
}
