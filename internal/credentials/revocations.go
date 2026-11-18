package credentials

import (
	"errors"
	"fmt"
	"os"
	"regexp"
)

var revocationNames = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Revocations owns permanent tombstones in a protocol-selected namespace.
// It stores names only; the protocol checks that a binding exists before revoking it.
type Revocations struct{ Directory string }

func (s Revocations) Revoked(name string) (bool, error) {
	if !revocationNames.MatchString(name) {
		return false, fmt.Errorf("invalid revocation name")
	}
	root, err := os.OpenRoot(s.Directory)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer root.Close()
	_, err = root.Stat(name + ".revoked")
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func (s Revocations) Revoke(name string) error {
	if !revocationNames.MatchString(name) {
		return fmt.Errorf("invalid revocation name")
	}
	err := os.MkdirAll(s.Directory, 0700)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(s.Directory)
	if err != nil {
		return err
	}
	defer root.Close()

	file, err := root.OpenFile(name+".revoked", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	err = file.Sync()
	closeErr := file.Close()
	return errors.Join(err, closeErr)
}
