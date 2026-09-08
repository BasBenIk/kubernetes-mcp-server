package redaction

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const lastAppliedConfiguration = "kubectl.kubernetes.io/last-applied-configuration"

// Redactor removes Secret values from objects returned to MCP clients.
type Redactor struct {
	mode       string
	key        []byte
	generation string
}

var (
	processKey        []byte
	processGeneration string
	processKeyOnce    sync.Once
)

func New(mode string) *Redactor {
	if mode == "" {
		return nil
	}
	processKeyOnce.Do(func() {
		processKey = make([]byte, 32)
		generation := make([]byte, 4)
		if _, err := rand.Read(processKey); err != nil {
			panic(fmt.Sprintf("generate redaction key: %v", err))
		}
		if _, err := rand.Read(generation); err != nil {
			panic(fmt.Sprintf("generate redaction generation: %v", err))
		}
		processGeneration = hex.EncodeToString(generation)
	})
	return &Redactor{mode: mode, key: processKey, generation: processGeneration}
}

func (r *Redactor) Apply(object *unstructured.Unstructured) {
	if r == nil || object == nil || object.GetKind() != "Secret" {
		return
	}
	r.redactMap(object.Object, "data")
	r.redactMap(object.Object, "stringData")
	annotations := object.GetAnnotations()
	delete(annotations, lastAppliedConfiguration)
	object.SetAnnotations(annotations)
}

func (r *Redactor) ApplyList(list *unstructured.UnstructuredList) {
	if r == nil || list == nil {
		return
	}
	for i := range list.Items {
		r.Apply(&list.Items[i])
	}
}

func (r *Redactor) redactMap(object map[string]interface{}, field string) {
	values, ok := object[field].(map[string]interface{})
	if !ok {
		return
	}
	for name, value := range values {
		if r.mode == "hashed" {
			mac := hmac.New(sha256.New, r.key)
			_, _ = fmt.Fprint(mac, value)
			values[name] = fmt.Sprintf("[REDACTED:gen_%s:%s]", r.generation, hex.EncodeToString(mac.Sum(nil))[:16])
			continue
		}
		values[name] = "[REDACTED]"
	}
}
