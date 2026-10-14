package config

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

func TestObservabilityMetricsContextEndpoints(t *testing.T) {
	cfg, err := ReadToml(t.Context(), []byte(`
        [observability_metrics_contexts.production]
        prometheus_url = "http://127.0.0.1:19090"
        alertmanager_url = "http://127.0.0.1:19093"

        [observability_metrics_contexts.staging]
        prometheus_url = "http://127.0.0.1:29090"
    `))
	if err != nil {
		t.Fatal(err)
	}
	production := cfg.ObservabilityMetricsContexts.Get()["production"]
	if production.PrometheusURL != "http://127.0.0.1:19090" || production.AlertmanagerURL != "http://127.0.0.1:19093" {
		t.Fatalf("unexpected production endpoints: %#v", production)
	}
	staging := cfg.ObservabilityMetricsContexts.Get()["staging"]
	if staging.PrometheusURL != "http://127.0.0.1:29090" || staging.AlertmanagerURL != "" {
		t.Fatalf("unexpected staging endpoints: %#v", staging)
	}
}

type ForkConfigSuite struct{ suite.Suite }

func (s *ForkConfigSuite) TestForkOptionsReload() {
	previous, err := ReadToml(s.T().Context(), []byte(`
		redact_secrets = "opaque"
		[observability_metrics_contexts.production]
		prometheus_url = "https://old.example.com"
	`), WithBaseDefault())
	s.Require().NoError(err)
	next, err := ReadToml(s.T().Context(), []byte(`
		redact_secrets = "hashed"
		[observability_metrics_contexts.production]
		prometheus_url = "https://new.example.com"
	`), WithBaseDefault(), WithPrevious(previous))
	s.Require().NoError(err)
	s.Run("updates redaction", func() { s.Equal("hashed", next.RedactSecrets.Get()) })
	s.Run("updates mapped endpoints", func() {
		s.Equal("https://new.example.com", next.ObservabilityMetricsContexts.Get()["production"].PrometheusURL)
	})
	s.Run("retains previous request configuration", func() {
		s.Equal("opaque", previous.RedactSecrets.Get())
		s.Equal("https://old.example.com", previous.ObservabilityMetricsContexts.Get()["production"].PrometheusURL)
	})
}

func (s *ForkConfigSuite) TestRejectsUnknownContextEndpointFields() {
	_, err := ReadToml(s.T().Context(), []byte(`
		[observability_metrics_contexts.production]
		promethues_url = "https://example.com"
	`), WithBaseDefault())
	s.Require().Error(err)
	s.Contains(err.Error(), "promethues_url")
}

func (s *ForkConfigSuite) TestEndpointCredentialsAreNotDescribed() {
	cfg, err := ReadToml(s.T().Context(), []byte(`
		[observability_metrics_contexts.production]
		prometheus_url = "https://user:secret@example.com"
	`), WithBaseDefault())
	s.Require().NoError(err)
	s.NotContains(cfg.ObservabilityMetricsContexts.Describe(), "secret@example.com")
	s.Contains(cfg.ObservabilityMetricsContexts.Describe(), "<redacted>")
}

func TestForkConfig(t *testing.T) { suite.Run(t, new(ForkConfigSuite)) }
