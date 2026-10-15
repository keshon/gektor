// Package app composes and owns profile-driven services independently of any
// frontend. Runtime manages accepted services; frontends borrow them.
package app

import (
	"context"

	"github.com/keshon/tars/internal/chat"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/speech"
)

// Services composes profile-driven thinking and optional speech services.
type Services struct {
	NewVoice func(context.Context, string) (speech.Service, error)
	Stream   bool
	Warnings []string
	Profile  config.Profile
	// Chat configurations return a candidate with its own client; speech-only
	// Agent configurations leave Client nil. Runtime handles cleanup and reuse.
	Configure func(config.Profile) (Services, error)
	Client    llm.Client
	Settings  chat.Config
	Directory string
}
