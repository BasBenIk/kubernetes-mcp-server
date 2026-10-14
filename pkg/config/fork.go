package config

import "fmt"

// ObservabilityMetricsEndpoint identifies explicitly configured metrics endpoints
// associated with one Kubernetes target. Tool arguments cannot override these URLs.
type ObservabilityMetricsEndpoint struct {
	PrometheusURL   string `toml:"prometheus_url"`
	AlertmanagerURL string `toml:"alertmanager_url"`
}

func validateRedactSecrets(mode string) error {
	if mode != "" && mode != "opaque" && mode != "hashed" {
		return fmt.Errorf("invalid redact_secrets %q: must be \"opaque\" or \"hashed\"", mode)
	}
	return nil
}
