package tools

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/keshon/gektor/internal/gektor"
	"github.com/keshon/gektor/internal/session"
)

var browserProfileName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,47}$`)

func ValidateBrowserProfile(name string) error {
	upper := strings.ToUpper(name)
	reserved := upper == "CON" || upper == "PRN" || upper == "AUX" || upper == "NUL" || len(upper) == 4 && (strings.HasPrefix(upper, "COM") || strings.HasPrefix(upper, "LPT")) && upper[3] >= '1' && upper[3] <= '9'
	if !browserProfileName.MatchString(name) || reserved {
		return fmt.Errorf("invalid browser profile %q: use 1–48 letters, digits, underscores or hyphens, starting with a letter or digit; reserved device names are not allowed", name)
	}
	return nil
}

func browserProfileDir(ref session.BrowserProfile) (string, error) {
	if ref.Channel != "chrome" && ref.Channel != "msedge" {
		return "", fmt.Errorf("browser must be chrome or msedge")
	}
	if err := ValidateBrowserProfile(ref.Profile); err != nil {
		return "", err
	}
	dir := gektor.Sub("browser-profiles", ref.Channel, strings.ToLower(ref.Profile))
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("browser profile requires a user home directory")
	}
	return dir, nil
}

func (b *Browser) referenceLocked() *session.BrowserProfile {
	if b.selected != nil {
		ref := *b.selected
		return &ref
	}
	profile := b.Profile
	if profile == "" {
		profile = "default"
	}
	return &session.BrowserProfile{Channel: b.Channel, Profile: strings.ToLower(profile)}
}

// Reference returns metadata only; inspecting/saving a session creates nothing.
func (b *Browser) Reference() *session.BrowserProfile {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.referenceLocked()
}

// SelectProfile runs before restoring lazy tool catalogs. A new conversation
// resets to the launch selection; reopening a saved one restores its reference.
func (b *Browser) SelectProfile(saved *session.BrowserProfile) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	profile := b.Profile
	if profile == "" {
		profile = "default"
	}
	ref := session.BrowserProfile{Channel: b.Channel, Profile: profile}
	if saved != nil && saved.Channel != b.Channel {
		return fmt.Errorf("session uses %s profile %q; reopen with -browser %s, or start a new session to change browser", saved.Channel, saved.Profile, saved.Channel)
	}
	if saved != nil && !b.ProfileExplicit {
		ref = *saved
	}
	if _, err := browserProfileDir(ref); err != nil {
		return err
	}
	ref.Profile = strings.ToLower(ref.Profile)
	current := b.referenceLocked()
	if *current != ref {
		b.closeLocked()
	}
	b.selected = &ref
	return nil
}
