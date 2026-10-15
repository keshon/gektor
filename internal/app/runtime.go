package app

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/keshon/tars/internal/chat"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/speech"
)

// Runtime owns accepted thinking and speech services. Its owner calls Close;
// frontends borrow Services and Voice. Lifecycle changes are serialized.
type Runtime struct {
	Services *Services
	Voice    speech.Service
	closed   bool
}

func NewRuntime(services *Services) *Runtime {
	if services == nil {
		services = &Services{}
	}
	return &Runtime{Services: services}
}

func closeClient(client llm.Client) {
	if c, ok := client.(interface{ Close() }); ok {
		c.Close()
	}
}

func (r *Runtime) EnableVoice(ctx context.Context) error {
	if r.closed {
		return fmt.Errorf("runtime is closed")
	}
	if r.Services.NewVoice == nil {
		return fmt.Errorf("this profile has no speech configuration")
	}
	if r.Voice == nil {
		voice, err := r.Services.NewVoice(ctx, filepath.Join(r.Services.Directory, "..", "voice"))
		if err != nil {
			return err
		}
		r.Voice = voice
	}
	return nil
}

func (r *Runtime) DisableVoice() {
	if r.Voice != nil {
		r.Voice.Close()
		r.Voice = nil
	}
}

func (r *Runtime) Close() {
	if r.closed {
		return
	}
	r.closed = true
	r.DisableVoice()
	closeClient(r.Services.Client)
}

// Apply validates a candidate and persists it before transferring ownership.
// A nil candidate client means speech-only reconfiguration; the thinking client
// remains borrowed by the existing Agent runner. Persist must not retain or
// mutate the candidate, and must report any failed write before returning.
func (r *Runtime) Apply(profile config.Profile, current *chat.Conversation, persist func(Services, *chat.Conversation) error) (*chat.Conversation, error) {
	if r.closed {
		return nil, fmt.Errorf("runtime is closed")
	}
	if r.Services.Configure == nil {
		return nil, fmt.Errorf("runtime cannot be reconfigured")
	}
	next, err := r.Services.Configure(profile)
	if err != nil {
		return nil, err
	}
	thinking := next.Client != nil
	reuse := !thinking || profile.Provider == r.Services.Profile.Provider && profile.Generation.ContextLimit == r.Services.Profile.Generation.ContextLimit
	if thinking && reuse {
		closeClient(next.Client)
	}
	if reuse {
		next.Client = r.Services.Client
	}
	installed := false
	defer func() {
		if !installed && !reuse {
			closeClient(next.Client)
		}
	}()
	var updated *chat.Conversation
	if current != nil {
		updated, err = current.Reconfigured(next.Client, next.Settings)
	} else if thinking {
		_, err = chat.New(next.Client, next.Settings)
	}
	if err != nil {
		return nil, err
	}
	if persist != nil {
		if err := persist(next, updated); err != nil {
			return nil, err
		}
	}
	if !reflect.DeepEqual(next.Profile.Speech, r.Services.Profile.Speech) {
		r.DisableVoice()
	}
	if !reuse {
		closeClient(r.Services.Client)
	}
	*r.Services = next
	installed = true
	return updated, nil
}
