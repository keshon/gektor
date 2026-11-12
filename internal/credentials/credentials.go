// Package credentials resolves environment references without protocol settings.
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
