package config

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/keshon/gektor/internal/prompts"
)

// Profile describes a thinking backend and the identity its runs speak with.
//
// The backend is always external: GEKTOR talks to a server someone else
// started. It does not ship, launch or supervise one, so a profile carries
// an endpoint and a model name and never a path to a runtime or a set of
// weights.
type Profile struct {
	Version    int        `json:"version"`
	Provider   Provider   `json:"provider"`
	Generation Generation `json:"generation"`
	Identity   *Identity  `json:"identity,omitempty"`
}

// Clone returns an independently editable profile, including the optional
// identity block.
func (p Profile) Clone() Profile {
	if p.Identity != nil {
		i := *p.Identity
		p.Identity = &i
	}
	return p
}

type Provider struct {
	Kind      string `json:"kind"`
	URL       string `json:"url"`
	Model     string `json:"model"`
	APIKeyEnv string `json:"api_key_env,omitempty"`
}
type Generation struct {
	MaxTokens    int     `json:"max_tokens"`
	ContextLimit int     `json:"context_limit"`
	Temperature  float64 `json:"temperature,omitempty"`
}

// DefaultProfile is the first-run setup; no file needs to be installed.
func DefaultProfile() Profile {
	return Profile{Version: 1,
		Provider:   Provider{Kind: "llama", URL: "http://127.0.0.1:8080", Model: "local"},
		Generation: Generation{MaxTokens: 8192, ContextLimit: 8192, Temperature: 0.7},
		Identity:   &Identity{UserName: DefaultUserName, SystemPrompt: DefaultSystemPrompt, PromptPreset: "coding"},
	}
}

// DefaultSystemPrompt is the base instruction a launch uses when nothing
// configures one. It lived in three places as a literal — the -system-prompt
// flag default, the substitution for a profile with no identity block, and
// the generated profile for a launch with no profile — and drifted silently
// the first time one of them changed.
const DefaultSystemPrompt = "You are a helpful assistant."

// DefaultTemperature is what a backend uses when a profile sets none. Zero
// means "unset", not "deterministic", so anything that shows this number has
// to say so rather than rendering a bare 0.
//
// It lives next to the field it belongs to, so the editor that displays it
// and the code that parses it cannot disagree about what it means.
const DefaultTemperature = 0.4

// ParseTemperature accepts a number, or the word "default" to hand the
// choice back to the backend.
func ParseTemperature(s string) (float64, error) {
	if strings.EqualFold(strings.TrimSpace(s), "default") {
		return 0, nil
	}
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}

// DefaultUserName is the placeholder for an unset user name.
const DefaultUserName = "User"

// Identity is the persona a profile speaks with: who the assistant is, who
// it is talking to, and how it should answer.
type Identity struct {
	PromptPreset string `json:"prompt_preset,omitempty"`
	Name         string `json:"name,omitempty"`
	UserPersona  string `json:"user_persona,omitempty"`
	UserName     string `json:"user_name,omitempty"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	PostHistory  string `json:"post_history,omitempty"`
}

// Load rejects misspelled settings rather than silently dropping half of a
// provider.
//
// Decoding is strict. A profile written for an older schema fails with the
// offending key named rather than being rewritten, because a compatibility
// path here is permanent cost for a one-time problem and would keep the
// removed keys documented forever.
func Load(path string) (Profile, error) {
	var p Profile
	f, err := os.Open(path)
	if err != nil {
		return p, err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return p, fmt.Errorf("profile: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return p, fmt.Errorf("profile: expected one JSON document")
	}
	return p, p.Validate()
}

func (p Profile) Validate() error {
	if p.Identity != nil {
		if _, err := prompts.ResponsePreset(p.Identity.PromptPreset); err != nil {
			return err
		}
	}
	if math.IsNaN(p.Generation.Temperature) || math.IsInf(p.Generation.Temperature, 0) || p.Generation.Temperature < 0 || p.Generation.Temperature > 2 {
		return fmt.Errorf("profile: temperature must be between 0 and 2; zero uses transport default")
	}
	if p.Version != 1 {
		return fmt.Errorf("profile: unsupported version %d", p.Version)
	}
	switch p.Provider.Kind {
	case "kobold", "llama", "openai":
	default:
		return fmt.Errorf("profile: unsupported provider %q", p.Provider.Kind)
	}
	u, err := url.Parse(p.Provider.URL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("profile: provider URL must be an HTTP(S) endpoint without credentials, query or fragment")
	}
	if p.Identity != nil && len(p.Identity.Name) > 100 {
		return fmt.Errorf("profile: assistant name must be at most 100 characters")
	}
	if p.Provider.Model == "" {
		return fmt.Errorf("profile: model is required")
	}
	// MaxTokens is a ceiling; the agent reduces it to the room left in the
	// server's window on each request, including after a smaller model loads.
	if p.Generation.MaxTokens <= 0 || p.Generation.ContextLimit <= 0 {
		return fmt.Errorf("profile: context_limit and max_tokens must be positive")
	}
	return nil
}
