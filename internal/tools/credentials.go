package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

var envIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Resolve only explicitly named credentials, at execution/connection time.
func adapterEnv(refs map[string]string) ([]string, []string, error) {
	env := scrubEnv(os.Environ())
	var secrets []string
	// Remove sources and destinations before appending any resolved values.
	// Otherwise overlapping references depend on map iteration order.
	remove := map[string]bool{}
	keys := make([]string, 0, len(refs))
	for name, ref := range refs {
		remove[strings.ToUpper(name)], remove[strings.ToUpper(ref)] = true, true
		keys = append(keys, name)
	}
	for i := len(env) - 1; i >= 0; i-- {
		key, _, _ := strings.Cut(env[i], "=")
		if remove[strings.ToUpper(key)] {
			env = append(env[:i], env[i+1:]...)
		}
	}
	sort.Strings(keys)
	for _, name := range keys {
		ref := refs[name]
		value, ok := os.LookupEnv(ref)
		if !ok || value == "" {
			return nil, nil, fmt.Errorf("adapter credential environment variable %s is not set", ref)
		}
		env = append(env, name+"="+value)
		secrets = append(secrets, value)
	}
	return env, secrets, nil
}

// Cover literal and JSON-escaped values. Adapters must also avoid logging credentials.
func redactCredentials(text string, secrets []string) string {
	for _, value := range secrets {
		if value == "" {
			continue
		}
		encoded, _ := json.Marshal(value)
		text = strings.ReplaceAll(text, string(encoded[1:len(encoded)-1]), "[redacted]")
		text = strings.ReplaceAll(text, value, "[redacted]")
	}
	return text
}

type credentialError struct {
	err     error
	secrets []string
}

func (e credentialError) Error() string { return redactCredentials(e.err.Error(), e.secrets) }
func (e credentialError) Unwrap() error { return e.err }
func redactCredentialError(err error, secrets []string) error {
	if err == nil || len(secrets) == 0 {
		return err
	}
	return credentialError{err, secrets}
}
