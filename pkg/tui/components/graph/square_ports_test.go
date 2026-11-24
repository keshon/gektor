package graph

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestSquarePortsPreserveNodeTextAndGeometry(t *testing.T) {
	model := Model{Nodes: []Node{{ID: "one", Title: "file.go · text", Point: Point{X: 1, Y: 1}}}, Selected: "one"}
	for _, square := range []bool{false, true} {
		rows := strings.Split(ansi.Strip(model.View(50, 15, Styles{SquarePorts: square})), "\n")
		output := strings.Join(rows, "\n")
		if !strings.Contains(output, "file.go · text") {
			t.Fatal("node text changed", output)
		}
		marker := "○"
		if square {
			marker = "□"
		}
		if strings.Count(output, marker) != 2 {
			t.Fatal("port shape mismatch", square, output)
		}
		for _, row := range rows {
			if ansi.StringWidth(row) != 50 {
				t.Fatal("port shape changed graph width", row)
			}
		}
	}
}
