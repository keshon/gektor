package ssh

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/keshon/gektor/internal/credentials"
)

type SecretKind string

const (
	PasswordSecret   SecretKind = "ssh-password"
	PassphraseSecret SecretKind = "ssh-key-passphrase"
)

type storedSecret struct {
	Kind  SecretKind `json:"kind"`
	Value string     `json:"value"`
}

func resolveSecret(store credentials.Store, env, ref string, kind SecretKind) (string, error) {
	if ref == "" {
		return credentials.Environment(env)
	}
	if !credentials.Encrypted() {
		return "", fmt.Errorf("encrypted SSH credential storage is unavailable")
	}
	if store.Directory == "" {
		return "", fmt.Errorf("SSH credential store is unavailable")
	}
	raw, err := store.Resolve(ref)
	if err != nil {
		return "", err
	}
	var secret storedSecret
	err = json.Unmarshal(raw, &secret)
	if err != nil || secret.Kind != kind || secret.Value == "" || len(secret.Value) > 16384 {
		return "", fmt.Errorf("SSH credential unavailable or wrong purpose")
	}
	return secret.Value, nil
}

// ValidateProfile checks a reference-only candidate without reading credentials or connecting.
func ValidateProfile(name string, profile Profile) error {
	return validateProfiles(map[string]Profile{name: profile})
}

// SaveWithSecret publishes a new reference only after the profile is valid.
// A failed profile save removes the new credential; existing shared references are retained.
func (s Profiles) SaveWithSecret(name string, profile Profile, kind SecretKind, value string) error {
	if s.Credentials.Directory == "" {
		return fmt.Errorf("SSH credential store is unavailable")
	}
	if !credentials.Encrypted() {
		return fmt.Errorf("encrypted SSH credential storage is unavailable; use environment references or key files")
	}
	if value == "" || len(value) > 16384 || !utf8.ValidString(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return fmt.Errorf("secret must contain 1–16384 bytes of valid text without control characters")
	}
	profiles, err := s.Load()
	if err != nil {
		return err
	}
	// A valid placeholder permits reference validation before writing any secret.
	placeholder := strings.Repeat("0", 32)
	switch kind {
	case PasswordSecret:
		profile.PasswordEnv, profile.PasswordRef = "", placeholder
	case PassphraseSecret:
		profile.PassphraseEnv, profile.PassphraseRef = "", placeholder
	default:
		return fmt.Errorf("invalid SSH credential purpose")
	}
	err = ValidateProfile(name, profile)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(storedSecret{Kind: kind, Value: value})
	if err != nil {
		return err
	}
	id, err := s.Credentials.Put(raw)
	if err != nil {
		return err
	}
	if kind == PasswordSecret {
		profile.PasswordRef = id
	} else {
		profile.PassphraseRef = id
	}
	profiles[name] = profile
	err = s.Save(profiles)
	if err != nil {
		cleanupErr := s.Credentials.Delete(id)
		if cleanupErr != nil {
			return fmt.Errorf("saving SSH profile: %w; removing unused credential: %v", err, cleanupErr)
		}
	}
	return err
}
