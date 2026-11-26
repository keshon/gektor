package app

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/credentials"
	"github.com/keshon/gektor/internal/httptransport"
	"github.com/keshon/gektor/internal/providers"
	"github.com/keshon/gektor/internal/userdirs"
)

const previousProviderKeySlot = "00000000000000000000000000000000"

func providerKeyStore(provider providers.Descriptor) credentials.Store {
	return credentials.Store{Directory: userdirs.Sub("credentials", "providers", provider.ID)}
}

func providerKeySlot(endpoint string) string {
	// Callers validate the endpoint before deriving the slot.
	parsed, _ := url.Parse(endpoint)
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	identity := strings.TrimRight(parsed.String(), "/")
	hash := sha256.Sum256([]byte(identity))
	return fmt.Sprintf("%x", hash[:16])
}

func SaveProviderKey(kind, endpoint, value string) error {
	d, err := providers.Lookup(kind)
	if err != nil {
		return err
	}
	if d.Auth != providers.Bearer {
		return fmt.Errorf("provider does not support an API key")
	}
	err = d.ValidateEndpoint(endpoint)
	if err != nil {
		return err
	}

	value = strings.TrimSpace(value)
	if value == "" || strings.IndexFunc(value, func(r rune) bool { return r <= ' ' || r >= 127 }) >= 0 {
		return fmt.Errorf("paste one API key")
	}

	store := providerKeyStore(d)
	return store.Replace(providerKeySlot(endpoint), []byte(value))
}

// CheckProviderAuthentication catches an unusable credential before a TUI relaunch.
func CheckProviderAuthentication(o BackendOptions) error {
	d, err := providers.Lookup(o.Kind)
	if err != nil || d.Auth != providers.Bearer {
		return err
	}

	err = d.ValidateEndpoint(o.URL)
	if err != nil {
		return err
	}
	_, err = backendAPIKey(d, o)
	return err
}

func backendAPIKey(d providers.Descriptor, o BackendOptions) (string, error) {
	if key := strings.TrimSpace(o.APIKey); key != "" {
		return key, nil
	}

	name := strings.TrimSpace(o.APIKeyEnv)
	mode := o.KeyMode
	if mode == "" {
		mode = d.DefaultKeyMode(o.URL, name)
	}
	switch mode {
	case providers.KeyNone:
		return "", nil
	case providers.KeyEnv:
		if !credentials.ValidEnvironment(name) {
			return "", fmt.Errorf("enter a valid API key environment variable name")
		}
		key := strings.TrimSpace(os.Getenv(name))
		if key == "" {
			return "", fmt.Errorf("API key environment variable %s is empty or unset", name)
		}
		return key, nil
	case "":
		key := strings.TrimSpace(os.Getenv(d.DefaultKeyEnv))
		if key != "" {
			return key, nil
		}
	case providers.KeySaved:
	default:
		return "", fmt.Errorf("invalid authentication mode")
	}

	store := providerKeyStore(d)
	raw, err := store.Resolve(providerKeySlot(o.URL))
	if err != nil && d.DefaultKeyEnv != "" && strings.TrimSuffix(o.URL, "/") == d.DefaultURL {
		raw, err = store.Resolve(previousProviderKeySlot)
	}
	if err != nil {
		if mode == "" {
			return "", nil
		}
		return "", fmt.Errorf("no usable API key saved for %s at this address", d.Label)
	}
	defer clear(raw)
	return string(raw), nil
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
	return &http.Client{Timeout: 120 * time.Minute, Transport: connectionTransport{base: httptransport.Transport(), origin: origin, key: key, headers: headers}}
}
