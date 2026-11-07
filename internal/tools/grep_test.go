package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/workspace"
)

func TestGrepFiles_FindsMatchesAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	ws, _ := workspace.New(dir)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\nfunc Foo() {}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("package b\nfunc Bar() { Foo() }\n"), 0o644)

	tool := GrepFiles{WS: ws}
	args, _ := json.Marshal(map[string]string{"path": ".", "pattern": "Foo"})
	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "a.go:2:") || !strings.Contains(out, "b.go:2:") {
		t.Fatalf("expected matches in both files, got: %q", out)
	}
}

func TestGrepExplicitIgnoredTargets(t *testing.T) {
	ws, root := testWS(t)
	initGitRepo(t, root)
	writeTmp(t, root, ".gitignore", "sandbox/\n")
	if err := os.Mkdir(filepath.Join(root, "sandbox"), 0700); err != nil {
		t.Fatal(err)
	}
	writeTmp(t, root, "sandbox/page.html", "<script>initScene();</script>\n")
	for _, path := range []string{"sandbox/page.html", "sandbox", "."} {
		args, _ := json.Marshal(map[string]any{"path": path, "pattern": "script"})
		out, err := (GrepFiles{WS: ws}).Run(t.Context(), args)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "initScene") != (path != ".") {
			t.Fatalf("search %s: %s", path, out)
		}
	}
}

func TestGrepLiteralAndInvalidGlob(t *testing.T) {
	ws, root := testWS(t)
	writeTmp(t, root, "a.txt", "keys.push({\nkeysXpush({\n")
	out, err := (GrepFiles{WS: ws}).Run(t.Context(), json.RawMessage(`{"pattern":"keys.push({","literal":true}`))
	if err != nil || !strings.Contains(out, "a.txt:1:") || strings.Contains(out, "keysX") {
		t.Fatalf("literal search: %q %v", out, err)
	}
	if _, err := (GrepFiles{WS: ws}).Run(t.Context(), json.RawMessage(`{"pattern":"keys","glob":"["}`)); err == nil {
		t.Fatal("malformed glob silently returned no matches")
	}
}

func TestGrepContextMergesWindowsAndKeepsMatchLabels(t *testing.T) {
	ws, dir := testWS(t)
	writeTmp(t, dir, "a.txt", "one\ntwo\nMATCH three\nMATCH four\nfive\nsix\nseven\neight\nMATCH nine\nten\n")
	out, err := (GrepFiles{WS: ws}).Run(t.Context(), json.RawMessage(`{"path":".","pattern":"MATCH","before_context":1,"after_context":1}`))
	if err != nil {
		t.Fatal(err)
	}
	want := "a.txt-2-two\na.txt:3:MATCH three\na.txt:4:MATCH four\na.txt-5-five\n--\na.txt-8-eight\na.txt:9:MATCH nine\na.txt-10-ten\n"
	if out != want {
		t.Fatalf("overlap duplicated context or mislabelled a match:\ngot: %s\nwant: %s", out, want)
	}
}

func TestGrepContextSeparatesFilesAndSharesMatchLimit(t *testing.T) {
	ws, dir := testWS(t)
	writeTmp(t, dir, "a.txt", "before\nMATCH a\nafter\n")
	writeTmp(t, dir, "b.txt", "before\nMATCH b\nafter\n")
	out, err := (GrepFiles{WS: ws}).Run(t.Context(), json.RawMessage(`{"pattern":"MATCH","before_context":1,"after_context":1}`))
	want := "a.txt-1-before\na.txt:2:MATCH a\na.txt-3-after\n--\nb.txt-1-before\nb.txt:2:MATCH b\nb.txt-3-after\n"
	if err != nil || out != want {
		t.Fatalf("file contexts merged: %q %v", out, err)
	}
	writeTmp(t, dir, "a.txt", strings.Repeat("MATCH\n", 150))
	writeTmp(t, dir, "b.txt", strings.Repeat("MATCH\n", 150))
	out, err = (GrepFiles{WS: ws}).Run(t.Context(), json.RawMessage(`{"pattern":"MATCH"}`))
	if err != nil || strings.Count(out, "MATCH") != 200 || !strings.Contains(out, "b.txt:50:MATCH") || strings.Contains(out, "b.txt:51:") || !strings.Contains(out, "capped at 200 matches") {
		t.Fatalf("match limit was not shared across files: %q %v", out, err)
	}
}

func TestGrepIncludesIgnoredFilesWithoutExposingSensitiveContent(t *testing.T) {
	ws, dir := testWS(t)
	writeTmp(t, dir, ".env", "MATCH private-secret\n")
	writeTmp(t, dir, "public.txt", "MATCH public-value\n")
	out, err := (GrepFiles{WS: ws}).Run(t.Context(), json.RawMessage(`{"pattern":"MATCH","include_ignored":true}`))
	if err != nil || strings.Contains(out, "private-secret") || !strings.Contains(out, "public-value") || !strings.Contains(out, "skipped 1 sensitive files") {
		t.Fatalf("sensitive file bypassed search restrictions: %q %v", out, err)
	}
}

func TestGrepCancellationPreservesErrorIdentity(t *testing.T) {
	ws, dir := testWS(t)
	writeTmp(t, dir, "a.txt", "MATCH\n")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := (GrepFiles{WS: ws}).Run(ctx, json.RawMessage(`{"pattern":"MATCH","include_ignored":true}`))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled search: %v", err)
	}
}

func TestGrepContextBoundsOutputAndArguments(t *testing.T) {
	ws, dir := testWS(t)
	writeTmp(t, dir, "a.txt", strings.Repeat("MATCH "+strings.Repeat("x", 600)+"\n", 300))
	out, err := (GrepFiles{WS: ws}).Run(t.Context(), json.RawMessage(`{"path":".","pattern":"MATCH","before_context":10,"after_context":10}`))
	if err != nil || len(out) > readMaxBytes+200 || !strings.Contains(out, "output capped at 48 KiB") {
		t.Fatalf("unbounded or silent truncation: len=%d err=%v", len(out), err)
	}
	for _, args := range []string{
		`{"path":".","pattern":"MATCH","before_context":-1}`,
		`{"path":".","pattern":"MATCH","after_context":11}`,
	} {
		if _, err := (GrepFiles{WS: ws}).Run(t.Context(), json.RawMessage(args)); err == nil {
			t.Fatalf("accepted unbounded context: %s", args)
		}
	}
}

func TestGrepFiles_GlobFilter(t *testing.T) {
	dir := t.TempDir()
	ws, _ := workspace.New(dir)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("token\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("token\n"), 0o644)

	tool := GrepFiles{WS: ws}
	args, _ := json.Marshal(map[string]string{"path": ".", "pattern": "token", "glob": "*.go"})
	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "a.go") || strings.Contains(out, "a.txt") {
		t.Fatalf("glob filter not applied correctly: %q", out)
	}
}

func TestGrepFiles_NoMatches(t *testing.T) {
	dir := t.TempDir()
	ws, _ := workspace.New(dir)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("nothing here\n"), 0o644)

	tool := GrepFiles{WS: ws}
	args, _ := json.Marshal(map[string]string{"path": ".", "pattern": "zzz_not_present"})
	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "no matches" {
		t.Fatalf("got %q, want %q", out, "no matches")
	}
}

func TestGrepFiles_BadPatternErrors(t *testing.T) {
	dir := t.TempDir()
	ws, _ := workspace.New(dir)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	tool := GrepFiles{WS: ws}
	args, _ := json.Marshal(map[string]string{"path": ".", "pattern": "("})
	if _, err := tool.Run(context.Background(), args); err == nil {
		t.Fatal("expected error for invalid regex")
	}
}

func TestGrepFiles_EmptyPatternErrors(t *testing.T) {
	dir := t.TempDir()
	ws, _ := workspace.New(dir)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	tool := GrepFiles{WS: ws}
	args, _ := json.Marshal(map[string]string{"path": ".", "pattern": ""})
	if _, err := tool.Run(context.Background(), args); err == nil {
		t.Fatal("expected error for empty pattern")
	}
}

func TestGrepFiles_UnknownFieldErrors(t *testing.T) {
	dir := t.TempDir()
	ws, _ := workspace.New(dir)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	tool := GrepFiles{WS: ws}
	args, _ := json.Marshal(map[string]string{"path": ".", "command": "foo"})
	_, err := tool.Run(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("error = %v, want unknown field mention", err)
	}
}

func TestGrepFiles_SkipsBinaryFiles(t *testing.T) {
	dir := t.TempDir()
	ws, _ := workspace.New(dir)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}

	// Reproduces the real failure: a binary file with long stretches of
	// non-newline bytes that a wildcard pattern would happily "match",
	// dumping raw control-character garbage into the result.
	binData := make([]byte, 5000)
	for i := range binData {
		binData[i] = byte(i % 251) // varied bytes, deliberately avoiding newlines mostly
	}
	binData[100] = 0 // the actual signal looksBinary checks for
	os.WriteFile(filepath.Join(dir, "app.exe"), binData, 0o644)
	os.WriteFile(filepath.Join(dir, "app.go"), []byte("package main\nfunc mask() {}\n"), 0o644)

	tool := GrepFiles{WS: ws}
	args, _ := json.Marshal(map[string]string{"path": ".", "pattern": ".*"})
	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(out, "app.exe") {
		preview := out
		if len(preview) > 200 {
			preview = preview[:200]
		}
		t.Fatalf("binary file should have been skipped entirely, got: %q", preview)
	}
	if !strings.Contains(out, "app.go") {
		t.Fatalf("expected the real text file to still match, got: %q", out)
	}
}

func TestGrepFiles_TruncatesLongLines(t *testing.T) {
	dir := t.TempDir()
	ws, _ := workspace.New(dir)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}

	longLine := "needle" + strings.Repeat("x", 2000)
	os.WriteFile(filepath.Join(dir, "minified.js"), []byte(longLine+"\n"), 0o644)

	tool := GrepFiles{WS: ws}
	args, _ := json.Marshal(map[string]string{"path": ".", "pattern": "needle"})
	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(out) > 1000 {
		t.Fatalf("expected the long line to be truncated, result was %d bytes", len(out))
	}
	if !strings.Contains(out, "...(truncated)") {
		t.Fatalf("expected a truncation marker, got: %q", out)
	}
}
