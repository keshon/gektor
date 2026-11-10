package automation

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestTemplatePathValidationChecksLiteralsAndDefersReferences(t *testing.T) {
	var checked []string
	check := func(path string) error {
		checked = append(checked, path)
		if strings.HasPrefix(path, "../") {
			return fmt.Errorf("outside workspace")
		}
		return nil
	}
	raw := json.RawMessage(`{"inputs":["input.png",{"$input":"other"}],"path":{"$step":"output"},"code":"${input is ordinary code here}"}`)
	if err := ValidateLiteralPaths(raw, []string{"inputs", "path"}, check); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(checked, []string{"input.png"}) {
		t.Fatal(checked)
	}
	for _, raw := range []json.RawMessage{json.RawMessage(`{"path":"../secret"}`), json.RawMessage(`{"path":"${input.source}"}`)} {
		if err := ValidateLiteralPaths(raw, []string{"path"}, check); err == nil {
			t.Fatal("unsafe literal accepted", string(raw))
		}
	}
}
