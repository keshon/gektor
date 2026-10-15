// Package filetxn stages related file replacements before publishing them.
// Callers must serialize writers. Rollback covers ordinary errors, not process
// crashes or power loss between renames.
package filetxn

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Write prepares a complete file at the supplied temporary path. It must not
// modify Path itself. Existing serializers can be used without changing format.
type Write struct {
	Path string
	Save func(string) error
}

type staged struct {
	path, directory, prepared string
	backed, installed         bool
}

func Replace(writes []Write) error { return replace(writes, os.Rename) }

func replace(writes []Write, rename func(string, string) error) error {
	var files []staged
	defer func() {
		for _, f := range files {
			os.Remove(f.prepared)
			// A failed rollback retains the original for manual recovery.
			if !f.backed {
				os.Remove(filepath.Join(f.directory, "original"))
				os.Remove(f.directory)
			}
		}
	}()
	seen := make(map[string]bool)
	for _, w := range writes {
		path, err := filepath.Abs(w.Path)
		if err != nil {
			return err
		}
		if seen[path] {
			return fmt.Errorf("duplicate settings target: %s", path)
		}
		seen[path] = true
		info, err := os.Lstat(path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil && !info.Mode().IsRegular() {
			return fmt.Errorf("settings target is not a regular file: %s", path)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		dir, err := os.MkdirTemp(filepath.Dir(path), ".settings-*")
		if err != nil {
			return err
		}
		// Keep the prepared file beside its target: portable profile serializers
		// resolve asset paths relative to the output directory.
		prepared := dir + ".new"
		files = append(files, staged{path: path, directory: dir, prepared: prepared})
		if err := w.Save(prepared); err != nil {
			return err
		}
	}
	rollback := func(cause error) error {
		for i := len(files) - 1; i >= 0; i-- {
			f := &files[i]
			if f.installed {
				if err := os.Remove(f.path); err != nil {
					cause = errors.Join(cause, fmt.Errorf("remove settings replacement %s: %w", f.path, err))
					continue
				}
			}
			if f.backed {
				if err := rename(filepath.Join(f.directory, "original"), f.path); err != nil {
					cause = errors.Join(cause, fmt.Errorf("restore settings %s (backup in %s): %w", f.path, f.directory, err))
				} else {
					f.backed = false
				}
			}
		}
		return cause
	}
	for i := range files {
		f := &files[i]
		if err := rename(f.path, filepath.Join(f.directory, "original")); err == nil {
			f.backed = true
		} else if !os.IsNotExist(err) {
			return rollback(err)
		}
		if err := rename(f.prepared, f.path); err != nil {
			return rollback(err)
		}
		f.installed = true
	}
	for i := range files {
		files[i].backed = false
	}
	return nil
}
