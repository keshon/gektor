package tools

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// The OS releases the lease on process exit, including crashes. Never delete
// the lock file: another process may already hold a handle to the same file.
func lockBrowserProfile(dir string) (func(), error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, ".gektor.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	var overlap windows.Overlapped
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlap); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}
