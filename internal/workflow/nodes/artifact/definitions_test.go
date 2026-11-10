package artifact

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/store"
)

func TestReadTextRejectsBinaryAndLargeArtifacts(t *testing.T) {
	db, err := store.OpenStore(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	w, err := db.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	registry, err := workflow.NewRegistry(Definitions(nil)...)
	if err != nil {
		t.Fatal(err)
	}
	node, err := registry.Node("artifact_read_text")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := node.Execute(t.Context(), json.RawMessage(`{"id":"missing"}`)); err == nil {
		t.Fatal("read without run accepted")
	}
	ctx := workflow.WithArtifacts(t.Context(), w.Artifacts("read"))
	for _, data := range [][]byte{{0, 1, 2}, {255}, []byte(strings.Repeat("a", (32<<10)+1))} {
		meta, err := w.Artifacts("import").Put(t.Context(), "source", bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		args, _ := json.Marshal(map[string]string{"id": meta.ID})
		if _, err := node.Execute(ctx, args); err == nil {
			t.Fatal("non-text or oversized artifact accepted")
		}
	}
	meta, err := w.Artifacts("import").Put(t.Context(), "unicode", strings.NewReader("Привет\n"))
	if err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"id": meta.ID})
	out, err := node.Execute(ctx, args)
	if err != nil || !strings.Contains(out, "Привет") {
		t.Fatal(out, err)
	}
}
