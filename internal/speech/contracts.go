// Package speech defines audio capabilities independently of conversation runners.
package speech

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
)

type Voice struct {
	ID, Name, Language string
	Default            bool
}
type Transcript struct{ Text, Language string }
type Cue struct {
	Name   string
	Offset int
}
type Utterance struct {
	Text string
	Cues []Cue
}
type Capture interface {
	Run() ([]byte, error)
	Stop()
}
type Player interface {
	Enqueue(context.Context, []byte) error
	Finish() error
	Cancel()
}

// DeliveryProvider advertises optional provider-specific bracketed vocal cues.
type DeliveryProvider interface {
	DeliveryCues() []string
	DeliveryInstructions() string
}

func DeliveryPrompt(service Service) string {
	p, ok := service.(DeliveryProvider)
	if !ok || len(p.DeliveryCues()) == 0 {
		return ""
	}
	return "Optional speech delivery: use only these exact bracketed tags: [" + strings.Join(p.DeliveryCues(), "], [") + "]. They are audio controls, not spoken words. Do not invent stage directions or other tags. You may demonstrate these supported performances when requested.\n" + p.DeliveryInstructions()
}

type Service interface {
	Record(context.Context) (Capture, error)
	Recognize(context.Context, []byte) (Transcript, error)
	Synthesize(context.Context, string) ([]byte, error)
	Player(context.Context) Player
	Close()
}
type Config struct {
	VAD          string    `json:"vad,omitempty"`
	MicThreshold float64   `json:"mic_threshold,omitempty"`
	MicStartMS   int       `json:"mic_start_ms,omitempty"`
	MicEndMS     int       `json:"mic_end_ms,omitempty"`
	Enabled      bool      `json:"enabled,omitempty"`
	Microphone   string    `json:"microphone"`
	Recorder     string    `json:"recorder"`
	PlayerBinary string    `json:"player"`
	MaxSeconds   int       `json:"max_seconds"`
	Listening    Listening `json:"listening"`
	Speaking     Speaking  `json:"speaking"`
}
type Listening struct {
	Ownership string `json:"ownership"`
	URL       string `json:"url"`
	Binary    string `json:"binary,omitempty"`
	Model     string `json:"model,omitempty"`
}
type Speaking struct {
	Provider string `json:"provider"`
	Variant  string `json:"variant"`
	Binary   string `json:"binary"`
	Model    string `json:"model"`
}

func (c Config) Validate() error {
	if c.VAD != "" && c.VAD != "silero" && c.VAD != "energy" {
		return fmt.Errorf("vad must be silero or energy")
	}
	if c.MicThreshold < 0 || c.MicThreshold > 1 || c.MicStartMS < 0 || c.MicStartMS > 800 || c.MicEndMS < 0 || c.MicEndMS > 10000 {
		return fmt.Errorf("invalid microphone threshold/start/end timing")
	}
	if c.Recorder == "" || c.PlayerBinary == "" || c.Microphone == "" || c.MaxSeconds < 5 || c.MaxSeconds > 120 {
		return fmt.Errorf("speech requires recorder/player/microphone and max_seconds between 5 and 120")
	}
	if c.Speaking.Provider != "chatterbox" || (c.Speaking.Variant != "nano" && c.Speaking.Variant != "turbo") || c.Speaking.Binary == "" || c.Speaking.Model == "" {
		return fmt.Errorf("speech requires a Chatterbox nano/turbo runtime and model")
	}
	u, err := url.Parse(c.Listening.URL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("speech requires an HTTP(S) recognition URL without embedded credentials")
	}
	if c.Listening.Ownership == "managed" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || ip == nil || !ip.IsLoopback() || u.Port() == "" || u.Path != "" || c.Listening.Binary == "" || c.Listening.Model == "" {
			return fmt.Errorf("managed recognition requires a loopback IP/port, executable and model")
		}
	} else if c.Listening.Ownership != "external" {
		return fmt.Errorf("recognition ownership must be managed or external")
	}
	return nil
}
