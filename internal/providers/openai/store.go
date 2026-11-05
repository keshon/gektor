// Package openai manages Gektor's own ChatGPT OAuth registration.
package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const API = "https://api.openai.com/v1"
const issuer = "https://auth.openai.com"
const scopes = "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct"

type Account struct {
	ClientID     string    `json:"client_id"`
	Subject      string    `json:"subject"`
	Email        string    `json:"email"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	IDToken      string    `json:"id_token"`
	Scope        string    `json:"scope"`
	Expires      time.Time `json:"expires"`
}

type credentials struct {
	HostID   string             `json:"host_id"`
	Active   string             `json:"active"`
	Accounts map[string]Account `json:"accounts"`
}

// Manager stores credentials outside workspace profiles and transcripts.
type Manager struct{ Dir string }

func Default() *Manager {
	home, _ := os.UserHomeDir()
	return &Manager{Dir: filepath.Join(home, ".gektor", "auth", "chatgpt")}
}

// withStore holds a cross-process lock across rotating-token refresh and save.
func (m *Manager) withStore(ctx context.Context, edit func(*credentials) (bool, error)) error {
	if m.Dir == "" {
		return fmt.Errorf("ChatGPT credential directory is empty")
	}
	if err := os.MkdirAll(m.Dir, 0700); err != nil {
		return err
	}
	unlock, err := lockStore(ctx, filepath.Join(m.Dir, "lock"))
	if err != nil {
		return err
	}
	defer unlock()
	s := credentials{Accounts: map[string]Account{}}
	path := filepath.Join(m.Dir, "credentials")
	raw, err := os.ReadFile(path)
	if err == nil {
		raw, err = unprotect(raw)
		if err != nil {
			return fmt.Errorf("cannot unlock ChatGPT credentials")
		}
		if err = json.Unmarshal(raw, &s); err != nil {
			return fmt.Errorf("invalid ChatGPT credential store")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if s.Accounts == nil {
		s.Accounts = map[string]Account{}
	}
	changed, err := edit(&s)
	if !changed {
		return err
	}
	editErr := err
	raw, err = json.Marshal(s)
	if err != nil {
		return err
	}
	raw, err = protect(raw)
	if err != nil {
		return fmt.Errorf("protect ChatGPT credentials: %w", err)
	}
	f, err := os.CreateTemp(m.Dir, "credentials-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	return editErr
}

func permitted(scope string) bool {
	for _, s := range strings.Fields(scope) {
		if s == "chatgpt.tokens.use.direct" {
			return true
		}
	}
	return false
}

// Status exposes display information only, never credential material.
func (m *Manager) Status(ctx context.Context) (string, error) {
	var label string
	err := m.withStore(ctx, func(s *credentials) (bool, error) {
		a, ok := s.Accounts[s.Active]
		if !ok || a.AccessToken == "" {
			label = "Not connected. Run: gektor auth login"
			return false, nil
		}
		label = a.Email
		if label == "" {
			label = "ChatGPT account"
		}
		if !permitted(a.Scope) {
			label += " (ChatGPT plan usage not authorized)"
		}
		return false, nil
	})
	return label, err
}

// BoundAccount pins a registration and verified subject, not an access token.
type BoundAccount struct {
	manager               *Manager
	registration, subject string
	Label                 string
}

func (m *Manager) Bind(ctx context.Context) (*BoundAccount, error) {
	var bound *BoundAccount
	err := m.withStore(ctx, func(s *credentials) (bool, error) {
		a, ok := s.Accounts[s.Active]
		if !ok || a.AccessToken == "" {
			return false, fmt.Errorf("ChatGPT is not connected; run: gektor auth login")
		}
		if a.Subject == "" {
			return false, fmt.Errorf("ChatGPT identity is missing; sign in again")
		}
		label := a.Email
		if label == "" {
			label = "ChatGPT account"
		}
		bound = &BoundAccount{manager: m, registration: s.Active, subject: a.Subject, Label: label}
		return false, nil
	})
	return bound, err
}

// Scope contains no credentials and changes if a registration changes identity.
func (a *BoundAccount) Scope() string { return a.registration + ":" + a.subject }
func (a *BoundAccount) Token(ctx context.Context) (string, error) {
	return a.manager.token(ctx, a.registration, a.subject)
}
func (m *Manager) Token(ctx context.Context) (string, error) {
	return m.token(ctx, "", "")
}
func (m *Manager) token(ctx context.Context, registration, subject string) (string, error) {
	var token string
	err := m.withStore(ctx, func(s *credentials) (bool, error) {
		id := registration
		if id == "" {
			id = s.Active
		}
		a, ok := s.Accounts[id]
		if !ok || a.AccessToken == "" || (subject != "" && a.Subject != subject) {
			return false, fmt.Errorf("ChatGPT is not connected; run: gektor auth login")
		}
		if !permitted(a.Scope) {
			return false, fmt.Errorf("ChatGPT plan usage not authorized; run: gektor auth login")
		}
		changed := false
		if time.Until(a.Expires) < time.Minute {
			t, err := exchange(ctx, map[string]string{"grant_type": "refresh_token", "client_id": a.ClientID, "refresh_token": a.RefreshToken, "resource": API})
			if err != nil {
				return false, err
			}
			if t.RefreshToken == "" {
				return false, fmt.Errorf("ChatGPT refresh returned no replacement refresh token; sign in again")
			}
			a.AccessToken, a.RefreshToken, a.Expires = t.AccessToken, t.RefreshToken, time.Now().Add(time.Duration(t.ExpiresIn)*time.Second)
			if t.Scope != "" {
				a.Scope = t.Scope
			}
			if t.IDToken != "" {
				a.IDToken = t.IDToken
			}
			s.Accounts[id], changed = a, true
		}
		if !permitted(a.Scope) {
			return changed, fmt.Errorf("ChatGPT plan permission was withdrawn; sign in again")
		}
		token = a.AccessToken
		return changed, nil
	})
	return token, err
}
