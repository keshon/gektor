// Package memory owns local, explicitly saved user and project facts.
package memory

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/keshon/gektor/internal/userdirs"
)

type Scope string

const (
	User          Scope = "user"
	Project       Scope = "project"
	MaxTextRunes        = 2000
	DefaultBudget       = 6000
)

var ErrConflict = errors.New("memory changed; refresh before editing")
var ErrNotFound = errors.New("memory not found in this scope")

type Input struct {
	Scope         Scope
	Text          string
	SourceSession string
	SourceMessage string
}

type Entry struct {
	ID            string
	Scope         Scope
	Text          string
	ProjectID     string
	ProjectPath   string
	SourceSession string
	SourceMessage string
	Created       time.Time
	Updated       time.Time
	Revision      int64
}

// Store owns its database connection; callers close it after use.
type Store struct{ db *sql.DB }

func DefaultDir() (string, error) {
	dir := userdirs.Sub("memory")
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("memory storage requires an absolute user home")
	}
	return dir, nil
}

func (s *Store) Close() error { return s.db.Close() }

func canonicalWorkspace(workspace string) (string, error) {
	if workspace == "" {
		return "", nil
	}
	path, err := filepath.Abs(workspace)
	if err != nil {
		return "", err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve memory workspace: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("memory workspace must be a directory")
	}
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path, nil
}

func validate(in Input, path string) (Input, error) {
	in.Text = strings.TrimSpace(in.Text)
	if in.Scope != User && in.Scope != Project {
		return in, fmt.Errorf("choose user or project memory")
	}
	if in.Scope == Project && path == "" {
		return in, fmt.Errorf("project memory requires a workspace; choose user explicitly")
	}
	if in.Text == "" || !utf8.ValidString(in.Text) || utf8.RuneCountInString(in.Text) > MaxTextRunes {
		return in, fmt.Errorf("memory text must contain 1–%d Unicode characters", MaxTextRunes)
	}
	if strings.ContainsRune(in.Text, 0) {
		return in, fmt.Errorf("memory text cannot contain NUL")
	}
	return in, nil
}

func projectID(ctx context.Context, tx *sql.Tx, path string, scope Scope) (string, error) {
	if scope == User {
		return "", nil
	}
	id := rand.Text()
	_, err := tx.ExecContext(ctx, `INSERT INTO projects(id,path) VALUES(?,?) ON CONFLICT(path) DO NOTHING`, id, path)
	if err != nil {
		return "", err
	}
	err = tx.QueryRowContext(ctx, `SELECT id FROM projects WHERE path=?`, path).Scan(&id)
	return id, err
}

func (s *Store) Add(ctx context.Context, workspace string, in Input) (Entry, bool, error) {
	path, err := canonicalWorkspace(workspace)
	if err != nil {
		return Entry{}, false, err
	}
	in, err = validate(in, path)
	if err != nil {
		return Entry{}, false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Entry{}, false, err
	}
	defer tx.Rollback()
	project, err := projectID(ctx, tx, path, in.Scope)
	if err != nil {
		return Entry{}, false, err
	}
	existing, err := scanEntry(tx.QueryRowContext(ctx, entrySelect+` WHERE e.scope=? AND e.project_id=? AND e.text=? AND e.deleted=0`, in.Scope, project, in.Text))
	if err == nil {
		return existing, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Entry{}, false, err
	}
	now := time.Now().UTC()
	entry := Entry{ID: rand.Text(), Scope: in.Scope, Text: in.Text, ProjectID: project, SourceSession: in.SourceSession, SourceMessage: in.SourceMessage, Created: now, Updated: now, Revision: 1}
	if project != "" {
		entry.ProjectPath = path
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO entries(id,scope,project_id,text,source_session,source_message,created,updated,revision,deleted) VALUES(?,?,?,?,?,?,?,?,1,0)`, entry.ID, entry.Scope, project, entry.Text, entry.SourceSession, entry.SourceMessage, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return Entry{}, false, err
	}
	err = tx.Commit()
	return entry, err == nil, err
}

func (s *Store) List(ctx context.Context, workspace, query string) ([]Entry, error) {
	path, err := canonicalWorkspace(workspace)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, entrySelect+` WHERE e.deleted=0 AND (e.scope='user' OR p.path=?) ORDER BY CASE e.scope WHEN 'user' THEN 0 ELSE 1 END,e.created,e.id`, path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []Entry
	query = strings.ToLower(strings.TrimSpace(query))
	for rows.Next() {
		entry, scanErr := scanEntry(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		if query == "" || strings.Contains(strings.ToLower(entry.Text), query) {
			entries = append(entries, entry)
		}
	}
	return entries, rows.Err()
}

func (s *Store) Update(ctx context.Context, workspace, id string, revision int64, in Input) (Entry, error) {
	path, err := canonicalWorkspace(workspace)
	if err != nil {
		return Entry{}, err
	}
	in, err = validate(in, path)
	if err != nil {
		return Entry{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Entry{}, err
	}
	defer tx.Rollback()
	entry, err := visibleEntry(ctx, tx, path, id)
	if err != nil {
		return Entry{}, err
	}
	if entry.Revision != revision {
		return Entry{}, ErrConflict
	}
	project, err := projectID(ctx, tx, path, in.Scope)
	if err != nil {
		return Entry{}, err
	}
	var duplicate string
	err = tx.QueryRowContext(ctx, `SELECT id FROM entries WHERE scope=? AND project_id=? AND text=? AND deleted=0 AND id<>?`, in.Scope, project, in.Text, id).Scan(&duplicate)
	if err == nil {
		return Entry{}, fmt.Errorf("identical memory already exists: %s", duplicate)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Entry{}, err
	}
	entry.Scope, entry.Text, entry.ProjectID = in.Scope, in.Text, project
	entry.ProjectPath = ""
	if project != "" {
		entry.ProjectPath = path
	}
	entry.Updated, entry.Revision = time.Now().UTC(), revision+1
	_, err = tx.ExecContext(ctx, `UPDATE entries SET scope=?,project_id=?,text=?,updated=?,revision=? WHERE id=? AND revision=?`, entry.Scope, project, entry.Text, entry.Updated.Format(time.RFC3339Nano), entry.Revision, id, revision)
	if err != nil {
		return Entry{}, err
	}
	err = tx.Commit()
	return entry, err
}

func (s *Store) Delete(ctx context.Context, workspace, id string, revision int64) error {
	path, err := canonicalWorkspace(workspace)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	entry, err := visibleEntry(ctx, tx, path, id)
	if err != nil {
		return err
	}
	if entry.Revision != revision {
		return ErrConflict
	}
	_, err = tx.ExecContext(ctx, `UPDATE entries SET deleted=1,revision=revision+1,updated=? WHERE id=? AND revision=?`, time.Now().UTC().Format(time.RFC3339Nano), id, revision)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func visibleEntry(ctx context.Context, tx *sql.Tx, path, id string) (Entry, error) {
	entry, err := scanEntry(tx.QueryRowContext(ctx, entrySelect+` WHERE e.id=? AND e.deleted=0 AND (e.scope='user' OR p.path=?)`, id, path))
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, ErrNotFound
	}
	return entry, err
}

// Reconnect moves a missing project's registry path without changing its ID.
// Existing workspaces and already registered destinations cannot be merged.
func (s *Store) Reconnect(ctx context.Context, oldPath, newPath string) error {
	if oldPath == "" || newPath == "" {
		return fmt.Errorf("both old and new workspace paths are required")
	}
	previous, err := filepath.Abs(oldPath)
	if err != nil {
		return err
	}
	previous = filepath.Clean(previous)
	if runtime.GOOS == "windows" {
		previous = strings.ToLower(previous)
	}
	_, err = os.Stat(previous)
	if err == nil {
		return fmt.Errorf("old workspace still exists; project memories are not shared between workspaces")
	}
	if !os.IsNotExist(err) {
		return err
	}
	next, err := canonicalWorkspace(newPath)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(ctx, `SELECT id FROM projects WHERE path=?`, previous).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var other string
	err = tx.QueryRowContext(ctx, `SELECT id FROM projects WHERE path=?`, next).Scan(&other)
	if err == nil {
		return fmt.Errorf("destination already has project memory; reconnect cannot merge projects")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE projects SET path=? WHERE id=?`, next, id)
	if err != nil {
		return err
	}
	return tx.Commit()
}
