//go:build !windows

package credentials

// Unix uses a private directory and owner-only files, matching provider credentials.
func protect(raw []byte) ([]byte, error)   { return raw, nil }
func unprotect(raw []byte) ([]byte, error) { return raw, nil }
