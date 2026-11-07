package pixelita

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/keshon/gektor/internal/workspace"
	imagekit "github.com/keshon/pixelita"
)

func (t Operation) authorizeOutputs(ctx context.Context, args json.RawMessage, in Arguments, rep *imagekit.Report) error {
	// Authorize actual planned filenames, not just their parent directory.
	for _, item := range rep.Items {
		destination := filepath.Join(in.OutDir, filepath.Base(item.Output))
		if err := t.AuthorizeOutput(ctx, destination, args); err != nil {
			return err
		}
	}
	return nil
}

func publish(ctx context.Context, ws *workspace.Workspace, source, destination string) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return fmt.Errorf("no variants were produced")
	}
	parent := filepath.Dir(destination)
	if err := ws.MkdirAll(parent, 0755); err != nil {
		return err
	}
	if err := ws.Mkdir(destination, 0755); err != nil {
		return fmt.Errorf("reserve new output directory: %w", err)
	}
	var created []string
	complete := false
	defer func() {
		if !complete {
			for _, path := range created {
				if e := ws.Remove(path); e != nil {
					err = errors.Join(err, fmt.Errorf("cleanup %s: %w", path, e))
				}
			}
			if e := ws.Remove(destination); e != nil {
				err = errors.Join(err, fmt.Errorf("cleanup directory %s: %w", destination, e))
			}
		}
	}()
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if e.IsDir() || strings.ContainsAny(e.Name(), "/\\") {
			return fmt.Errorf("unexpected output entry")
		}
		data, err := os.ReadFile(filepath.Join(source, e.Name()))
		if err != nil {
			return err
		}
		path := filepath.Join(destination, e.Name())
		f, err := ws.CreateExclusive(path, 0600)
		if err != nil {
			return err
		}
		created = append(created, path)
		_, writeErr := f.Write(data)
		closeErr := f.Close()
		if err := errors.Join(writeErr, closeErr); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	complete = true
	return nil
}
