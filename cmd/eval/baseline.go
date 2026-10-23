package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Keep the initial baseline small and explicit. Changes to these scenarios are
// experiment changes, not a way to improve an existing score.
var coreProbes = []string{
	"05-patch-unique", "09-grep-then-edit", "13-dev-server-trap",
	"18-overflow-recovery", "23-browser-unavailable", "24-nested-skills",
	"25-ledger-repair",
}

func selectSuite(probes []Probe, name string) ([]Probe, error) {
	if name == "" {
		return probes, nil
	}
	if name != "core" {
		return nil, fmt.Errorf("unknown suite %q; available: core", name)
	}
	selected := make([]Probe, 0, len(coreProbes))
	for _, wanted := range coreProbes {
		found := false
		for _, p := range probes {
			if p.Name == wanted {
				selected = append(selected, p)
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("core suite is missing probe %s", wanted)
		}
	}
	return selected, nil
}

type baselineStats struct {
	Scored           int     `json:"scored"`
	Passed           int     `json:"passed"`
	Errors           int     `json:"errors"`
	Skipped          int     `json:"skipped"`
	MedianSeconds    float64 `json:"median_seconds"`
	MedianSteps      float64 `json:"median_steps"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CachedTokens     int64   `json:"cached_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	ToolFailures     int     `json:"tool_failures"`
	Clarifications   int     `json:"clarification_calls"`
}

type baselineReport struct {
	Overall baselineStats            `json:"overall"`
	Probes  map[string]baselineStats `json:"probes"`
}

func aggregateRuns(runs []Result) baselineStats {
	var s baselineStats
	var seconds, steps []float64
	for _, r := range runs {
		if r.Errored {
			s.Errors++
			continue
		}
		if r.Skipped {
			s.Skipped++
			continue
		}
		s.Scored++
		if r.Passed {
			s.Passed++
		}
		seconds = append(seconds, r.Seconds)
		steps = append(steps, float64(r.Steps))
		s.PromptTokens += r.PromptTokens
		s.CachedTokens += r.CachedTokens
		s.CompletionTokens += r.CompletionTokens
		for _, n := range r.ToolFailures {
			s.ToolFailures += n
		}
		s.Clarifications += r.Tools["ask_user"]
	}
	s.MedianSeconds = median(seconds)
	s.MedianSteps = median(steps)
	return s
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sort.Float64s(values)
	i := len(values) / 2
	if len(values)%2 == 1 {
		return values[i]
	}
	return (values[i-1] + values[i]) / 2
}

func writeSummary(dir string, runs []Result) error {
	byProbe := map[string][]Result{}
	for _, r := range runs {
		byProbe[r.Probe] = append(byProbe[r.Probe], r)
	}
	report := baselineReport{Overall: aggregateRuns(runs), Probes: map[string]baselineStats{}}
	for name, rows := range byProbe {
		report.Probes[name] = aggregateRuns(rows)
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "summary.json"), data, 0644)
}
