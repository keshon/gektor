package app

import (
	"os"
	"strings"

	"github.com/keshon/gektor/internal/llm"
)

// ResolveAPIKey attaches the bearer token to a remote client. Precedence
// is the flag, then the named env var, then the built-in lookup order.
// Local backends need no key and are left alone.
func ResolveAPIKey(flagVal, envName string, client *llm.Server) {
	if strings.TrimSpace(flagVal) != "" {
		client.WithAPIKey(strings.TrimSpace(flagVal))
		return
	}
	if strings.TrimSpace(envName) != "" {
		if v := strings.TrimSpace(os.Getenv(strings.TrimSpace(envName))); v != "" {
			client.WithAPIKey(v)
			return
		}
	}
	if v := llm.APIKeyFromEnv(); v != "" {
		client.WithAPIKey(v)
	}
}

// ExtraHeadersFromEnv supplies gateway-specific headers without new flags:
// OpenRouter recommends identifying the app on every request.
func ExtraHeadersFromEnv() map[string]string {
	out := map[string]string{}
	if v := strings.TrimSpace(os.Getenv("OPENROUTER_REFERER")); v != "" {
		out["HTTP-Referer"] = v
	} else if v := strings.TrimSpace(os.Getenv("GEKTOR_SITE_URL")); v != "" {
		out["HTTP-Referer"] = v
	}
	if v := strings.TrimSpace(os.Getenv("GEKTOR_SITE_TITLE")); v != "" {
		out["X-Title"] = v
	}
	return out
}
