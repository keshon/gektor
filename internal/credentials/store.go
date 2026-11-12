package credentials

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

// Store protects opaque values. Callers own protocol binding and the encoded format.
// Existing MCP files retain their names, format and platform protection.
type Store struct{ Directory string }

const maxStoredBytes = 1 << 20
const maxProtectedBytes = 2 << 20

var references = regexp.MustCompile(`^[a-f0-9]{32}$`)

func ValidReference(id string) bool { return references.MatchString(id) }

func (s Store) Put(value []byte) (string, error) {
	if len(value) == 0 || len(value) > maxStoredBytes {
		return "", fmt.Errorf("invalid credential size")
	}
	raw, err := protect(value)
	if err != nil {
		return "", fmt.Errorf("cannot protect credential")
	}
	err = os.MkdirAll(s.Directory, 0700)
	if err != nil {
		return "", err
	}
	var id [16]byte
	_, err = rand.Read(id[:])
	if err != nil {
		return "", err
	}
	name := hex.EncodeToString(id[:])
	path := filepath.Join(s.Directory, name)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}

	_, err = file.Write(raw)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path) // The incomplete file is never published as a reference.
		return "", err
	}
	return name, nil
}

func (s Store) Resolve(id string) ([]byte, error) {
	if !ValidReference(id) {
		return nil, fmt.Errorf("invalid credential reference")
	}
	file, err := os.Open(filepath.Join(s.Directory, id))
	if err != nil {
		return nil, fmt.Errorf("credential unavailable")
	}
	defer file.Close() // Reading is complete before the decoded value is returned.
	raw, err := io.ReadAll(io.LimitReader(file, maxProtectedBytes+1))
	if err != nil || len(raw) > maxProtectedBytes {
		return nil, fmt.Errorf("credential unavailable")
	}
	raw, err = unprotect(raw)
	if err != nil {
		return nil, fmt.Errorf("cannot unlock credential")
	}
	return raw, nil
}

func (s Store) Delete(id string) error {
	if !ValidReference(id) {
		return fmt.Errorf("invalid credential reference")
	}
	err := os.Remove(filepath.Join(s.Directory, id))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
