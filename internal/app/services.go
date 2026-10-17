// Package app composes and owns profile-driven services independently of any
// frontend. Frontends borrow a composed set for the length of one launch.
package app

import (
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/llm"
)

// Services is what one launch is composed from: a thinking client, the
// identity its runs speak with, and the profile it came from.
//
// There is no reconfigure callback. A launch composes once; changing settings
// ends the launch and composes a new one, so there is nothing for a callback
// to be called on behalf of.
type Services struct {
	Identity  Identity
	Stream    bool
	Warnings  []string
	Profile   config.Profile
	Client    llm.Client
	Directory string
}