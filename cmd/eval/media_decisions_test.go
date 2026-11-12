package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMediaDecisionCriteriaRejectObservedMistakes(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	probes, err := loadProbes("eval/probes", "")
	if err != nil {
		t.Fatal(err)
	}
	selected, err := selectSuite(probes, "media-decisions")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		answer string
		bad    []string
	}{
		{`{"selected_url":"https://www.youtube.com/watch?v=studio0342x","verified_official":false,"reason":"Mira Vale uploader, 342 seconds, official studio title; reject 61 seconds and live performance."}`, []string{
			`studio0342x|preview0061`, `studio0342x|concert0342`,
			`"verified_official":false|"verified_official":true`,
		}},
		{`{"download_path":"song.m4a","download_mode":"audio","convert_format":"mp3","stream_copy":false,"operations":["video_download","media_convert","media_inspect"],"reason":"AAC source needs MP3 encoding."}`, []string{
			`song.m4a|song.mp3`, `"stream_copy":false|"stream_copy":true`,
			`"video_download","media_convert","media_inspect"|"video_download","media_inspect"`,
		}},
		{`{"candidate_urls":["https://www.youtube.com/watch?v=recommend01","https://www.youtube.com/watch?v=recommend02"],"verified_official":false,"evidence_basis":"search results only","explanation":"These uploads are not verified; titles alone are insufficient."}`, []string{
			`"verified_official":false|"verified_official":true`,
			`search results only|video metadata inspected`,
		}},
	}
	for i, p := range selected {
		t.Run(p.Name, func(t *testing.T) {
			entries, err := os.ReadDir(p.Seed)
			if err != nil || len(entries) != 1 || entries[0].Name() != []string{"sources.json", "formats.json", "search.json"}[i] {
				t.Fatal("decision fixture must expose only its own evidence", entries, err)
			}
			if failures := checkAnswer(p, cases[i].answer); len(failures) != 0 {
				t.Fatal("valid decision rejected", failures)
			}
			if i == 0 {
				quoted := strings.Replace(cases[i].answer, "Mira Vale uploader", `\"Mira Vale\" uploader`, 1)
				if failures := checkAnswer(p, quoted); len(failures) != 0 {
					t.Fatal("escaped quotation marks changed rationale scoring", failures)
				}
			}
			if i == 2 {
				reversed := strings.NewReplacer("recommend01", "recommend02", "recommend02", "recommend01").Replace(cases[i].answer)
				if failures := checkAnswer(p, reversed); len(failures) != 0 {
					t.Fatal("candidate ordering changed the decision", failures)
				}
			}
			for _, invalid := range []string{
				"Explanation followed by " + cases[i].answer,
				"```json\n" + cases[i].answer + "\n```",
				cases[i].answer + cases[i].answer,
				strings.TrimSuffix(cases[i].answer, "}") + `,"unrequested":true}`,
			} {
				if failures := checkAnswer(p, invalid); len(failures) == 0 {
					t.Fatal("malformed or out-of-contract answer accepted", invalid)
				}
			}
			for _, replacement := range cases[i].bad {
				parts := strings.SplitN(replacement, "|", 2)
				bad := strings.ReplaceAll(cases[i].answer, parts[0], parts[1])
				if failures := checkAnswer(p, bad); len(failures) == 0 {
					t.Fatal("observed mistake accepted", bad)
				}
			}
			ok := true
			calls := []call{
				{tool: "enable_capability", args: `{"id":"native_media"}`, success: &ok},
				{tool: "read_file", args: `{"path":"` + []string{"sources.json", "formats.json", "search.json"}[i] + `"}`, success: &ok},
			}
			if failures := checkTrace(p, calls); len(failures) != 0 {
				t.Fatal("offline discovery/read rejected", failures)
			}
			for _, name := range []string{"video_download", "media_convert", "run_program", "search_web", "write_file"} {
				if failures := checkTrace(p, append(calls, call{tool: name, success: &ok})); len(failures) == 0 {
					t.Fatal("execution accepted in offline probe", name)
				}
			}
		})
	}
}

func TestAnswerSchemaDryValidationAndSchemaOnlyProbe(t *testing.T) {
	dir := t.TempDir()
	p := Probe{Task: "return JSON", Tier: "signal", AnswerSchema: json.RawMessage(`{"type":"object","properties":{"verified":{"const":false}},"required":["verified"],"additionalProperties":false}`)}
	write := func() {
		t.Helper()
		data, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "schema.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	if _, err := loadProbes(dir, ""); err != nil {
		t.Fatal("schema alone is an assertion", err)
	}
	for _, answer := range []string{`{"verified":false,"verified":true}`, `{"verified":true,"verified":false}`} {
		if failures := checkAnswer(p, answer); len(failures) == 0 {
			t.Fatal("conflicting duplicate verification accepted", answer)
		}
	}
	p.AnswerSchema = json.RawMessage(`{"type":"invalid"}`)
	write()
	if _, err := loadProbes(dir, ""); err == nil {
		t.Fatal("invalid schema accepted before inference")
	}
}

func TestMediaTrackDefaultsCriteria(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	probes, err := loadProbes("eval/probes", "49-media-track-defaults")
	if err != nil {
		t.Fatal(err)
	}
	if len(probes) != 1 {
		t.Fatalf("expected one track defaults probe, got %d", len(probes))
	}

	answer := `{"download_path":"track.m4a","download_mode":"audio","operations":["video_download","media_inspect"],"verified_official":false,"reason":"Keep the source audio and inspect it; the uploader is not verified as the artist."}`
	failures := checkAnswer(probes[0], answer)
	if len(failures) != 0 {
		t.Fatal("valid source-audio plan rejected", failures)
	}

	invalid := []string{
		strings.Replace(answer, "track.m4a", "track.mp3", 1),
		strings.Replace(answer, `"audio"`, `"merged"`, 1),
		strings.Replace(answer, `"video_download","media_inspect"`, `"video_download","media_convert","media_inspect"`, 1),
		strings.Replace(answer, `"video_download","media_inspect"`, `"video_download"`, 1),
		strings.Replace(answer, `"verified_official":false`, `"verified_official":true`, 1),
		"Here is the plan: " + answer,
	}
	for _, text := range invalid {
		failures = checkAnswer(probes[0], text)
		if len(failures) == 0 {
			t.Fatalf("invalid source-audio plan accepted: %s", text)
		}
	}
}
