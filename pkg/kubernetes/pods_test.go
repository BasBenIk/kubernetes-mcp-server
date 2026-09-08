package kubernetes

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/suite"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ResolveContainerSuite struct {
	suite.Suite
}

func (s *ResolveContainerSuite) TestResolveContainer() {
	s.Run("explicit container is returned as-is", func() {
		pod := &v1.Pod{
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{Name: "main"},
					{Name: "sidecar"},
				},
			},
		}
		s.Equal("explicit", resolveContainer(pod, "explicit"))
	})
	s.Run("single container pod returns that container", func() {
		pod := &v1.Pod{
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{Name: "only-container"},
				},
			},
		}
		s.Equal("only-container", resolveContainer(pod, ""))
	})
	s.Run("multi-container pod with annotation returns annotated container", func() {
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					DefaultContainerAnnotation: "sidecar",
				},
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{Name: "main"},
					{Name: "sidecar"},
				},
			},
		}
		s.Equal("sidecar", resolveContainer(pod, ""))
	})
	s.Run("multi-container pod without annotation falls back to first container", func() {
		pod := &v1.Pod{
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{Name: "first"},
					{Name: "second"},
				},
			},
		}
		s.Equal("first", resolveContainer(pod, ""))
	})
	s.Run("annotation with empty value falls back to first container", func() {
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					DefaultContainerAnnotation: "",
				},
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{Name: "first"},
					{Name: "second"},
				},
			},
		}
		s.Equal("first", resolveContainer(pod, ""))
	})
	s.Run("pod with no containers returns empty string", func() {
		pod := &v1.Pod{
			Spec: v1.PodSpec{
				Containers: []v1.Container{},
			},
		}
		s.Equal("", resolveContainer(pod, ""))
	})
	s.Run("explicit container takes precedence over annotation", func() {
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					DefaultContainerAnnotation: "sidecar",
				},
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{Name: "main"},
					{Name: "sidecar"},
				},
			},
		}
		s.Equal("main", resolveContainer(pod, "main"))
	})
}

func TestResolveContainer(t *testing.T) {
	suite.Run(t, new(ResolveContainerSuite))
}

type ExtractKubernetesErrorSuite struct {
	suite.Suite
}

func (s *ExtractKubernetesErrorSuite) TestExtractKubernetesError() {
	s.Run("returns ordinary errors unchanged", func() {
		original := fmt.Errorf("connection refused")
		s.Equal(original, extractKubernetesError(original))
	})
	s.Run("returns StatusError with a message unchanged", func() {
		err := &apierrors.StatusError{ErrStatus: metav1.Status{Message: "pods \"example\" not found", Code: 404}}
		s.Equal(err, extractKubernetesError(err))
	})
	s.Run("extracts the first API cause when the message is empty", func() {
		err := &apierrors.StatusError{ErrStatus: metav1.Status{
			Reason: metav1.StatusReasonBadRequest,
			Code:   400,
			Details: &metav1.StatusDetails{Causes: []metav1.StatusCause{
				{Message: ""},
				{Message: `container "app" in pod "example" not found`},
			}},
		}}
		result := extractKubernetesError(err)
		s.Contains(result.Error(), "BadRequest")
		s.Contains(result.Error(), `container "app" in pod "example" not found`)
	})
	s.Run("uses status reason and code when no API cause is available", func() {
		err := &apierrors.StatusError{ErrStatus: metav1.Status{Reason: metav1.StatusReasonBadRequest, Code: 400}}
		s.Equal("BadRequest (HTTP 400)", extractKubernetesError(err).Error())
	})
}

func TestExtractKubernetesError(t *testing.T) {
	suite.Run(t, new(ExtractKubernetesErrorSuite))
}
