package redaction

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type RedactionSuite struct{ suite.Suite }

func (s *RedactionSuite) TestApply() {
	s.Run("redacts Secret values and last applied annotation", func() {
		object := &unstructured.Unstructured{Object: map[string]interface{}{
			"kind": "Secret", "metadata": map[string]interface{}{"annotations": map[string]interface{}{"kubectl.kubernetes.io/last-applied-configuration": "plaintext", "safe": "value"}},
			"data": map[string]interface{}{"password": "secret"}, "stringData": map[string]interface{}{"token": "token"},
		}}
		New("opaque").Apply(object)
		data, _, _ := unstructured.NestedMap(object.Object, "data")
		stringData, _, _ := unstructured.NestedMap(object.Object, "stringData")
		s.Equal("[REDACTED]", data["password"])
		s.Equal("[REDACTED]", stringData["token"])
		s.NotContains(object.GetAnnotations(), "kubectl.kubernetes.io/last-applied-configuration")
		s.Equal("value", object.GetAnnotations()["safe"])
	})
	s.Run("uses deterministic per-process HMAC markers", func() {
		object := &unstructured.Unstructured{Object: map[string]interface{}{"kind": "Secret", "data": map[string]interface{}{"one": "same", "two": "same"}}}
		New("hashed").Apply(object)
		data, _, _ := unstructured.NestedMap(object.Object, "data")
		s.Equal(data["one"], data["two"])
		s.True(strings.HasPrefix(data["one"].(string), "[REDACTED:gen_"))
	})
	s.Run("uses the same marker across Redactor instances", func() {
		first := &unstructured.Unstructured{Object: map[string]interface{}{"kind": "Secret", "data": map[string]interface{}{"key": "same"}}}
		second := first.DeepCopy()
		New("hashed").Apply(first)
		New("hashed").Apply(second)
		s.Equal(first.Object["data"].(map[string]interface{})["key"], second.Object["data"].(map[string]interface{})["key"])
	})
	s.Run("does not change non-Secrets", func() {
		object := &unstructured.Unstructured{Object: map[string]interface{}{"kind": "ConfigMap", "data": map[string]interface{}{"key": "visible"}}}
		New("opaque").Apply(object)
		s.Equal("visible", object.Object["data"].(map[string]interface{})["key"])
	})
}

func TestRedaction(t *testing.T) { suite.Run(t, new(RedactionSuite)) }
