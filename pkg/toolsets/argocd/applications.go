package argocd

import (
	"context"
	"fmt"
	"sort"

	"github.com/containers/kubernetes-mcp-server/pkg/api"
	"github.com/google/jsonschema-go/jsonschema"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
)

var applicationGVK = schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "Application"}
var applicationGVR = schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications"}

type applicationStatus struct {
	Name                 string `json:"name"`
	Namespace            string `json:"namespace"`
	Project              string `json:"project,omitempty"`
	SourceRepository     string `json:"source_repository,omitempty"`
	SourceRevision       string `json:"source_revision,omitempty"`
	SourcePath           string `json:"source_path,omitempty"`
	DestinationServer    string `json:"destination_server,omitempty"`
	DestinationNamespace string `json:"destination_namespace,omitempty"`
	SyncStatus           string `json:"sync_status,omitempty"`
	SyncRevision         string `json:"sync_revision,omitempty"`
	HealthStatus         string `json:"health_status,omitempty"`
	OperationPhase       string `json:"operation_phase,omitempty"`
	OperationMessage     string `json:"operation_message,omitempty"`
}

func applicationsTools(ctx context.Context, toolsetContext api.ToolsetContext) []api.ServerTool {
	return []api.ServerTool{{
		Tool: api.Tool{
			Name:        "argocd_applications_status",
			Description: "Summarize Argo CD Application fleet health and sync state. Use this to quickly identify drifted or unhealthy applications without retrieving full reconciliation histories.",
			InputSchema: &jsonschema.Schema{Type: "object", Properties: map[string]*jsonschema.Schema{
				"namespace":     {Type: "string", Description: "Namespace to inspect. Omit to inspect accessible Applications in all namespaces."},
				"labelSelector": {Type: "string", Description: "Optional Kubernetes label selector for Applications.", Pattern: "^([/_.\\-A-Za-z0-9=, ()!])+$"},
			}},
			Annotations: api.ToolAnnotations{Title: "Argo CD: Application Fleet Status", ReadOnlyHint: ptr.To(true), DestructiveHint: ptr.To(false), IdempotentHint: ptr.To(true), OpenWorldHint: ptr.To(true)},
		},
		Handler: applicationStatuses,
		RBAC: api.RBACBounded(api.RBACRequirement{
			Verbs:     []string{"list"},
			Target:    api.RBACTarget{Resource: &api.RBACResourceTarget{APIGroup: "argoproj.io", Resource: "applications"}},
			Namespace: &api.RBACNamespace{Argument: "namespace"},
		}),
		TargetCompatibilityFilters: []func() bool{func() bool {
			if !toolsetContext.TargetCompatibilityFiltersEnabled {
				return true
			}
			return api.AnyTargetHasGVK(ctx, toolsetContext.Inspector, applicationGVK)
		}},
	}}
}

func applicationStatuses(params api.ToolHandlerParams) (*api.ToolCallResult, error) {
	p := api.WrapParams(params)
	namespace := p.OptionalString("namespace", "")
	selector := p.OptionalString("labelSelector", "")
	if err := p.Err(); err != nil {
		return api.NewToolCallResultStructured(nil, fmt.Errorf("failed to list Argo CD Applications: %w", err)), nil
	}
	list, err := params.DynamicClient().Resource(applicationGVR).Namespace(namespace).List(params.Context, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return api.NewToolCallResultStructured(nil, fmt.Errorf("failed to list Argo CD Applications: %w", err)), nil
	}
	result := make([]applicationStatus, 0, len(list.Items))
	for i := range list.Items {
		result = append(result, summarizeApplication(&list.Items[i]))
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Namespace == result[j].Namespace {
			return result[i].Name < result[j].Name
		}
		return result[i].Namespace < result[j].Namespace
	})
	return api.NewToolCallResultStructured(map[string]any{"applications": result}, nil), nil
}

func summarizeApplication(application *unstructured.Unstructured) applicationStatus {
	result := applicationStatus{Name: application.GetName(), Namespace: application.GetNamespace()}
	result.Project, _, _ = unstructured.NestedString(application.Object, "spec", "project")
	result.SourceRepository, _, _ = unstructured.NestedString(application.Object, "spec", "source", "repoURL")
	result.SourceRevision, _, _ = unstructured.NestedString(application.Object, "spec", "source", "targetRevision")
	result.SourcePath, _, _ = unstructured.NestedString(application.Object, "spec", "source", "path")
	result.DestinationServer, _, _ = unstructured.NestedString(application.Object, "spec", "destination", "server")
	result.DestinationNamespace, _, _ = unstructured.NestedString(application.Object, "spec", "destination", "namespace")
	result.SyncStatus, _, _ = unstructured.NestedString(application.Object, "status", "sync", "status")
	result.SyncRevision, _, _ = unstructured.NestedString(application.Object, "status", "sync", "revision")
	result.HealthStatus, _, _ = unstructured.NestedString(application.Object, "status", "health", "status")
	result.OperationPhase, _, _ = unstructured.NestedString(application.Object, "status", "operationState", "phase")
	result.OperationMessage, _, _ = unstructured.NestedString(application.Object, "status", "operationState", "message")
	return result
}
