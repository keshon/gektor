// Package app composes profile-driven services independently of any frontend.
// Frontends own the lifetime of the clients and speech services they accept.
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
	// Configure returns a candidate with its own client. The caller must close
	// that client if validation fails or the existing transport can be reused.
	Configure func(config.Profile) (Services, error)
	Client    llm.Client
	Settings  chat.Config
	Directory string
}
