package credentials

import (
	"encoding/json"
	secrets "github.com/keshon/gektor/internal/credentials"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestUpdatesRequireCurrentRevisionAndKeepRevocation(t *testing.T) {
	store := Store{Directory: t.TempDir()}
	t.Setenv("GEKTOR_HTTP_UPDATE", "current-private-value")
	config := Config{Origin: "https://api.example.com", Env: "GEKTOR_HTTP_UPDATE", Header: "Authorization", Prefix: "Bearer "}
	err := store.Register("api", config)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.Inspect("api")
	if err != nil {
		t.Fatal(err)
	}

	config.Prefix = "Token "
	for _, revision := range []string{"", strings.Repeat("0", 64)} {
		err = store.Update("api", revision, config)
		if err == nil {
			t.Fatal("uninspected update accepted")
		}
	}
	err = store.Update("api", before.Revision, config)
	if err != nil {
		t.Fatal(err)
	}
	after, err := store.Inspect("api")
	if err != nil || after.Revision == before.Revision {
		t.Fatal(after, err)
	}
	_, _, _, err = store.Resolve("api", config.Origin, before.Revision)
	if err == nil {
		t.Fatal("old revision followed update")
	}
	_, header, _, err := store.Resolve("api", config.Origin, after.Revision)
	if err != nil || header != "Token current-private-value" {
		t.Fatal("updated binding did not resolve", err)
	}
	err = store.Update("api", before.Revision, config)
	if err == nil {
		t.Fatal("stale writer overwrote current binding")
	}

	err = store.Revoke("api")
	if err != nil {
		t.Fatal(err)
	}
	config.Prefix = "Changed "
	err = store.Update("api", after.Revision, config)
	if err == nil {
		t.Fatal("update revived revoked name")
	}
}

func TestConcurrentUpdatesAcceptOnlyOneInspectedRevision(t *testing.T) {
	store := Store{Directory: t.TempDir()}
	config := Config{Origin: "https://api.example.com", Env: "GEKTOR_HTTP_UPDATE", Header: "Authorization"}
	err := store.Register("api", config)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.Inspect("api")
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	var workers sync.WaitGroup
	failures := make([]error, 2)
	for i, prefix := range []string{"first ", "second "} {
		workers.Go(func() {
			<-start
			replacement := config
			replacement.Prefix = prefix
			independent := Store{Directory: store.Directory}
			failures[i] = independent.Update("api", before.Revision, replacement)
		})
	}
	close(start)
	workers.Wait()

	successes := 0
	for _, failure := range failures {
		if failure == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatal("concurrent writers accepted the same revision", failures)
	}
}

func TestStoredUpdateCleansRejectedValueAndRetainsPreviousSource(t *testing.T) {
	store := Store{Directory: t.TempDir()}
	config := Config{Origin: "https://api.example.com", Header: "X-API-Key"}
	err := store.RegisterStored("api", config, []byte("old-private-value"))
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.Inspect("api")
	if err != nil {
		t.Fatal(err)
	}
	old, _, err := store.read("api")
	if err != nil {
		t.Fatal(err)
	}

	err = store.UpdateStored("api", before.Revision, config, []byte("new-private-value"))
	if err != nil {
		t.Fatal(err)
	}
	after, err := store.Inspect("api")
	if err != nil || after.Revision == before.Revision {
		t.Fatal(after, err)
	}
	_, _, value, err := store.Resolve("api", config.Origin, after.Revision)
	if err != nil || value != "new-private-value" {
		t.Fatal("new stored value unavailable", err)
	}
	protected := secrets.Store{Directory: store.Directory}
	_, err = protected.Resolve(old.Reference)
	if err != nil {
		t.Fatal("update deleted a potentially shared old source", err)
	}

	entries, err := os.ReadDir(store.Directory)
	if err != nil {
		t.Fatal(err)
	}
	err = store.UpdateStored("api", before.Revision, config, []byte("rejected-private-value"))
	if err == nil {
		t.Fatal("stale stored update accepted")
	}
	remaining, err := os.ReadDir(store.Directory)
	if err != nil || len(remaining) != len(entries) {
		t.Fatal("rejected update retained a protected value", err)
	}
}

func TestOriginBoundEnvironmentReference(t *testing.T) {
	s := Store{Directory: t.TempDir()}
	t.Setenv("GEKTOR_AUDIT_TOKEN", "not-for-logs")
	c := Config{Origin: "https://api.example.com", Env: "GEKTOR_AUDIT_TOKEN", Header: "Authorization", Prefix: "Bearer "}
	if err := s.Register("example", c); err != nil {
		t.Fatal(err)
	}
	binding, err := s.Inspect("example")
	if err != nil {
		t.Fatal(err)
	}
	header, value, secret, err := s.Resolve("example", "https://api.example.com/v1/items", binding.Revision)
	if err != nil || header != "Authorization" || value != "Bearer not-for-logs" || secret != "not-for-logs" {
		t.Fatal(header, err)
	}
	for _, origin := range []string{"https://evil.example.com", "http://api.example.com", "https://api.example.com:444"} {
		if _, _, _, err := s.Resolve("example", origin, binding.Revision); err == nil {
			t.Fatal("credential escaped origin", origin)
		}
	}
	if err := s.Register("example", c); err == nil {
		t.Fatal("overwrote existing credential")
	}
	if _, _, _, err := s.Resolve("../example", c.Origin, binding.Revision); err == nil {
		t.Fatal("accepted traversal")
	}
	t.Setenv(c.Env, "bad\r\nsecret")
	if _, _, _, err := s.Resolve("example", c.Origin, binding.Revision); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("invalid secret exposed", err)
	}
}

func TestBindingPinsMetadataButResolvesCurrentSecret(t *testing.T) {
	store := Store{Directory: t.TempDir()}
	t.Setenv("GEKTOR_BINDING_TEST", "first-private-value")
	config := Config{Origin: "https://api.example.com", Env: "GEKTOR_BINDING_TEST", Header: "Authorization", Prefix: "Bearer "}
	err := store.Register("api", config)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := store.Inspect("api")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.Env, "rotated-private-value")
	_, _, secret, err := store.Resolve("api", config.Origin, binding.Revision)
	if err != nil || secret != "rotated-private-value" {
		t.Fatal("secret rotation unavailable", err)
	}
	current, err := store.Inspect("api")
	if err != nil || current.Revision != binding.Revision {
		t.Fatal("secret changed binding revision", err)
	}
	_, _, _, err = store.Resolve("api", config.Origin, "")
	if err == nil {
		t.Fatal("unpinned binding accepted")
	}

	config.Header = "X-API-Key"
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(store.Directory, "api.json"), raw, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = store.Resolve("api", config.Origin, binding.Revision)
	if err == nil {
		t.Fatal("changed binding used old approval")
	}
	changed, err := store.Inspect("api")
	if err != nil || changed.Revision == binding.Revision {
		t.Fatal("binding change not visible", err)
	}

	err = store.Revoke("api")
	if err != nil {
		t.Fatal(err)
	}
	err = store.Revoke("api")
	if err != nil {
		t.Fatal("repeated revocation failed", err)
	}
	changed, err = store.Inspect("api")
	if err != nil || !changed.Revoked {
		t.Fatal("revocation not visible", err)
	}
	_, _, _, err = store.Resolve("api", config.Origin, changed.Revision)
	if err == nil {
		t.Fatal("revoked binding resolved")
	}
	err = os.Remove(filepath.Join(store.Directory, "api.json"))
	if err != nil {
		t.Fatal(err)
	}
	err = store.Register("api", config)
	if err == nil {
		t.Fatal("revoked name reused")
	}
}

func TestStoredBindingSeparatesSecretsAndCleansFailedRegistration(t *testing.T) {
	store := Store{Directory: t.TempDir()}
	config := Config{Origin: "https://api.example.com", Header: "X-API-Key"}
	value := "private-stored-value"
	err := store.RegisterStored("api", config, []byte(value))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := store.Inspect("api")
	if err != nil || binding.Source != "stored" || binding.Protection != secrets.Protection() {
		t.Fatal("stored metadata unavailable", err)
	}
	metadata, err := json.Marshal(binding)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(store.Directory, "api.json"))
	if err != nil || strings.Contains(string(raw), value) || strings.Contains(string(metadata), value) {
		t.Fatal("secret leaked into binding metadata", err)
	}
	_, resolved, _, err := store.Resolve("api", config.Origin, binding.Revision)
	if err != nil || resolved != value {
		t.Fatal("stored binding unavailable", err)
	}
	before, err := os.ReadDir(store.Directory)
	if err != nil {
		t.Fatal(err)
	}
	err = store.RegisterStored("api", config, []byte("another-private-value"))
	if err == nil {
		t.Fatal("binding overwritten")
	}
	after, err := os.ReadDir(store.Directory)
	if err != nil || len(after) != len(before) {
		t.Fatal("failed registration left a secret file", err)
	}
}

func TestBindingRejectsMalformedMetadataWithoutEchoingIt(t *testing.T) {
	store := Store{Directory: t.TempDir()}
	for _, raw := range []string{
		`{"origin":"https://api.example.com","header":"Authorization","env":"TOKEN","secret":"do-not-echo"}`,
		`{"origin":"https://api.example.com","header":"Authorization","env":"TOKEN"} {}`,
		strings.Repeat(" ", 8193),
	} {
		err := os.WriteFile(filepath.Join(store.Directory, "bad.json"), []byte(raw), 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, err = store.Inspect("bad")
		if err == nil || strings.Contains(err.Error(), "do-not-echo") {
			t.Fatal("invalid binding accepted or leaked", err)
		}
	}
}

func TestHTTPNameAndOpaqueReferenceRevocationsAreIndependent(t *testing.T) {
	store := Store{Directory: t.TempDir()}
	protected := secrets.Store{Directory: store.Directory}
	id, err := protected.Put([]byte("independent-private-value"))
	if err != nil {
		t.Fatal(err)
	}
	config := Config{Origin: "https://api.example.com", Env: "HTTP_NAMESPACE_TEST", Header: "Authorization"}
	err = store.Register(id, config)
	if err != nil {
		t.Fatal(err)
	}
	err = store.Revoke(id)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := protected.Resolve(id)
	if err != nil || string(raw) != "independent-private-value" {
		t.Fatal("HTTP name revocation escaped its namespace", err)
	}
}

func TestStoredHTTPSecretCannotBeReboundToAnotherTargetOrPurpose(t *testing.T) {
	for _, mode := range []string{"origin", "header", "purpose"} {
		t.Run(mode, func(t *testing.T) {
			store := Store{Directory: t.TempDir()}
			config := Config{Origin: "https://api.example.com", Header: "Authorization"}
			err := store.RegisterStored("api", config, []byte("private-bound-value"))
			if err != nil {
				t.Fatal(err)
			}
			config, _, err = store.read("api")
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "origin":
				config.Origin = "https://other.example.com"
			case "header":
				config.Header = "X-API-Key"
			case "purpose":
				protected := secrets.Store{Directory: store.Directory}
				config.Reference, err = protected.Put([]byte(`{"kind":"ssh-password","value":"private-bound-value"}`))
				if err != nil {
					t.Fatal(err)
				}
			}
			raw, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			err = os.WriteFile(filepath.Join(store.Directory, "api.json"), raw, 0600)
			if err != nil {
				t.Fatal(err)
			}
			binding, err := store.Inspect("api")
			if err != nil {
				t.Fatal(err)
			}

			_, _, _, err = store.Resolve("api", config.Origin, binding.Revision)
			if err == nil || strings.Contains(err.Error(), "private-bound-value") {
				t.Fatal("stored binding bypassed or leaked", err)
			}
		})
	}
}
