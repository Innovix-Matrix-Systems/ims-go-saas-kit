package config

import "testing"

func TestTracingFollowsTheOTLPEndpoint(t *testing.T) {
	for _, tc := range []struct {
		name     string
		env      map[string]string
		enabled  bool
		endpoint string
	}{
		{name: "off by default", env: map[string]string{}},
		{name: "an endpoint turns it on", env: map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:4318"}, enabled: true, endpoint: "http://localhost:4318"},
		{name: "the traces endpoint wins", env: map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://a:4318", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "http://b:4318/v1/traces"}, enabled: true, endpoint: "http://b:4318/v1/traces"},
		{name: "OTEL_ENABLED without an endpoint", env: map[string]string{"OTEL_ENABLED": "true"}, enabled: true},
		{name: "the SDK kill switch", env: map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:4318", "OTEL_SDK_DISABLED": "true"}, endpoint: "http://localhost:4318"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APP_ENV_FILE", "")
			for _, key := range []string{"OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "OTEL_ENABLED", "OTEL_SDK_DISABLED"} {
				t.Setenv(key, tc.env[key])
			}
			cfg, err := Load("")
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Tracing.Enabled != tc.enabled || cfg.Tracing.Endpoint != tc.endpoint {
				t.Errorf("Tracing = %+v, want enabled=%v endpoint=%q", cfg.Tracing, tc.enabled, tc.endpoint)
			}
		})
	}
}

func TestTracingSampler(t *testing.T) {
	t.Setenv("APP_ENV_FILE", "")
	t.Setenv("OTEL_TRACES_SAMPLER", "")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tracing.SampleRatio != 0.1 || cfg.Tracing.SamplerFromEnv {
		t.Errorf("default = %+v, want 10%% from the app", cfg.Tracing)
	}

	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "1")
	if cfg, err = Load(""); err != nil || cfg.Tracing.SampleRatio != 1 {
		t.Errorf("SAMPLER_ARG alone = %+v, %v; want ratio 1", cfg.Tracing, err)
	}

	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "2")
	if _, err := Load(""); err == nil {
		t.Error("OTEL_TRACES_SAMPLER_ARG=2 loaded, want an error")
	}

	t.Setenv("OTEL_TRACES_SAMPLER", "always_on")
	if cfg, err = Load(""); err != nil || !cfg.Tracing.SamplerFromEnv {
		t.Errorf("with OTEL_TRACES_SAMPLER = %+v, %v; want the SDK's sampler", cfg.Tracing, err)
	}
}
