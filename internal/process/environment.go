// Package process runs bounded native processes and retains their measured output.
// Callers own authorization and working-directory selection. This package also
// provides process-tree lifecycle helpers for background commands. GEKTOR's
// same-executable JSON workers use the separate subprocess package.
package process

import "strings"

// ScrubEnv copies env without variables whose names contain credential markers.
// Filtering is case-insensitive and heuristic; it does not inspect values.
func ScrubEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k := kv
		if i := strings.Index(kv, "="); i >= 0 {
			k = kv[:i]
		}
		upper := strings.ToUpper(k)
		if strings.Contains(upper, "API_KEY") || strings.Contains(upper, "APIKEY") ||
			strings.Contains(upper, "TOKEN") || strings.Contains(upper, "SECRET") ||
			strings.Contains(upper, "PASSWORD") || strings.Contains(upper, "CREDENTIAL") || strings.Contains(upper, "COOKIE") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
