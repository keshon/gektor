package main

import (
	"testing"

	"github.com/keshon/gektor/internal/tools"
	toolextension "github.com/keshon/gektor/internal/tools/extension"
)

func TestNativeToolExposure(t *testing.T) {
	baseline, err := nativeToolExposure("baseline")
	if err != nil || baseline != nil {
		t.Fatalf("baseline must preserve defaults: %v, %v", baseline, err)
	}
	lazy, err := nativeToolExposure("lazy-web")
	if err != nil || len(lazy) != 1 || lazy[tools.NativeWeb] != toolextension.Lazy {
		t.Fatalf("lazy-web must change only web exposure: %v, %v", lazy, err)
	}
	for _, invalid := range []string{"", "eager", "LAZY-WEB"} {
		if _, err := nativeToolExposure(invalid); err == nil {
			t.Errorf("accepted invalid profile %q", invalid)
		}
	}
}
