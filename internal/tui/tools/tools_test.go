package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/executables"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "-version" {
		marker := os.Getenv("GEKTOR_TOOLS_PROBE_MARKER")
		if marker != "" {
			err := os.WriteFile(marker, []byte("probed"), 0600)
			if err != nil {
				os.Exit(2)
			}
		}
		if os.Getenv("GEKTOR_TOOLS_PROBE_BLOCK") == "1" {
			for {
				time.Sleep(time.Second)
			}
		}
		fmt.Println("fixture version 1")
		return
	}
	os.Exit(m.Run())
}

func toolsFixture(t *testing.T) (*Model, string) {
	t.Helper()
	dir := t.TempDir()
	marker := filepath.Join(dir, "probe")
	t.Setenv("GEKTOR_TOOLS_PROBE_MARKER", marker)
	r := executables.New(filepath.Join(dir, "applications.json"))
	err := r.Save(executables.Config{Applications: map[string]executables.Setting{"ffmpeg": {Path: os.Args[0]}, "node": {Disabled: true}}})
	if err != nil {
		t.Fatal(err)
	}
	model := New(r)
	t.Cleanup(model.Close)
	return model, marker
}

func TestOpeningAndSavingPreferencesNeverRunsExecutable(t *testing.T) {
	model, marker := toolsFixture(t)
	view := model.Media(true)
	if len(view.Table.Model.Rows) != 5 {
		t.Fatal("missing dependencies")
	}
	_, err := model.Preferences("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	err = model.Save("ffmpeg", map[string]string{"path": os.Args[0], "disabled": "false"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = os.Stat(marker)
	if !os.IsNotExist(err) {
		t.Fatal("preferences implicitly executed a process", err)
	}
	config, err := model.Resolver.Load()
	if err != nil || !config.Applications["node"].Disabled {
		t.Fatal("editing lost another program", err)
	}
	command := model.Check(t.Context(), "ffmpeg")
	result := command().(CheckResult)
	if !model.Accept(result) || result.observation.err != nil || result.observation.status.State != "ready" || !strings.Contains(result.observation.status.Version, "fixture version 1") {
		t.Fatal(result)
	}
	_, err = os.Stat(marker)
	if err != nil {
		t.Fatal("explicit check did not run", err)
	}
}

func TestClosingJoinsCheckEvenBeforeCommandIsDelivered(t *testing.T) {
	model, marker := toolsFixture(t)
	t.Setenv("GEKTOR_TOOLS_PROBE_BLOCK", "1")
	command := model.Check(t.Context(), "ffmpeg")
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := os.Stat(marker)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("probe never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	model.Close()
	if model.cancel != nil || model.done != nil || model.checking != "" {
		t.Fatal("closed check retained its lifetime")
	}
	if model.Accept(command().(CheckResult)) {
		t.Fatal("late check updated a closed view")
	}
}
