//go:build ignore

package main

import "testing"

func TestCandidateRejectsApplicationImports(t *testing.T) {
	for _, path := range []string{
		sourceModule + "/internal/app",
		sourceModule + "/internal/tui/agent",
		sourceModule + "/internal/tui/components-extra",
	} {
		data := []byte("package example\nimport _ " + `"` + path + `"`)
		if checkImports("example.go", data) == nil {
			t.Fatalf("candidate admitted application import %s", path)
		}
	}
}

func TestCandidateAllowsControlAndExternalImports(t *testing.T) {
	data := []byte(`package example
import (
 "strings"
 tea "charm.land/bubbletea/v2"
 "github.com/keshon/gektor/internal/tui/screen"
 "github.com/keshon/gektor/internal/tui/components/fields"
)
`)
	if err := checkImports("example.go", data); err != nil {
		t.Fatal(err)
	}
}
