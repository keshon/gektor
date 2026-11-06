package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/keshon/gektor/internal/automation"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

var ErrConflict = errors.New("record revision conflict")

type localSchemas struct{}

func (localSchemas) Load(url string) (any, error) {
	return nil, fmt.Errorf("external schema references are disabled: %s", url)
}

func compileSchema(data []byte) (*jsonschema.Schema, []byte, error) {
	if len(data) > 64<<10 {
		return nil, nil, fmt.Errorf("schema exceeds 64 KiB")
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, nil, err
	}
	object, ok := doc.(map[string]any)
	if !ok || object["type"] != "object" {
		return nil, nil, fmt.Errorf("record schema must declare type object")
	}
	c := jsonschema.NewCompiler()
	c.UseLoader(localSchemas{})
	const url = "https://gektor.invalid/record.json"
	if err := c.AddResource(url, doc); err != nil {
		return nil, nil, err
	}
	schema, err := c.Compile(url)
	if err != nil {
		return nil, nil, err
	}
	canonical, err := json.Marshal(doc)
	if len(canonical) > 64<<10 {
		return nil, nil, fmt.Errorf("canonical schema exceeds 64 KiB")
	}
	return schema, canonical, err
}

func entryKey(key string) bool { return validKey(key) && key == strings.ToLower(key) }

func (s *Store) lockKind(ctx context.Context, kind string) (*os.File, error) {
	if !entryKey(kind) {
		return nil, fmt.Errorf("kind must use 1–64 lowercase letters, digits, underscores or hyphens; device names are reserved")
	}
	dir := filepath.Join("records", kind)
	if err := s.root.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		f, err := s.lock(filepath.Join(dir, "lock"))
		if !errors.Is(err, errLocked) {
			return f, err
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// RegisterSchema is an operator action. Existing kinds are immutable: use a
// new kind for a changed schema instead of invalidating persisted entries.
func (s *Store) RegisterSchema(ctx context.Context, kind string, data []byte) (string, error) {
	_, canonical, err := compileSchema(data)
	if err != nil {
		return "", err
	}
	lease, err := s.lockKind(ctx, kind)
	if err != nil {
		return "", err
	}
	defer lease.Close()
	path := filepath.Join("records", kind, "schema.json")
	var old json.RawMessage
	if err := s.read(path, 64<<10, &old); err == nil {
		_, previous, err := compileSchema(old)
		if err != nil {
			return "", err
		}
		if !bytes.Equal(canonical, previous) {
			return "", fmt.Errorf("schema for %s already exists; choose a new kind", kind)
		}
	} else if !os.IsNotExist(err) {
		return "", err
	} else if err := s.write(path, json.RawMessage(canonical)); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(canonical)), nil
}

func (s *Store) schema(kind string) (*jsonschema.Schema, string, error) {
	if !entryKey(kind) {
		return nil, "", fmt.Errorf("invalid record kind")
	}
	var raw json.RawMessage
	if err := s.read(filepath.Join("records", kind, "schema.json"), 64<<10, &raw); err != nil {
		return nil, "", fmt.Errorf("registered schema %s: %w", kind, err)
	}
	schema, canonical, err := compileSchema(raw)
	return schema, fmt.Sprintf("%x", sha256.Sum256(canonical)), err
}

func (s *Store) RecordSchema(kind string) (json.RawMessage, error) {
	if !entryKey(kind) {
		return nil, fmt.Errorf("invalid record kind")
	}
	var raw json.RawMessage
	if err := s.read(filepath.Join("records", kind, "schema.json"), 64<<10, &raw); err != nil {
		return nil, err
	}
	_, canonical, err := compileSchema(raw)
	return canonical, err
}

func validateData(schema *jsonschema.Schema, data json.RawMessage) error {
	if len(data) > 64<<10 {
		return fmt.Errorf("record data exceeds 64 KiB")
	}
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return err
	}
	if _, ok := v.(map[string]any); !ok {
		return fmt.Errorf("record data must be an object")
	}
	return schema.Validate(v)
}

func (s *Store) Entry(kind, id string) (automation.Entry, error) {
	if !entryKey(id) || id == "schema" {
		return automation.Entry{}, fmt.Errorf("invalid or reserved record ID")
	}
	schema, digest, err := s.schema(kind)
	if err != nil {
		return automation.Entry{}, err
	}
	var entry automation.Entry
	if err := s.read(filepath.Join("records", kind, id+".json"), 128<<10, &entry); err != nil {
		return automation.Entry{}, err
	}
	if entry.Kind != kind || entry.ID != id || entry.Revision == 0 || entry.SchemaSHA256 != digest {
		return automation.Entry{}, fmt.Errorf("record identity, revision or schema mismatch")
	}
	if err := validateData(schema, entry.Data); err != nil {
		return automation.Entry{}, err
	}
	return entry, nil
}

// PutEntry uses revision zero to create. Updates must supply the last read
// revision. The OS lock serializes the check and rename across processes.
func (s *Store) PutEntry(ctx context.Context, kind, id string, revision uint64, data json.RawMessage) (automation.Entry, error) {
	if !entryKey(id) || id == "schema" {
		return automation.Entry{}, fmt.Errorf("invalid or reserved record ID")
	}
	lease, err := s.lockKind(ctx, kind)
	if err != nil {
		return automation.Entry{}, err
	}
	defer lease.Close()
	schema, digest, err := s.schema(kind)
	if err != nil {
		return automation.Entry{}, err
	}
	if err := validateData(schema, data); err != nil {
		return automation.Entry{}, err
	}
	old, err := s.Entry(kind, id)
	if err != nil && !os.IsNotExist(err) {
		return automation.Entry{}, err
	}
	if old.Revision != revision || revision == ^uint64(0) {
		return automation.Entry{}, fmt.Errorf("%w: expected %d, current %d", ErrConflict, revision, old.Revision)
	}
	entry := automation.Entry{Kind: kind, ID: id, Revision: revision + 1, SchemaSHA256: digest, Data: append(json.RawMessage(nil), data...), Updated: time.Now().UTC()}
	if err := ctx.Err(); err != nil {
		return automation.Entry{}, err
	}
	if err := s.write(filepath.Join("records", kind, id+".json"), entry); err != nil {
		return automation.Entry{}, err
	}
	return entry, nil
}
