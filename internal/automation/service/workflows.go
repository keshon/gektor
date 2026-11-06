package service

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/keshon/gektor/internal/automation"
	"os"
	"path/filepath"
)

// Save writes a recipe atomically to an operator-selected workspace path.
// The adapter authorizes the destination; replacement does not follow a final symlink.
func (s Service) Save(ctx context.Context, path string, source json.RawMessage) (out string, err error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s.WS == nil || s.Registry == nil {
		return "", fmt.Errorf("workflow service unavailable")
	}
	if err := automation.ValidateJSON(source, s.DefinitionSchema()); err != nil {
		return "", err
	}
	// Preserve JSON numbers rather than converting integer inputs through float64.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(source, &raw); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	if len(data) > 64<<10 {
		return "", fmt.Errorf("recipe exceeds 64 KiB")
	}
	path, err = s.WS.Relative(path)
	if err != nil {
		return "", err
	}
	parent := filepath.Dir(path)
	if err := s.WS.MkdirAll(parent, 0700); err != nil {
		return "", err
	}
	tmp := filepath.Join(parent, ".gektor-recipe-"+rand.Text()+".tmp")
	f, err := s.WS.CreateExclusive(tmp, 0600)
	if err != nil {
		return "", err
	}
	defer func() {
		if e := s.WS.Remove(tmp); e != nil && !os.IsNotExist(e) {
			err = errors.Join(err, e)
		}
	}()
	_, writeErr := f.Write(data)
	if err := errors.Join(writeErr, f.Close()); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := s.WS.Rename(tmp, path); err != nil {
		return "", err
	}
	result, err := json.Marshal(map[string]any{"recipe_path": filepath.ToSlash(path), "saved": true})
	return string(result), err
}
