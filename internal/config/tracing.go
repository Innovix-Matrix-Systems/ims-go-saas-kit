package config

import (
	"fmt"
	"strconv"
	"strings"
)

// OpenTelemetry tracing (OTEL_*). The exporter itself reads the standard
// variables (OTEL_EXPORTER_OTLP_ENDPOINT, _HEADERS, OTEL_SERVICE_NAME, …);
// here only whether it runs and the default sampler.

// Tracing holds the tracing switch.
type Tracing struct {
	// Enabled: an OTLP endpoint is configured (or OTEL_ENABLED=true), and
	// OTEL_SDK_DISABLED is not true.
	Enabled bool
	// Endpoint is shown in `ims about` (empty: the exporter default).
	Endpoint string
	// SamplerFromEnv: OTEL_TRACES_SAMPLER is set, so the SDK's own sampler
	// applies and SampleRatio is not used.
	SamplerFromEnv bool
	// SampleRatio is the share of new traces kept by the default
	// parent-based sampler (OTEL_TRACES_SAMPLER_ARG, default 0.1).
	SampleRatio float64
}

func (c *Config) loadTracing() error {
	endpoint := strings.TrimSpace(getString("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", getString("OTEL_EXPORTER_OTLP_ENDPOINT", "")))
	enabled, err := getBool("OTEL_ENABLED", endpoint != "")
	if err != nil {
		return err
	}
	disabled, err := getBool("OTEL_SDK_DISABLED", false)
	if err != nil {
		return err
	}
	c.Tracing = Tracing{
		Enabled:        enabled && !disabled,
		Endpoint:       endpoint,
		SamplerFromEnv: strings.TrimSpace(getString("OTEL_TRACES_SAMPLER", "")) != "",
		SampleRatio:    0.1,
	}
	if c.Tracing.SamplerFromEnv {
		return nil // the SDK reads OTEL_TRACES_SAMPLER_ARG itself
	}
	if raw := strings.TrimSpace(getString("OTEL_TRACES_SAMPLER_ARG", "")); raw != "" {
		ratio, err := strconv.ParseFloat(raw, 64)
		if err != nil || ratio < 0 || ratio > 1 {
			return fmt.Errorf("OTEL_TRACES_SAMPLER_ARG: must be a number between 0 and 1")
		}
		c.Tracing.SampleRatio = ratio
	}
	return nil
}
