// Package config defines, validates, edits, and persists profiles and application preferences.
// Values are caller-owned; this package has no cache or background work.
package config

// Profile describes a thinking backend and the identity its runs speak with.
//
// The backend is always external: GEKTOR talks to a server someone else
// started. It does not ship, launch or supervise one, so a profile carries
// an endpoint and a model name and never a path to a runtime or a set of
// weights. Identity is optional; Clone before editing a shared profile.
type Profile struct {
	Version    int        `json:"version"`
	Provider   Provider   `json:"provider"`
	Generation Generation `json:"generation"`
	Identity   *Identity  `json:"identity,omitempty"`
}

// Preferences stores application choices. Profile is optional and caller-owned;
// use Profile.Clone before editing a shared profile. No resources are retained.
type Preferences struct {
	Mode        string     `json:"mode,omitempty"`
	Theme       string     `json:"theme,omitempty"`
	Appearance  Appearance `json:"appearance,omitempty"`
	Version     int        `json:"version"`
	Workspace   string     `json:"workspace,omitempty"`
	ProfilePath string     `json:"profile_path,omitempty"`
	Profile     *Profile   `json:"profile,omitempty"`
	Harness     Harness    `json:"harness,omitempty"`
}
