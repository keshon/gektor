package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"

	"github.com/keshon/tars/internal/prompts"
)

// Profile describes a thinking backend and the identity its runs speak with.
// Asset paths resolve relative to the profile; model files may live elsewhere.
type Profile struct {
	Version    int       `json:"version"`
	Provider   Provider  `json:"provider"`
	Generation Generation `json:"generation"`
	Identity   *Identity `json:"identity,omitempty"`
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
	Ownership string `json:"ownership"`
	APIKeyEnv string `json:"api_key_env,omitempty"`
	Runtime   string `json:"runtime,omitempty"`
	ModelRoot string `json:"model_root,omitempty"`
	ModelFile string `json:"model_file,omitempty"`
	GPULayers int    `json:"gpu_layers,omitempty"`
}
type Generation struct {
	MaxTokens    int     `json:"max_tokens"`
	ContextLimit int     `json:"context_limit"`
	Temperature  float64 `json:"temperature,omitempty"`
}

// DefaultSystemPrompt is the base instruction a launch uses when nothing
// configures one. It lived in three places as a literal — the -system-prompt
// flag default, the substitution for a profile with no identity block, and
// the generated profile for a launch with no profile — and drifted silently
// the first time one of them changed.
const DefaultSystemPrompt = "You are a helpful assistant."

// DefaultUserName is the placeholder for an unset user name.
const DefaultUserName = "User"

// Identity is the persona a profile speaks with: who the assistant is, who
// it is talking to, and how it should answer.
//
// This block was called "conversation" while a chat mode shared it with
// greeting text, alternate greetings and character-card preferences. Those
// are gone; what remained was always identity.
type Identity struct {
	PromptPreset string `json:"prompt_preset,omitempty"`
	Name         string `json:"name,omitempty"`
	UserPersona  string `json:"user_persona,omitempty"`
	UserName     string `json:"user_name,omitempty"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	PostHistory  string `json:"post_history,omitempty"`
}

// legacyBlocks are keys older profiles may carry that Profile no longer
// declares. Decoding is strict everywhere else, so these are handled
// explicitly rather than by relaxing DisallowUnknownFields.
var legacyBlocks = map[string]string{
	"conversation": "identity",
	// Speech is discarded, not mapped: the services it described no longer
	// exist, and keeping the values would only let a stale profile look
	// configured.
	"speech": "",
}

// Load rejects misspelled settings and unsupported runtime ownership rather
// than silently attaching to a service the application was expected to start.
func Load(path string) (Profile, error) {
	var p Profile
	f, err := os.Open(path)
	if err != nil {
		return p, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return p, err
	}
	data, err = migrateLegacyBlocks(data)
	if err != nil {
		return p, fmt.Errorf("profile: %w", err)
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return p, fmt.Errorf("profile: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return p, fmt.Errorf("profile: expected one JSON document")
	}
	for _, value := range []*string{&p.Provider.Runtime, &p.Provider.ModelFile, &p.Provider.ModelRoot} {
		if *value != "" && !filepath.IsAbs(*value) {
			*value, err = filepath.Abs(filepath.Join(filepath.Dir(path), *value))
			if err != nil {
				return p, err
			}
		}
	}
	return p, p.Validate()
}

// migrateLegacyBlocks rewrites legacy top-level keys before the strict
// decode runs. A "conversation" block is renamed rather than dropped: it is
// the same fields under an older name, and discarding it would silently
// erase a reader's persona the first time they loaded a saved setup.
func migrateLegacyBlocks(data []byte) ([]byte, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return data, nil // let the strict decoder produce the real error
	}
	moved := false
	for old, replacement := range legacyBlocks {
		value, ok := top[old]
		if !ok {
			continue
		}
		delete(top, old)
		moved = true
		if replacement == "" {
			continue
		}
		if _, exists := top[replacement]; exists {
			return nil, fmt.Errorf("%q and its replacement %q are both present", old, replacement)
		}
		top[replacement] = value
	}
	if !moved {
		return data, nil
	}
	return json.Marshal(top)
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
	if p.Provider.Ownership != "external" && p.Provider.Ownership != "managed" {
		return fmt.Errorf("profile: ownership must be external or managed")
	}
	if p.Provider.Ownership == "managed" {
		if p.Provider.Kind != "llama" || p.Provider.Runtime == "" || p.Provider.ModelFile == "" {
			return fmt.Errorf("profile: managed llama requires runtime and model_file")
		}
		if p.Provider.GPULayers < 0 || p.Provider.GPULayers > 999 {
			return fmt.Errorf("profile: gpu_layers must be 0..999")
		}
	}
	u, err := url.Parse(p.Provider.URL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("profile: provider URL must be an HTTP(S) endpoint without credentials, query or fragment")
	}
	if p.Provider.Ownership == "managed" && (u.Scheme != "http" || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") || u.Port() == "" || (u.Path != "" && u.Path != "/")) {
		return fmt.Errorf("profile: managed llama needs a loopback HTTP origin with an explicit port")
	}
	if p.Identity != nil && len(p.Identity.Name) > 100 {
		return fmt.Errorf("profile: assistant name must be at most 100 characters")
	}
	if p.Provider.Model == "" {
		return fmt.Errorf("profile: model is required")
	}
	if p.Generation.MaxTokens <= 0 || p.Generation.ContextLimit <= p.Generation.MaxTokens {
		return fmt.Errorf("profile: context_limit must exceed positive max_tokens")
	}
	return nil
}