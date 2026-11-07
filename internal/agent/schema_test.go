package agent

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestArgumentSchemasRemainBoundedAcrossCatalogChanges(t *testing.T) {
	for i := 0; i < 2*maxArgumentSchemas; i++ {
		schema := json.RawMessage(fmt.Sprintf(`{"type":"object","required":["field%d"]}`, i))
		if err := validateArguments(json.RawMessage(fmt.Sprintf(`{"field%d":true}`, i)), schema); err != nil {
			t.Fatal(err)
		}
		if err := validateArguments(json.RawMessage(`{}`), schema); err == nil {
			t.Fatalf("schema %d lost its required field", i)
		}
	}
	argumentSchemas.Lock()
	n := len(argumentSchemas.entries)
	argumentSchemas.Unlock()
	if n > maxArgumentSchemas {
		t.Fatalf("retained %d schemas, limit %d", n, maxArgumentSchemas)
	}
}
