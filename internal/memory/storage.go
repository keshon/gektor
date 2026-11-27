package memory

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

func Open(directory string) (*Store, error) {
	if !filepath.IsAbs(directory) {
		return nil, fmt.Errorf("memory directory must be absolute")
	}
	err := os.MkdirAll(directory, 0700)
	if err != nil {
		return nil, err
	}
	return openDatabase(directory)
}

// OpenExisting returns nil when no database exists and never creates a directory.
func OpenExisting(directory string) (*Store, error) {
	if !filepath.IsAbs(directory) {
		return nil, fmt.Errorf("memory directory must be absolute")
	}
	_, err := os.Stat(filepath.Join(directory, "memory.sqlite"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return openDatabase(directory)
}

func openDatabase(directory string) (*Store, error) {
	path := filepath.Join(directory, "memory.sqlite")
	uriPath := filepath.ToSlash(path)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	location := url.URL{Scheme: "file", Path: uriPath}
	options := url.Values{}
	options.Add("_pragma", "busy_timeout(5000)")
	options.Add("_pragma", "journal_mode(WAL)")
	options.Add("_pragma", "synchronous(FULL)")
	options.Add("_pragma", "foreign_keys(ON)")
	options.Set("_txlock", "immediate")
	location.RawQuery = options.Encode()
	db, err := sql.Open("sqlite", location.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS projects(id TEXT PRIMARY KEY,path TEXT NOT NULL UNIQUE);
CREATE TABLE IF NOT EXISTS entries(
id TEXT PRIMARY KEY,scope TEXT NOT NULL CHECK(scope IN ('user','project')),
project_id TEXT NOT NULL,text TEXT NOT NULL,source_session TEXT NOT NULL,source_message TEXT NOT NULL,
created TEXT NOT NULL,updated TEXT NOT NULL,revision INTEGER NOT NULL,deleted INTEGER NOT NULL DEFAULT 0,
CHECK((scope='user' AND project_id='') OR (scope='project' AND project_id<>'')));
CREATE UNIQUE INDEX IF NOT EXISTS active_memory ON entries(scope,project_id,text) WHERE deleted=0;`)
	if err != nil {
		db.Close()
		return nil, err
	}
	err = os.Chmod(path, 0600)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

const entrySelect = `SELECT e.id,e.scope,e.text,e.project_id,COALESCE(p.path,''),e.source_session,e.source_message,e.created,e.updated,e.revision FROM entries e LEFT JOIN projects p ON p.id=e.project_id`

type scanner interface{ Scan(...any) error }

func scanEntry(row scanner) (Entry, error) {
	var entry Entry
	var created, updated string
	err := row.Scan(&entry.ID, &entry.Scope, &entry.Text, &entry.ProjectID, &entry.ProjectPath, &entry.SourceSession, &entry.SourceMessage, &created, &updated, &entry.Revision)
	if err != nil {
		return Entry{}, err
	}
	entry.Created, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return Entry{}, err
	}
	entry.Updated, err = time.Parse(time.RFC3339Nano, updated)
	return entry, err
}
