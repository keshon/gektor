package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func frozenProbeForTest(t *testing.T, name string) Probe {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "eval", "probes", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var probe Probe
	err = json.Unmarshal(body, &probe)
	if err != nil {
		t.Fatal(err)
	}
	return probe
}

func TestExactEditRejectsCommentAndCollateralChanges(t *testing.T) {
	probe := frozenProbeForTest(t, "05-patch-unique")
	check := probe.Verify[0]
	ws := checkWorkspaceForTest(t)
	cases := []struct {
		name string
		text string
		pass bool
	}{
		{"exact", check.Content, true},
		{"windows newline", strings.ReplaceAll(check.Content, "\n", "\r\n"), true},
		{"comment decoy", strings.Replace(check.Content, `"v2"`, `"v1"`, 1) + "// const Version = \"v2\"\n", false},
		{"broken function", strings.Replace(check.Content, `return "hello"`, `return missing`, 1), false},
		{"changed greeting", strings.Replace(check.Content, `"hello"`, `"hi"`, 1), false},
		{"extra source", check.Content + "func unrelated() {}\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ws.WriteFile(check.Path, []byte(tc.text), 0600)
			if err != nil {
				t.Fatal(err)
			}
			output, passed := RunCheck(t.Context(), check, ws)
			if passed != tc.pass {
				t.Fatal(passed, output)
			}
		})
	}
	ok, failed := true, false
	for _, success := range []*bool{&ok, &failed, nil} {
		calls := []call{{tool: "patch_file", args: `{"path":"sample.go"}`, success: success}}
		if passed := len(checkTrace(probe, calls)) == 0; passed != (success == &ok) {
			t.Fatal("patch success requirement", calls)
		}
	}
}

func TestOverflowOracleRequiresCompleteAnswerAndSuccessfulFileRead(t *testing.T) {
	probe := frozenProbeForTest(t, "18-overflow-recovery")
	contents := probe.Verify[0].Content
	for _, answer := range []string{contents, strings.TrimSuffix(contents, "\n"), strings.ReplaceAll(contents, "\n", "\r\n")} {
		if failures := checkAnswer(probe, answer); len(failures) != 0 {
			t.Fatal(failures)
		}
	}
	for _, answer := range []string{"Hello from eval fixtures", "Here are the contents:\n" + contents, contents + "Done.", strings.ToLower(contents)} {
		if len(checkAnswer(probe, answer)) == 0 {
			t.Fatal("inexact answer passed", answer)
		}
	}
	ok, failed := true, false
	valid := call{tool: "read_file", args: `{"path":"hello.txt"}`, success: &ok, output: contents}
	if failures := checkTrace(probe, []call{valid}); len(failures) != 0 {
		t.Fatal(failures)
	}
	for _, change := range []string{"failed", "wrong path", "partial content", "mutation"} {
		calls := []call{valid}
		switch change {
		case "failed":
			calls[0].success = &failed
		case "wrong path":
			calls[0].args = `{"path":"not-hello.txt"}`
		case "partial content":
			calls[0].output = "Hello from eval fixtures."
		case "mutation":
			calls = append(calls, call{tool: "patch_file"})
		}
		if len(checkTrace(probe, calls)) == 0 {
			t.Fatal("bad read-only trace passed", change)
		}
	}
}
