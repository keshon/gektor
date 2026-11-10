// Package record adapts workspace record collections to workflow nodes.
package record

import (
	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/automation/records"
	"github.com/keshon/gektor/internal/workspace"
)

func Definitions(ws *workspace.Workspace, store string) []automation.Node {
	var nodes []automation.Node
	for _, write := range []bool{false, true} {
		op := records.Operation{WS: ws, StoreDir: store, Write: write}
		nodes = append(nodes, automation.Node{Name: op.Name(), Version: 1, Description: op.Description(), InputSchema: op.Schema(), OutputSchema: op.OutputSchema(), ReadOnly: op.ReadOnly(), RetrySafe: op.RetrySafe(), Resources: records.Resources, Execute: op.Run})
	}
	return nodes
}
