package main

import (
	"fmt"

	"github.com/keshon/gektor/internal/tools"
	toolextension "github.com/keshon/gektor/internal/tools/extension"
)

// nativeToolExposure changes initial schema exposure only. Lazy web tools remain
// discoverable through the ordinary capability catalog and permission checks.
func nativeToolExposure(profile string) (map[string]toolextension.Exposure, error) {
	switch profile {
	case "baseline":
		return nil, nil
	case "lazy-web":
		return map[string]toolextension.Exposure{tools.NativeWeb: toolextension.Lazy}, nil
	default:
		return nil, fmt.Errorf("unknown -tool-profile %q: want baseline or lazy-web", profile)
	}
}
