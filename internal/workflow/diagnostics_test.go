package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

func TestDiagnosticsKeepTypedStepAndEscapedSchemaPointers(t *testing.T) {
	err := ValidateJSON(json.RawMessage(`{"a/b~c":3}`), json.RawMessage(`{"type":"object","properties":{"a/b~c":{"type":"string"}}}`))
	if err == nil {
		t.Fatal("expected schema failure")
	}
	located := &StepError{StepID: "actual", Pointer: "/args", Err: err}
	wrapped := fmt.Errorf("recipe mentions step decoy: %w", located)
	diagnostics := Diagnose(wrapped, Definition{Steps: []Step{{ID: "decoy"}}})
	if len(diagnostics) != 1 || diagnostics[0].StepID != "actual" || diagnostics[0].Pointer != "/args/a~1b~0c" {
		t.Fatal(diagnostics)
	}
	if !errors.Is(wrapped, err) {
		t.Fatal("lost underlying validation error")
	}
}

func TestDiagnosticsReportEverySchemaLeaf(t *testing.T) {
	err := ValidateJSON(json.RawMessage(`{"steps":[{"args":{"first":3,"second":4}}]}`), json.RawMessage(`{"type":"object","properties":{"steps":{"type":"array","items":{"type":"object","properties":{"args":{"type":"object","properties":{"first":{"type":"string"},"second":{"type":"boolean"}}}}}}}}`))
	if err == nil {
		t.Fatal("expected schema failure")
	}
	diagnostics := Diagnose(err, Definition{Steps: []Step{{ID: "transform"}}})
	pointers := map[string]bool{}
	for _, diagnostic := range diagnostics {
		if diagnostic.StepID != "transform" || diagnostic.Message == "" {
			t.Fatal(diagnostic)
		}
		pointers[diagnostic.Pointer] = true
	}
	if len(diagnostics) != 2 || !pointers["/args/first"] || !pointers["/args/second"] {
		t.Fatal(diagnostics)
	}
	withoutDefinition := Diagnose(err, Definition{})
	if withoutDefinition[0].StepID != "" || withoutDefinition[0].Pointer[:7] != "/steps/" {
		t.Fatal(withoutDefinition)
	}
}

func TestDiagnosticsLeaveUnknownLocationsBlank(t *testing.T) {
	if Diagnose(nil, Definition{}) != nil {
		t.Fatal("nil error produced diagnostics")
	}
	sentinel := errors.New("read denied")
	diagnostics := Diagnose(sentinel, Definition{Steps: []Step{{ID: "unrelated"}}})
	if len(diagnostics) != 1 || diagnostics[0].StepID != "" || diagnostics[0].Pointer != "" || diagnostics[0].Message != sentinel.Error() {
		t.Fatal(diagnostics)
	}
}
