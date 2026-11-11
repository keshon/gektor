package config

import "testing"

func TestChangingTheModelDoesNotInventAnIdentity(t *testing.T) {
	p := validProfile()
	err := ApplySection(&p, SectionModel, map[string]string{
		"kind": "llama", "endpoint": "http://127.0.0.1:9999", "model": "other",
	})
	if err != nil {
		t.Fatal(err)
	}

	if p.Identity != nil {
		t.Fatalf("an identity block was invented: %+v", p.Identity)
	}

	if p.Provider.Model != "other" {
		t.Fatalf("model = %q", p.Provider.Model)
	}
}

func TestUnknownSectionIsRejectedAndWritesNothing(t *testing.T) {
	p := validProfile()
	before := p
	err := ApplySection(&p, "generaton", map[string]string{"temperature": "0.5"})
	if err == nil {
		t.Fatal("a typo'd section was accepted")
	}

	if p.Identity != nil {
		t.Fatalf("a rejected section still mutated the profile: %+v", p.Identity)
	}

	if p.Generation.Temperature != before.Generation.Temperature {
		t.Fatalf("a rejected section still wrote: %v", p.Generation.Temperature)
	}
}

func TestEveryOfferedSectionIsHandled(t *testing.T) {
	values := map[string]map[string]string{
		SectionModel:      {"kind": "llama", "endpoint": "http://127.0.0.1:8080", "model": "local"},
		SectionGeneration: {"temperature": "0.7", "max": "256", "context": "4096"},
		SectionPrompts:    {"system": "rules", "post": ""},
		SectionYou:        {"user": "Alex", "persona": "", "name": "GEKTOR"},
	}

	for _, section := range Sections() {
		p := validProfile()
		err := ApplySection(&p, section, values[section])
		if err != nil {
			t.Errorf("section %q is offered but not handled: %v", section, err)
		}
	}
}

func TestApplySectionCreatesTheIdentityItEdits(t *testing.T) {
	p := validProfile()
	if p.Identity != nil {
		t.Fatal("precondition: profile should have no identity block")
	}

	err := ApplySection(&p, "you", map[string]string{"user": "Alex", "persona": "a colleague"})
	if err != nil {
		t.Fatal(err)
	}

	if p.Identity == nil || p.Identity.UserName != "Alex" || p.Identity.UserPersona != "a colleague" {
		t.Fatalf("identity not created: %+v", p.Identity)
	}
}
