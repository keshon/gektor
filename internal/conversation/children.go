package conversation

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
)

// ChildStatePath is stable across session reloads. Provider call IDs are opaque,
// never path segments; separate parent filenames cannot share child archives.
func ChildStatePath(parent, callID string) string {
	if parent == "" || callID == "" {
		return ""
	}
	key := sha256.Sum256([]byte(filepath.Base(parent) + "\x00" + callID))
	return filepath.Join(filepath.Dir(parent), "children", fmt.Sprintf("%x", key[:]), "state.json")
}
