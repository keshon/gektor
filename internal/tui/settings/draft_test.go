package settings

import (
	"testing"

	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/tui/components/form"
)

func profile() config.Profile {
	return config.Profile{
		Version:    1,
		Provider:   config.Provider{Kind: "llama", URL: "http://localhost:8080", Model: "local"},
		Generation: config.Generation{MaxTokens: 100, ContextLimit: 1000},
		Identity:   &config.Identity{UserName: "User"},
	}
}

func TestDraftOwnsPageEditsAndObservesUnstoredActiveEditor(t *testing.T) {
	d := New(profile())
	fields := []form.Field{{Key: "user", Value: "User"}, {Key: "persona"}}
	page := d.Visit("you", fields)
	page[0].Value = "Alex"
	if !d.Dirty("you", page) {
		t.Fatal("active editor change was not detected")
	}
	if d.Dirty("", nil) {
		t.Fatal("observing active editor mutated the draft")
	}
	d.Store("you", page)
	page[0].Value = "unowned edit"
	stored := d.Visit("you", fields)
	if stored[0].Value != "Alex" || fields[0].Value != "User" {
		t.Fatal("page snapshots share mutable slices")
	}
	resolved, err := d.Resolve()
	if err != nil || resolved.Identity.UserName != "Alex" || d.Base().Identity.UserName != "User" {
		t.Fatalf("resolution changed its base or lost edits: %+v %v", resolved, err)
	}
}

func TestFailedResolutionCanBeCorrectedWithoutChangingBase(t *testing.T) {
	d := New(profile())
	fields := []form.Field{{Key: "temperature", Value: "0"}, {Key: "max", Value: "100"}, {Key: "context", Value: "1000"}}
	d.Visit("generation", fields)
	fields[0].Value = "0.8"
	fields[1].Value = "invalid"
	d.Store("generation", fields)
	if _, err := d.Resolve(); err == nil {
		t.Fatal("invalid edit resolved")
	}
	if d.Base().Generation.Temperature != 0 {
		t.Fatal("partly applied invalid edit leaked into base")
	}
	fields[1].Value = "200"
	d.Store("generation", fields)
	resolved, err := d.Resolve()
	if err != nil || resolved.Generation.Temperature != .8 || resolved.Generation.MaxTokens != 200 {
		t.Fatalf("corrected edit did not resolve: %+v %v", resolved.Generation, err)
	}
}

func TestLoadingProfileDiscardsPriorPageEditsAndCopiesOwnership(t *testing.T) {
	d := New(profile())
	d.Visit("you", []form.Field{{Key: "user", Value: "User"}})
	d.Store("you", []form.Field{{Key: "user", Value: "obsolete edit"}})
	p := profile()
	p.Identity.UserName = "Loaded"
	d.Load(p)
	p.Identity.UserName = "external mutation"
	if !d.Dirty("", nil) || len(d.ChangedSections()) != 0 || d.Base().Identity.UserName != "Loaded" {
		t.Fatal("loaded profile was not isolated from caller or previous pages")
	}
}
