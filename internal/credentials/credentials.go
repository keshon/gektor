// Package credentials resolves environment references and protects opaque stored credentials.
package credentials

import (
	"fmt"
	"os"
	"regexp"
)

var environments = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func ValidEnvironment(name string) bool { return environments.MatchString(name) }

func Environment(name string) (string, error) {
	if !ValidEnvironment(name) {
		return "", fmt.Errorf("invalid credential environment name")
	}
	secret := os.Getenv(name)
	if secret == "" || len(secret) > 64<<10 {
		return "", fmt.Errorf("credential environment value missing or too large")
	}
	return secret, nil
}

// Source selects one live environment value or protected stored value.
type Source struct {
	Env       string `json:"env,omitempty"`
	Reference string `json:"reference,omitempty"`
}

func (s Source) Validate() error {
	if (s.Env == "") == (s.Reference == "") {
		return fmt.Errorf("credential requires exactly one environment or stored reference")
	}
	if s.Env != "" && !ValidEnvironment(s.Env) {
		return fmt.Errorf("invalid credential environment name")
	}
	if s.Reference != "" && !ValidReference(s.Reference) {
		return fmt.Errorf("invalid credential reference")
	}
	return nil
}

// Resolve returns caller-owned bytes; the source never retains a resolved value.
func (s Source) Resolve(store Store) ([]byte, error) {
	err := s.Validate()
	if err != nil {
		return nil, err
	}
	if s.Reference != "" {
		return store.Resolve(s.Reference)
	}
	value, err := Environment(s.Env)
	if err != nil {
		return nil, err
	}
	return []byte(value), nil
}
