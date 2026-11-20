package transcript

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestActionRowKeepsStateAsMetadataAndTargetYield(t *testing.T) {
	for _, item := range []struct {
		state ActionState
		label string
	}{{ActionCompleted, "Completed"}, {ActionRunning, "Running"}, {ActionReturned, "Returned"}, {ActionSkipped, "Skipped"}, {ActionFailed, "Failed"}} {
		row := ActionRow{Action: "Read file", Target: "文件/" + strings.Repeat("long path/", 20), Name: "read_file", Failure: "HTTP 404\nmore details", State: item.state}
		for width := 0; width <= 180; width++ {
			text := ansi.Strip(row.View(width, ActionStyles{}))
			if ansi.StringWidth(text) != width || strings.Contains(text, "\n") {
				t.Fatalf("state %d width %d escaped bounds: %q", item.state, width, text)
			}
			if width >= 20 && !strings.Contains(text, item.label) {
				t.Fatalf("state disappeared at width %d: %q", width, text)
			}
			if width >= 40 && !strings.Contains(text, "Read file") {
				t.Fatalf("action disappeared at width %d: %q", width, text)
			}
			if width == 180 && !strings.Contains(text, "read_file │ "+item.label) {
				t.Fatal("roomy row lost its technical identity", text)
			}
		}
	}
}
