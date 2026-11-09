package contract

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/capabilities/images"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/workspace"
)

func allowOutput(context.Context, string, json.RawMessage) error { return nil }

func TestSessionAttachmentInspectAndPixelComparison(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 20, 10))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "clipboard.png")
	if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := llm.WithAttachments(t.Context(), []llm.Message{{Role: llm.RoleUser, Images: []string{path}}})
	args := json.RawMessage(`{"paths":["attachment:1"]}`)
	out, err := (Operation{Service: images.Service{WS: ws, Inputs: testInputs{ws}}, Kind: "inspect"}).Run(ctx, args)
	if err != nil || !strings.Contains(out, `"format":"png"`) || !strings.Contains(out, `"unchanged":true`) {
		t.Fatal(out, err)
	}
	args = json.RawMessage(`{"paths":["attachment:1","attachment:1"]}`)
	out, err = (Operation{Service: images.Service{WS: ws, Inputs: testInputs{ws}}, Kind: "compare"}).Run(ctx, args)
	if err != nil || !strings.Contains(out, "semantic similarity are not measured") {
		t.Fatal(out, err)
	}
	if _, err := (Operation{Service: images.Service{WS: ws, Inputs: testInputs{ws}}, Kind: "inspect"}).Run(t.Context(), json.RawMessage(`{"paths":["attachment:1"]}`)); err == nil {
		t.Fatal("attachment leaked into another session")
	}
	entries, _ := os.ReadDir(ws.Root())
	if len(entries) != 0 {
		t.Fatal("inspection wrote project files", entries)
	}
}

func TestInspectionKeepsEqualBasenamesSeparate(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	for _, path := range []string{"first/input.png", "second/input.png"} {
		writeTestImage(t, ws, path)
	}
	out, err := (Operation{Service: images.Service{WS: ws, Inputs: testInputs{ws}}, Kind: "inspect"}).Run(t.Context(), json.RawMessage(`{"paths":["first/input.png","second/input.png"]}`))
	if err != nil {
		t.Fatal(out, err)
	}
	var result struct {
		Items []struct{ Path string } `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, item := range result.Items {
		seen[item.Path] = true
	}
	if len(result.Items) != 2 || !seen["first/input.png"] || !seen["second/input.png"] {
		t.Fatalf("sources with equal names were conflated: %s", out)
	}
}

func TestEveryImageOutputIsAuthorizedBeforePublication(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	original := writeTestImage(t, ws, "input.png")
	denied := errors.New("second destination denied")
	var destinations []string
	args := json.RawMessage(`{"paths":["input.png"],"widths":[8,16],"out_dir":"out"}`)
	op := Operation{Service: images.Service{WS: ws, Inputs: testInputs{ws}}, Kind: "variants", AuthorizeOutput: func(_ context.Context, path string, got json.RawMessage) error {
		if !bytes.Equal(got, args) {
			t.Fatal("authorization lost original arguments")
		}
		destinations = append(destinations, filepath.ToSlash(path))
		if _, err := ws.Stat("out"); !os.IsNotExist(err) {
			t.Fatal("publication began before all destinations were authorized")
		}
		if len(destinations) == 2 {
			return denied
		}
		return nil
	}}
	if out, err := op.Run(t.Context(), args); !errors.Is(err, denied) || out != "" {
		t.Fatalf("denied output: %q %v", out, err)
	}
	if len(destinations) != 2 || destinations[0] == destinations[1] || !strings.HasPrefix(destinations[0], "out/") || !strings.HasPrefix(destinations[1], "out/") {
		t.Fatalf("destination authorization: %v", destinations)
	}
	if _, err := ws.Stat("out"); !os.IsNotExist(err) {
		t.Fatal("denied batch published files")
	}
	after, err := ws.ReadFile("input.png")
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("denied batch changed its source", err)
	}
}

func writeTestImage(t *testing.T, ws *workspace.Workspace, path string) []byte {
	t.Helper()
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 32, 24))); err != nil {
		t.Fatal(err)
	}
	if err := ws.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteFile(path, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestUnsupportedProfileBlocksPreviewAndWrite(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 32, 24))); err != nil {
		t.Fatal(err)
	}
	// The decoder ignores ICC metadata; the operation must still respect it.
	payload := []byte("profile\x00\x00")
	chunk := make([]byte, len(payload)+12)
	binary.BigEndian.PutUint32(chunk, uint32(len(payload)))
	copy(chunk[4:], "iCCP")
	copy(chunk[8:], payload)
	binary.BigEndian.PutUint32(chunk[len(chunk)-4:], crc32.ChecksumIEEE(chunk[4:len(chunk)-4]))
	raw := append([]byte{}, b.Bytes()[:33]...)
	raw = append(raw, chunk...)
	raw = append(raw, b.Bytes()[33:]...)
	if err := ws.WriteFile("profile.png", raw, 0600); err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{"preview", "variants"} {
		args := `{"paths":["profile.png"],"widths":[16]}`
		if op == "variants" {
			args = `{"paths":["profile.png"],"widths":[16],"out_dir":"out"}`
		}
		out, err := (Operation{Service: images.Service{WS: ws, Inputs: testInputs{ws}}, Kind: op, AuthorizeOutput: allowOutput}).Run(t.Context(), json.RawMessage(args))
		if err == nil || !strings.Contains(err.Error(), "unsupported_metadata") || strings.Contains(err.Error(), "Temp") {
			t.Fatalf("%s: %v %s", op, err, out)
		}
		var result struct {
			Status  string                       `json:"status"`
			Summary struct{ Files, Blocked int } `json:"summary"`
		}
		if e := json.Unmarshal([]byte(out), &result); e != nil {
			t.Fatal(e)
		}
		if result.Status != "blocked" || result.Summary.Files != 1 || result.Summary.Blocked != 1 || strings.Contains(out, "--apply") {
			t.Fatalf("misleading report: %s", out)
		}
	}
	if _, err := ws.Stat("out"); !os.IsNotExist(err) {
		t.Fatal("blocked variant created output")
	}
	after, _ := ws.ReadFile("profile.png")
	if !bytes.Equal(after, raw) {
		t.Fatal("source modified")
	}
}

func TestImageWorkflowPreservesSourcesAndOutputs(t *testing.T) {
	dir := t.TempDir()
	im := image.NewNRGBA(image.Rect(0, 0, 32, 24))
	im.Set(1, 1, color.NRGBA{R: 255, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	original := append([]byte(nil), b.Bytes()...)
	if err := os.WriteFile(filepath.Join(dir, "input.png"), original, 0600); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	call := func(op, args string) string {
		t.Helper()
		out, err := (Operation{Service: images.Service{WS: ws, Inputs: testInputs{ws}}, Kind: op, AuthorizeOutput: allowOutput}).Run(t.Context(), json.RawMessage(args))
		if err != nil {
			t.Fatalf("%s: %v %s", op, err, out)
		}
		var result struct {
			Status  string `json:"status"`
			Sources []struct {
				Before    string `json:"sha256_before"`
				After     string `json:"sha256_after"`
				Unchanged *bool  `json:"unchanged"`
			} `json:"sources"`
		}
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"inspect": "inspected", "preview": "previewed", "variants": "written", "compare": "compared"}[op]
		if result.Status != want || len(result.Sources) == 0 || result.Sources[0].Unchanged == nil || !*result.Sources[0].Unchanged || result.Sources[0].Before != result.Sources[0].After {
			t.Fatalf("bad native outcome/evidence: %s", out)
		}
		return out
	}
	call("inspect", `{"paths":["input.png"]}`)
	call("preview", `{"paths":["input.png"],"widths":[16]}`)
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatal("preview wrote to workspace")
	}
	call("variants", `{"paths":["input.png"],"widths":[16],"out_dir":"out"}`)
	outputs, err := os.ReadDir(filepath.Join(dir, "out"))
	if err != nil || len(outputs) != 1 {
		t.Fatalf("outputs %v %v", outputs, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "out", outputs[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width != 16 || config.Height != 12 {
		t.Fatalf("wrong dimensions %+v %v", config, err)
	}
	call("compare", `{"paths":["input.png","out/`+outputs[0].Name()+`"]}`)
	failedOutput, err := (Operation{Service: images.Service{WS: ws, Inputs: testInputs{ws}}, Kind: "variants", AuthorizeOutput: allowOutput}).Run(t.Context(), json.RawMessage(`{"paths":["input.png"],"widths":[8],"out_dir":"out"}`))
	if err == nil || !strings.Contains(failedOutput, `"status":"failed"`) {
		t.Fatal("reused output directory")
	}
	if !strings.Contains(failedOutput, `"failure_stage":"publication"`) || !strings.Contains(failedOutput, `"status":"not_published"`) {
		t.Fatalf("publication failure confused with encoding: %s", failedOutput)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "input.png"))
	if !bytes.Equal(original, after) {
		t.Fatal("source changed")
	}
	still, _ := os.ReadFile(filepath.Join(dir, "out", outputs[0].Name()))
	if !bytes.Equal(data, still) {
		t.Fatal("existing output changed")
	}
	for _, args := range []string{`{"paths":["../outside.png"]}`, `{"paths":["input.png"],"unknown":true}`} {
		if _, err := (Operation{Service: images.Service{WS: ws, Inputs: testInputs{ws}}, Kind: "inspect"}).Run(t.Context(), json.RawMessage(args)); err == nil {
			t.Fatalf("accepted %s", args)
		}
	}
}

func TestImageDoesNotFollowEscapingSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.png"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "escape")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	ws, err := workspace.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if _, err := (Operation{Service: images.Service{WS: ws, Inputs: testInputs{ws}}, Kind: "inspect"}).Run(t.Context(), json.RawMessage(`{"paths":["escape/secret.png"]}`)); err == nil {
		t.Fatal("read escaped workspace")
	}
}

func TestFailedBatchPublishesNothing(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 32, 24))); err != nil {
		t.Fatal(err)
	}
	good := b.Bytes()
	bad := append([]byte{}, good...)
	// DecodeConfig accepts the header; pixel decoding must reject the bad IDAT CRC.
	for off := 8; off+12 <= len(bad); {
		n := int(binary.BigEndian.Uint32(bad[off : off+4]))
		if string(bad[off+4:off+8]) == "IDAT" {
			bad[off+8+n] ^= 1
			break
		}
		off += n + 12
	}
	if err := ws.WriteFile("good.png", good, 0600); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteFile("bad.png", bad, 0600); err != nil {
		t.Fatal(err)
	}
	out, err := (Operation{Service: images.Service{WS: ws, Inputs: testInputs{ws}}, Kind: "variants"}).Run(t.Context(), json.RawMessage(`{"paths":["good.png","bad.png"],"widths":[16],"out_dir":"out"}`))
	if err == nil {
		t.Fatalf("failed batch succeeded: %s", out)
	}
	var report struct {
		Summary struct{ Failed int } `json:"summary"`
		Totals  map[string]int       `json:"totals"`
		Items   []struct {
			Written bool
			Output  string
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	if report.Summary.Failed != 1 || report.Totals["writtenOutputs"] != 0 {
		t.Fatalf("bad failure accounting: %s", out)
	}
	for _, item := range report.Items {
		if item.Written || item.Output != "" {
			t.Fatalf("unpublished file reported as written: %s", out)
		}
	}
	if _, err := ws.Stat("out"); !os.IsNotExist(err) {
		t.Fatal("failed batch published outputs")
	}
}

type testInputs struct{ WS *workspace.Workspace }

func (s testInputs) IsReference(path string) bool { return llm.IsAttachment(path) }
func (s testInputs) Open(ctx context.Context, path string) (*os.File, error) {
	return llm.OpenImage(ctx, path, s.WS.Open)
}
