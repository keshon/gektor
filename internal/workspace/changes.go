package workspace

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

func IgnoredDirectory(name string) bool {
	switch name {
	case ".git", ".gektor", "node_modules", ".venv", "venv", "__pycache__", "target", "dist", "build":
		return true
	}
	return false
}

type fileStamp struct {
	Size     int64
	Modified int64
	Mode     fs.FileMode
	Digest   [32]byte
}

// ChangeObserver owns one workspace's change baseline and digest cache.
// Begin starts a run; Observe advances its baseline only after a successful scan.
// An observer is used sequentially and is not safe for overlapping runs.
type ChangeObserver struct {
	workspace *Workspace
	previous  map[string]fileStamp
	started   bool
}

// NewChangeObserver constructs an observer without scanning the workspace.
func (w *Workspace) NewChangeObserver() *ChangeObserver {
	return &ChangeObserver{workspace: w}
}

// Begin captures a fresh baseline. Failure leaves the observer inactive.
func (o *ChangeObserver) Begin(ctx context.Context) error {
	o.started = false
	o.previous = nil
	previous, err := o.workspace.inventory(ctx, nil)
	if err != nil {
		return err
	}
	o.previous = previous
	o.started = true
	return nil
}

// Observe reports changes since Begin or the last successful Observe.
// Final scans can complete after run cancellation, with a separate 30s timeout.
func (o *ChangeObserver) Observe(ctx context.Context) ([]string, error) {
	if !o.started {
		return nil, fmt.Errorf("change observation has not begun")
	}
	scanCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	next, err := o.workspace.inventory(scanCtx, o.previous)
	if err != nil {
		return nil, err
	}
	var changed []string
	for p, stamp := range next {
		if old, ok := o.previous[p]; !ok || old.Digest != stamp.Digest || old.Mode != stamp.Mode {
			changed = append(changed, p)
		}
	}
	for p := range o.previous {
		if _, ok := next[p]; !ok {
			changed = append(changed, p)
		}
	}
	o.previous = next
	sort.Strings(changed)
	return changed, nil
}

func (w *Workspace) inventory(ctx context.Context, previous map[string]fileStamp) (map[string]fileStamp, error) {
	files, err := w.FileSet(ctx)
	if err != nil {
		return nil, err
	}
	dirs := FileDirectories(files)
	result := map[string]fileStamp{}
	err = fs.WalkDir(w.FS(), ".", func(name string, d fs.DirEntry, err error) error {
		path := filepath.Join(w.root, filepath.FromSlash(name))
		if err := ctx.Err(); err != nil {
			return err
		}
		if err != nil {
			return err
		}
		if d.IsDir() {
			if dirs != nil {
				rel, _ := filepath.Rel(w.root, path)
				if !dirs[rel] {
					return filepath.SkipDir
				}
			}
			if path != w.root && (d.Name() == ".git" || d.Name() == ".gektor" || (files == nil && IgnoredDirectory(d.Name()) && d.Name() != "dist" && d.Name() != "build" && d.Name() != "target")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() && d.Type()&os.ModeSymlink == 0 {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(w.root, path)
		if err != nil {
			return err
		}
		if files != nil && !files[rel] {
			return nil
		}
		if len(result) >= 100000 {
			return fmt.Errorf("workspace inventory exceeds 100000 files")
		}
		rel = filepath.ToSlash(rel)
		stamp := fileStamp{Size: info.Size(), Modified: info.ModTime().UnixNano(), Mode: info.Mode()}
		if old, ok := previous[rel]; ok && old.Size == stamp.Size && old.Modified == stamp.Modified && old.Mode == stamp.Mode {
			result[rel] = old
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := w.files.Readlink(filepath.FromSlash(name))
			if err != nil {
				return err
			}
			stamp.Digest = sha256.Sum256([]byte(target))
			result[rel] = stamp
			return nil
		}
		f, err := w.Open(path)
		if err != nil {
			return err
		}
		h := sha256.New()
		_, err = io.Copy(h, contextReader{ctx, f})
		f.Close()
		if err != nil {
			return err
		}
		copy(stamp.Digest[:], h.Sum(nil))
		result[rel] = stamp
		return nil
	})
	return result, err
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
