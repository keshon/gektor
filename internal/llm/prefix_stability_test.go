package llm

import (
	"bytes"
	"encoding/json"
	"testing"
)

// A backend that caches a prompt reuses it only when the token sequence is
// unchanged from the start. Tool definitions are not messages, but every
// chat template that supports tool calling renders them into the prompt,
// ahead of the conversation. So their bytes sit at the front of what a
// backend tokenises and are the one part of the request that must not
// wobble between steps.
//
// Note what is deliberately NOT measured here: the raw JSON byte prefix.
// Messages are serialised before tools in the request body, so growing the
// conversation always "breaks" the byte prefix even when the templated
// prompt is a clean extension. Measuring raw bytes measures the field order
// in the encoder, not cache reuse.

func TestToolDefinitionsSerialiseIdenticallyEveryTime(t *testing.T) {
	defs := []ToolDef{
		{Name: "write_file", Description: "w", Parameters: json.RawMessage(`{"type":"object"}`)},
		{Name: "read_file", Description: "r", Parameters: json.RawMessage(`{"type":"object"}`)},
		{Name: "grep_files", Description: "g", Parameters: json.RawMessage(`{"type":"object"}`)},
	}
	first, err := json.Marshal(defs)
	if err != nil {
		t.Fatal(err)
	}
	// Go randomises map iteration per range. A tool set assembled from a
	// map and marshalled per call reorders unpredictably, which is stable in
	// a test that marshals once and is not stable in a run.
	for i := 0; i < 500; i++ {
		got, err := json.Marshal(defs)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, first) {
			t.Fatalf("tool JSON differed on call %d:\n%s\n%s", i, first, got)
		}
	}
}

// RawMessage must reach the wire as the schema the tool wrote, not re-encoded
// through a map. json.Marshal would sort keys and stay stable, but a schema
// that Compact()ed away whitespace on the way is still a schema - just not
// the one whose length the cache key was built from.
func TestToolParametersAreSentVerbatim(t *testing.T) {
	const schema = `{"type":"object","properties":{"path":{"type":"string"}},` +
		`"required":["path"],"additionalProperties":false}`
	def := ToolDef{Name: "read_file", Description: "r", Parameters: json.RawMessage(schema)}
	blob, err := json.Marshal([]ToolDef{def})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(blob, []byte(schema)) {
		t.Fatalf("schema was re-encoded on the way out:\n%s", blob)
	}
}
