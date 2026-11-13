package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/llm"
)

func TestOfflineContextStartsNothingAndReportsDeferredCatalog(t *testing.T) {
	t.Setenv("PATH", "")
	root := t.TempDir()
	config := filepath.Join(t.TempDir(), "capabilities.json")
	if err := os.WriteFile(config, []byte(`{"version":1,"packs":[{"id":"optional","version":"1","description":"optional integration","type":"mcp","command":["GEKTOR_MUST_NOT_START"]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	options, err := parseLaunchOptions([]string{"-context-report", "-workspace", root, "-backend", "http://127.0.0.1:1", "-capabilities", config, "question"})
	if err != nil {
		t.Fatal(err)
	}
	if options.browser != "chrome" {
		t.Fatal("bundled browser is not available by default", options.browser)
	}
	var out bytes.Buffer
	if err := inspectStartup(resolvedLaunch{launchOptions: options}, &out); err != nil {
		t.Fatal(err)
	}
	var report llm.ContextReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.EstimatedTokens <= 0 || report.BackendTokens != 0 {
		t.Fatal(report)
	}
	for _, tool := range report.Tools {
		if tool.Name == "cap__social__read_post" || tool.Name == "mcp__optional__read" || strings.HasPrefix(tool.Name, "mcp__browser__") || tool.Name == "browser_observe" {
			t.Fatal("inactive tool schema was loaded")
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("offline preview wrote workspace state", entries, err)
	}
}
