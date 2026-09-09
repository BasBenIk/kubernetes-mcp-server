package config

import "testing"

func TestObservabilityMetricsContextEndpoints(t *testing.T) {
	cfg, err := ReadToml([]byte(`
        [observability_metrics_contexts.production]
        prometheus_url = "http://127.0.0.1:19090"
        alertmanager_url = "http://127.0.0.1:19093"

        [observability_metrics_contexts.staging]
        prometheus_url = "http://127.0.0.1:29090"
    `))
	if err != nil {
		t.Fatal(err)
	}
	production := cfg.GetObservabilityMetricsEndpoints()["production"]
	if production.PrometheusURL != "http://127.0.0.1:19090" || production.AlertmanagerURL != "http://127.0.0.1:19093" {
		t.Fatalf("unexpected production endpoints: %#v", production)
	}
	staging := cfg.GetObservabilityMetricsEndpoints()["staging"]
	if staging.PrometheusURL != "http://127.0.0.1:29090" || staging.AlertmanagerURL != "" {
		t.Fatalf("unexpected staging endpoints: %#v", staging)
	}
}
