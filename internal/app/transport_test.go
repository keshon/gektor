package app

import (
	"runtime"
	"testing"

	"github.com/keshon/gektor/internal/providers"
)

func TestBearerAuthenticationIsScopedToConnection(t *testing.T) {
	homeVariable := "HOME"
	if runtime.GOOS == "windows" {
		homeVariable = "USERPROFILE"
	}
	t.Setenv(homeVariable, t.TempDir())
	t.Setenv("OPENROUTER_API_KEY", "unrelated-hosted-key")
	t.Setenv("GEKTOR_CONNECTION_TEST_KEY", "env-test-key")
	t.Setenv("GEKTOR_CONNECTION_TEST_MISSING", "")

	for _, kind := range providers.Kinds() {
		d, err := providers.Lookup(kind)
		if err != nil {
			t.Fatal(err)
		}
		if d.Auth != providers.Bearer {
			continue
		}

		t.Run(kind, func(t *testing.T) {
			endpoint := "https://gateway.example/v1"
			options := BackendOptions{Kind: kind, URL: endpoint}
			key, err := backendAPIKey(d, options)
			if err != nil || key != "" {
				t.Fatal("unconfigured connection resolved a key", err)
			}

			err = SaveProviderKey(kind, endpoint+"/", "first-test-key")
			if err != nil {
				t.Fatal(err)
			}
			err = SaveProviderKey(kind, endpoint, "replacement-test-key")
			if err != nil {
				t.Fatal(err)
			}
			options.KeyMode = providers.KeySaved
			key, err = backendAPIKey(d, options)
			if err != nil || key != "replacement-test-key" {
				t.Fatal("saved key was not resolved", err)
			}

			options.URL = "https://gateway.example/other"
			key, err = backendAPIKey(d, options)
			if err == nil || key != "" {
				t.Fatal("saved key resolved for another base URL")
			}

			options.KeyMode = providers.KeyNone
			key, err = backendAPIKey(d, options)
			if err != nil || key != "" {
				t.Fatal("None resolved a key", err)
			}

			options.KeyMode = providers.KeyEnv
			options.APIKeyEnv = "GEKTOR_CONNECTION_TEST_KEY"
			key, err = backendAPIKey(d, options)
			if err != nil || key != "env-test-key" {
				t.Fatal("named environment key was not resolved", err)
			}
			options.APIKeyEnv = "GEKTOR_CONNECTION_TEST_MISSING"
			key, err = backendAPIKey(d, options)
			if err == nil || key != "" {
				t.Fatal("missing explicit environment variable silently fell back")
			}
		})
	}
}
