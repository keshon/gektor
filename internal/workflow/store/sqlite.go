package store

import (
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

func openDatabase(path string) (*sql.DB, error) {
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	location := url.URL{Scheme: "file", Path: path}
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

	_, err = db.Exec(`
CREATE TABLE IF NOT EXISTS runs (
	id TEXT PRIMARY KEY,
	status TEXT NOT NULL,
	finished BLOB NOT NULL,
	record BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS invocations (
	run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
	position TEXT NOT NULL,
	parent TEXT NOT NULL,
	ordinal INTEGER NOT NULL,
	record BLOB NOT NULL,
	PRIMARY KEY (run_id, position)
);
CREATE TABLE IF NOT EXISTS attempts (
	run_id TEXT NOT NULL,
	position TEXT NOT NULL,
	number INTEGER NOT NULL,
	record BLOB NOT NULL,
	PRIMARY KEY (run_id, position, number),
	FOREIGN KEY (run_id, position) REFERENCES invocations(run_id, position) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS artifacts (
	run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
	id TEXT NOT NULL,
	status TEXT NOT NULL CHECK (status IN ('writing', 'published', 'deleting')),
	bytes INTEGER NOT NULL CHECK (bytes >= 0 AND bytes <= 16777216),
	record BLOB,
	PRIMARY KEY (run_id, id)
);
CREATE INDEX IF NOT EXISTS artifacts_pending ON artifacts(run_id) WHERE status != 'published';
CREATE TABLE IF NOT EXISTS jobs (id TEXT PRIMARY KEY, record BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS schedules (id TEXT PRIMARY KEY, record BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS control (id INTEGER PRIMARY KEY CHECK (id = 1), schedule_revision INTEGER NOT NULL);
INSERT OR IGNORE INTO control VALUES (1, 0);
`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("opening workflow database: %w", err)
	}
	return db, nil
}
