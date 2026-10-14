// Package config loads portable application profiles without starting services.
package config

import (
	"encoding/json"
	"fmt"
	"github.com/keshon/tars/internal/speech"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Profile describes thinking, conversation and optional speech services.
// Asset paths resolve relative to the profile; model files may live elsewhere.
type Profile struct {
	Version      int            `json:"version"`
	Provider     Provider       `json:"provider"`
	Generation   Generation     `json:"generation"`
	Conversation *Conversation  `json:"conversation,omitempty"`
	Speech       *speech.Config `json:"speech,omitempty"`
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

type Conversation struct {
	Character                  string `json:"character,omitempty"`
	UserName                   string `json:"user_name,omitempty"`
	SystemPrompt               string `json:"system_prompt,omitempty"`
	PostHistory                string `json:"post_history,omitempty"`
	PreferCharacterPrompt      *bool  `json:"prefer_character_prompt,omitempty"`
	PreferCharacterPostHistory *bool  `json:"prefer_character_post_history,omitempty"`
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
	d := json.NewDecoder(io.LimitReader(f, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return p, fmt.Errorf("profile: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return p, fmt.Errorf("profile: expected one JSON document")
	}
	if p.Conversation != nil && p.Conversation.Character != "" && !filepath.IsAbs(p.Conversation.Character) {
		absolute, err := filepath.Abs(filepath.Join(filepath.Dir(path), p.Conversation.Character))
		if err != nil {
			return p, err
		}
		p.Conversation.Character = absolute
	}
	for _, value := range []*string{&p.Provider.Runtime, &p.Provider.ModelFile, &p.Provider.ModelRoot} {
		if *value != "" && !filepath.IsAbs(*value) {
			*value, err = filepath.Abs(filepath.Join(filepath.Dir(path), *value))
			if err != nil {
				return p, err
			}
		}
	}
	if p.Speech != nil {
		for _, value := range []*string{&p.Speech.Listening.Binary, &p.Speech.Listening.Model, &p.Speech.Speaking.Binary, &p.Speech.Speaking.Model} {
			if *value != "" && !filepath.IsAbs(*value) {
				absolute, err := filepath.Abs(filepath.Join(filepath.Dir(path), *value))
				if err != nil {
					return p, err
				}
				*value = absolute
			}
		}
		for _, value := range []*string{&p.Speech.Recorder, &p.Speech.PlayerBinary} {
			if *value != "" && !filepath.IsAbs(*value) && (strings.ContainsAny(*value, "/\\")) {
				absolute, err := filepath.Abs(filepath.Join(filepath.Dir(path), *value))
				if err != nil {
					return p, err
				}
				*value = absolute
			}
		}
	}
	return p, p.Validate()
}
func (p Profile) Validate() error {
	if p.Speech != nil {
		if err := p.Speech.Validate(); err != nil {
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
	if p.Provider.Model == "" {
		return fmt.Errorf("profile: model is required")
	}
	if p.Generation.MaxTokens <= 0 || p.Generation.ContextLimit <= p.Generation.MaxTokens {
		return fmt.Errorf("profile: context_limit must exceed positive max_tokens")
	}
	return nil
}
