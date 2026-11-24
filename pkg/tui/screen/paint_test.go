package screen

import (
	"strings"
	"testing"
)

func TestPaintSurfaceRestoresLocalBackground(t *testing.T) {
	before := CurrentPalette()
	content := "start\x1b[31mred\x1b[0m gap\x1b[49m end"
	painted := PaintSurface(content, "#123456")
	background := "\x1b[48;2;18;52;86m"
	if strings.Count(painted, background) != 3 || !strings.Contains(painted, "\x1b[31mred") {
		t.Fatal("surface reset lost local background or semantic text color", painted)
	}
	if CurrentPalette() != before {
		t.Fatal("local surface changed the application palette")
	}
}
