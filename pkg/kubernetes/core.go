package kubernetes

import (
	"github.com/containers/kubernetes-mcp-server/pkg/api"
	"github.com/containers/kubernetes-mcp-server/pkg/redaction"
)

type Core struct {
	api.KubernetesClient
	redactor *redaction.Redactor
}

type CoreOption func(*Core)

func WithSecretRedaction(mode string) CoreOption {
	return func(core *Core) { core.redactor = redaction.New(mode) }
}

func NewCore(client api.KubernetesClient, options ...CoreOption) *Core {
	core := &Core{KubernetesClient: client}
	for _, option := range options {
		option(core)
	}
	return core
}
