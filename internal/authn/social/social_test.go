package social

import (
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/config"
)

func TestNewBuildsEnabledProvidersInOrder(t *testing.T) {
	registry := New(config.Social{
		Providers: []string{"github", "google"},
		Google:    config.GoogleLogin{ClientIDs: []string{"web"}},
		GitHub:    config.GitHubLogin{ClientID: "id", ClientSecret: "secret"},
	})

	var names []string
	for _, provider := range registry.List() {
		names = append(names, provider.Name())
	}
	if len(names) != 2 || names[0] != "github" || names[1] != "google" {
		t.Fatalf("providers = %v, want [github google]", names)
	}
	if _, ok := registry.Get("google"); !ok {
		t.Error("Get(google) not found")
	}
	if _, ok := registry.Get("facebook"); ok {
		t.Error("Get(facebook) found a provider that is not enabled")
	}
}

func TestNilRegistryHasNoProviders(t *testing.T) {
	var registry *Registry
	if _, ok := registry.Get("google"); ok || len(registry.List()) != 0 {
		t.Fatal("a nil registry must have no providers")
	}
}
