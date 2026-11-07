// Package record adapts workspace record collections to workflow nodes.
package record

import (
	"encoding/json"

	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/workspace"
)

func Definitions(ws *workspace.Workspace, store string) []automation.Node {
	var nodes []automation.Node
	for _, write := range []bool{false, true} {
		op := Operation{WS: ws, StoreDir: store, Write: write}
		nodes = append(nodes, automation.Node{Name: op.Name(), Version: 1, Description: op.Description(), InputSchema: op.Schema(), OutputSchema: json.RawMessage(`{"type":"object","properties":{"kind":{"type":"string"},"id":{"type":"string"},"revision":{"type":"integer"},"data":{"type":["object","null"]},"found":{"type":"boolean"},"schema_sha256":{"type":"string"},"updated":{"type":"string"}},"required":["kind","id","revision","data"]}`), ReadOnly: op.ReadOnly(), RetrySafe: op.ReadOnly(), Resources: RecordResources, Execute: op.Run})
	}
	return nodes
}
