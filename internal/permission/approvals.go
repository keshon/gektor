package permission

import (
	"net/url"
	"strings"
	"sync"
)

// Approvals holds in-memory grants for one open session. Safe for concurrent use.
// The frontend resets it when leaving the session; grants are never persisted.
type Approvals struct {
	mu         sync.Mutex // guards grants and fullAccess
	grants     map[[2]string]bool
	fullAccess bool
}

// Target keeps resources exact except ssh_exec, whose canonical resource is
// an endpoint followed by a command. File and URL spaces remain significant.
func Target(tool, resource string) string {
	if tool != "ssh_exec" {
		return resource
	}
	endpoint, _, hasCommand := strings.Cut(resource, " ")
	if !hasCommand {
		return resource
	}

	parsed, err := url.Parse(endpoint)
	if err != nil {
		return resource
	}
	if parsed.Scheme != "ssh" || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return resource
	}

	return endpoint
}

// SessionAnswer preserves the a/always replies used by existing transports.
func SessionAnswer(answer string) bool {
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "a" || answer == "always"
}

func (a *Approvals) Allows(tool, resource string) bool {
	target := Target(tool, resource)
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.fullAccess || a.grants[[2]string{tool, target}]
}

func (a *Approvals) Remember(tool, resource string) {
	target := Target(tool, resource)
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.grants == nil {
		a.grants = make(map[[2]string]bool)
	}
	a.grants[[2]string{tool, target}] = true
}

func (a *Approvals) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.grants = nil
	a.fullAccess = false
}

func (a *Approvals) Count() int {
	a.mu.Lock()
	defer a.mu.Unlock()

	return len(a.grants)
}

func (a *Approvals) FullAccess() bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.fullAccess
}

// SetFullAccess also revokes previous grants when returning to permission prompts.
func (a *Approvals) SetFullAccess(enabled bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.fullAccess = enabled
	if !enabled {
		a.grants = nil
	}
}
