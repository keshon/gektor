package credentials

import (
	"strings"
	"testing"
)

func TestOriginBoundEnvironmentReference(t *testing.T) {
	s := Store{Directory: t.TempDir()}
	t.Setenv("GEKTOR_AUDIT_TOKEN", "not-for-logs")
	c := Config{Origin: "https://api.example.com", Env: "GEKTOR_AUDIT_TOKEN", Header: "Authorization", Prefix: "Bearer "}
	if err := s.Register("example", c); err != nil {
		t.Fatal(err)
	}
	header, value, secret, err := s.Resolve("example", "https://api.example.com/v1/items")
	if err != nil || header != "Authorization" || value != "Bearer not-for-logs" || secret != "not-for-logs" {
		t.Fatal(header, err)
	}
	for _, origin := range []string{"https://evil.example.com", "http://api.example.com", "https://api.example.com:444"} {
		if _, _, _, err := s.Resolve("example", origin); err == nil {
			t.Fatal("credential escaped origin", origin)
		}
	}
	if err := s.Register("example", c); err == nil {
		t.Fatal("overwrote existing credential")
	}
	if _, _, _, err := s.Resolve("../example", c.Origin); err == nil {
		t.Fatal("accepted traversal")
	}
	t.Setenv(c.Env, "bad\r\nsecret")
	if _, _, _, err := s.Resolve("example", c.Origin); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("invalid secret exposed", err)
	}
}
