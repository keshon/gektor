package tools

import "github.com/keshon/gektor/internal/httptransport"

func normalizePublicHTTPURL(raw string) (string, error) { return httptransport.Normalize(raw, false) }
func normalizeLoopbackURL(raw string) (string, error)   { return httptransport.Normalize(raw, true) }
