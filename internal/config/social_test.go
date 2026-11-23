package config

import (
	"strings"
	"testing"
)

func TestLoadSocial(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
		want    []string
	}{
		{name: "off by default"},
		{name: "google and github with credentials",
			env: map[string]string{
				"SOCIAL_PROVIDERS":            " Google, github ",
				"SOCIAL_GOOGLE_CLIENT_IDS":    "web-id, ios-id",
				"SOCIAL_GITHUB_CLIENT_ID":     "id",
				"SOCIAL_GITHUB_CLIENT_SECRET": "secret",
			},
			want: []string{"google", "github"}},
		{name: "google without client ids", env: map[string]string{"SOCIAL_PROVIDERS": "google"},
			wantErr: "SOCIAL_GOOGLE_CLIENT_IDS"},
		{name: "github without a secret",
			env:     map[string]string{"SOCIAL_PROVIDERS": "github", "SOCIAL_GITHUB_CLIENT_ID": "id"},
			wantErr: "SOCIAL_GITHUB_CLIENT_SECRET"},
		{name: "unknown provider", env: map[string]string{"SOCIAL_PROVIDERS": "myspace"}, wantErr: "not supported"},
		{name: "listed twice",
			env:     map[string]string{"SOCIAL_PROVIDERS": "google,Google", "SOCIAL_GOOGLE_CLIENT_IDS": "web-id"},
			wantErr: "twice"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("APP_ENV_FILE", "")
			for key, value := range tt.env {
				t.Setenv(key, value)
			}
			cfg, err := Load("")
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Load() error = %v, want one mentioning %s", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if strings.Join(cfg.Social.Providers, ",") != strings.Join(tt.want, ",") {
				t.Errorf("Providers = %v, want %v", cfg.Social.Providers, tt.want)
			}
		})
	}
}
