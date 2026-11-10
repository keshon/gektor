package conversation

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/keshon/gektor/internal/llm"
)

// Prompt revisions preserve system text before resume replaces History[0].
// FirstRequest is zero for an old archived prompt whose delivery is unknown.
type PromptRevision struct {
	ID           int             `json:"id"`
	FirstRequest int             `json:"first_request,omitempty"`
	Source       string          `json:"source"`
	SHA256       string          `json:"sha256"`
	Text         string          `json:"text"`
	Sections     []PromptSection `json:"sections,omitempty"`
}

// Section offsets are byte positions into the immutable revision text.
type PromptSection struct {
	Name  string `json:"name"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// MessageRange uses inclusive, one-based archive IDs, as read_history does.
type MessageRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// RequestManifest records the latest agent request before provider formatting.
// It is not a claim about the backend's private template, cache or reasoning.
type RequestManifest struct {
	Number          int            `json:"number"`
	PromptRevision  int            `json:"prompt_revision"`
	ArchiveMessages int            `json:"archive_messages"`
	MessageCount    int            `json:"message_count"`
	Included        []MessageRange `json:"included_archive_ranges"`
	Rewritten       []int          `json:"rewritten_archive_ids,omitempty"`
	Compacted       bool           `json:"compacted"`
	HasSummary      bool           `json:"has_summary"`
	ImageCount      int            `json:"image_count"`
}

func (s *Snapshot) RecordPrompt(text string, sections []llm.PromptSection, request int) PromptRevision {
	if n := len(s.Prompts); n > 0 && s.Prompts[n-1].Text == text && !(request > 0 && s.Prompts[n-1].Source == "saved_archive") {
		return s.Prompts[n-1]
	}
	p := PromptRevision{ID: len(s.Prompts) + 1, Text: text, Source: "saved_archive", SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(text)))}
	if request > 0 {
		p.Source, p.FirstRequest = "model_request", request
	}
	cursor := 0
	for _, section := range sections {
		if section.Text == "" {
			continue
		}
		if start := strings.Index(text[cursor:], section.Text); start >= 0 {
			start += cursor
			p.Sections = append(p.Sections, PromptSection{section.Name, start, start + len(section.Text)})
			cursor = start + len(section.Text)
		}
	}
	s.Prompts = append(s.Prompts, p)
	return p
}

func (s Snapshot) validateInspection() error {
	for i, p := range s.Prompts {
		if p.ID != i+1 || p.FirstRequest < 0 || (p.Source != "saved_archive" && p.Source != "model_request") || p.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(p.Text))) {
			return fmt.Errorf("invalid prompt revision")
		}
		for _, section := range p.Sections {
			if section.Start < 0 || section.End < section.Start || section.End > len(p.Text) {
				return fmt.Errorf("invalid prompt section")
			}
		}
	}
	if r := s.LastRequest; r != nil {
		if r.Number < 1 || r.PromptRevision < 1 || r.PromptRevision > len(s.Prompts) || r.ArchiveMessages < 0 || r.ArchiveMessages > len(s.History) || r.MessageCount < 0 || r.ImageCount < 0 {
			return fmt.Errorf("invalid request manifest")
		}
		end := 0
		for _, span := range r.Included {
			if span.Start <= end || span.End < span.Start || span.End > r.ArchiveMessages {
				return fmt.Errorf("invalid request archive range")
			}
			end = span.End
		}
		for _, id := range r.Rewritten {
			if id < 1 || id > r.ArchiveMessages {
				return fmt.Errorf("invalid rewritten archive reference")
			}
		}
	}
	return nil
}
