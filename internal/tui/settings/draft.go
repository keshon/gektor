// Package settings owns staged Settings edits independently of UI focus,
// service lifetimes, and persistence.
package settings

import (
	"fmt"
	"slices"

	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/tui/components/form"
)

type page struct {
	original, current []form.Field
}

// Draft owns a profile and copies of each visited page. Failed resolution never
// mutates the base profile or consumes the user's edits.
type Draft struct {
	base   config.Profile
	loaded bool
	pages  map[string]page
}

func New(profile config.Profile) *Draft {
	return &Draft{base: profile.Clone(), pages: make(map[string]page)}
}

func (d *Draft) Base() config.Profile { return d.base.Clone() }

// Load stages another profile; Apply remains a separate application decision.
func (d *Draft) Load(profile config.Profile) {
	d.base = profile.Clone()
	d.loaded = true
	d.pages = make(map[string]page)
}

// Visit records a page's baseline once and restores any edits on later visits.
func (d *Draft) Visit(section string, defaults []form.Field) []form.Field {
	p, ok := d.pages[section]
	if !ok {
		p = page{original: slices.Clone(defaults), current: slices.Clone(defaults)}
		d.pages[section] = p
	}
	return slices.Clone(p.current)
}

func (d *Draft) Store(section string, fields []form.Field) {
	p := d.pages[section]
	p.current = slices.Clone(fields)
	d.pages[section] = p
}

func (d *Draft) Fields(section string) ([]form.Field, bool) {
	p, ok := d.pages[section]
	return slices.Clone(p.current), ok
}

func (d *Draft) ChangedSections() []string {
	var sections []string
	for section, p := range d.pages {
		if !slices.Equal(p.current, p.original) {
			sections = append(sections, section)
		}
	}
	slices.Sort(sections)
	return sections
}

// Dirty also accounts for an active editor without storing it during rendering.
func (d *Draft) Dirty(active string, fields []form.Field) bool {
	if d.loaded {
		return true
	}
	for section, p := range d.pages {
		current := p.current
		if section == active {
			current = fields
		}
		if !slices.Equal(current, p.original) {
			return true
		}
	}
	return false
}

// Resolve applies changed pages in stable order to a fresh profile. Application
// preferences and profile-copy controls are handled by the parent application.
func (d *Draft) Resolve() (config.Profile, error) {
	p := d.Base()
	for _, section := range d.ChangedSections() {
		if section == "application" || section == "profiles" {
			continue
		}
		values := make(map[string]string)
		for _, field := range d.pages[section].current {
			values[field.Key] = field.Value
		}
		if err := config.ApplySection(&p, section, values); err != nil {
			return config.Profile{}, fmt.Errorf("%s: %w", section, err)
		}
	}
	return p, p.Validate()
}
