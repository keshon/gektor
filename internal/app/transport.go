package app

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// ResolvedAPIKey selects the bearer token at composition. Precedence
// is the flag, then the named env var, then the built-in lookup order.
// Local backends need no key and are left alone.
func ResolvedAPIKey(flagVal, envName string) string {
	if v := strings.TrimSpace(flagVal); v != "" {
		return v
	}
	if name := strings.TrimSpace(envName); name != "" {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}
	return APIKeyFromEnv()
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

// APIKeyFromEnv returns the first bearer token found under the usual
// names, or "". Flag values win over this; this is the unattended path.
func APIKeyFromEnv() string {
	for _, name := range []string{
		"GEKTOR_API_KEY",
		"OPENAI_API_KEY",
		"OPENROUTER_API_KEY",
		"DEEPSEEK_API_KEY",
		"GROQ_API_KEY",
		"TOGETHER_API_KEY",
		"ANTHROPIC_API_KEY",
	} {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}
	return ""
}

// connectionTransport adds connection headers only for the configured origin.
// Redirects to other hosts must never acquire credentials through this wrapper.
type connectionTransport struct {
	base    http.RoundTripper
	origin  *url.URL
	key     string
	headers map[string]string
}

func (t connectionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	if r.URL.Scheme == t.origin.Scheme && r.URL.Host == t.origin.Host {
		for k, v := range t.headers {
			r.Header.Set(k, v)
		}
		if t.key != "" {
			r.Header.Set("Authorization", "Bearer "+t.key)
		}
	}
	return t.base.RoundTrip(r)
}
func connectionHTTP(endpoint, key string, headers map[string]string) *http.Client {
	origin, _ := url.Parse(endpoint)
	return &http.Client{Timeout: 120 * time.Minute, Transport: connectionTransport{base: http.DefaultTransport, origin: origin, key: key, headers: headers}}
}
