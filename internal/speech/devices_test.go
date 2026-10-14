package speech

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMicrophoneDiscoveryFiltersVideoAndAliases(t *testing.T) {
	text := `[dshow] "Webcam" (video)
[dshow] "Microphone (Storm Pro)" (audio)
[dshow] Alternative name "@device_cm_123"
[dshow] "Microphone (Storm Pro)" (audio)
[dshow] "Microphone (ARDOR)" (audio)`
	got := parseMicrophones(text)
	if len(got) != 2 || got[0] != "Microphone (Storm Pro)" || got[1] != "Microphone (ARDOR)" {
		t.Fatalf("%v", got)
	}
}
func TestInstalledMicrophoneEnumeration(t *testing.T) {
	if os.Getenv("TARS_DEVICE_TEST") == "" {
		t.Skip("device enumeration opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	devices, err := Microphones(ctx, "ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	t.Log(strings.Join(devices, "; "))
}
