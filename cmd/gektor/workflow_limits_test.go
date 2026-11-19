package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkerLimitsRejectBeforeOpeningWorkspaceOrStore(t *testing.T) {
	for _, args := range [][]string{
		{"serve", "-max-jobs", "0"},
		{"serve", "-max-jobs", "33"},
		{"serve", "-max-operations", "-1"},
		{"serve", "-max-operations", "33"},
		{"jobs", "-max-jobs", "2"},
	} {
		root := t.TempDir()
		workspace := filepath.Join(root, "missing-workspace")
		directory := filepath.Join(root, "missing-store")
		args = append(args, "-workspace", workspace)
		var out bytes.Buffer
		err := workflowQueue(t.Context(), args, &out, directory)
		want := "must be between 1 and 32"
		if args[0] != "serve" {
			want = "flag provided but not defined"
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatal("worker option reached workspace handling", args, err)
		}
		for _, path := range []string{workspace, directory} {
			_, statErr := os.Stat(path)
			if !os.IsNotExist(statErr) {
				t.Fatal("invalid options touched storage", args, path, statErr)
			}
		}
	}
}
