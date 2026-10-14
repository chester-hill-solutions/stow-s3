package benchmark_test

import (
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	sqlite3 "github.com/mattn/go-sqlite3"
)

type sqliteStore struct {
	mu      sync.RWMutex
	db      *sql.DB
	journal string
}

func openSQLite(root string, memory bool) (objectStore, error) {
	path := filepath.Join(root, "objects.db")
	journal := "wal"
	if memory {
		path = "file:" + filepath.Join(root, "memory") + "?mode=memory&cache=shared"
		journal = "memory"
	} else {
		path = "file:" + path + "?"
	}
	db, err := sql.Open("sqlite3", path+"&_busy_timeout=5000&_journal_mode="+journal+"&_synchronous=FULL")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	if _, err = db.Exec("CREATE TABLE objects (key TEXT PRIMARY KEY, body BLOB NOT NULL)"); err != nil {
		db.Close()
		return nil, err
	}
	return &sqliteStore{db: db, journal: journal}, nil
}

func validateStoreConfig(store objectStore) error {
	s, ok := store.(*sqliteStore)
	if !ok {
		return nil
	}
	var journal string
	var synchronous int
	err := s.db.QueryRow("PRAGMA journal_mode").Scan(&journal)
	if err == nil {
		err = s.db.QueryRow("PRAGMA synchronous").Scan(&synchronous)
	}
	if err != nil || journal != s.journal || synchronous != 2 {
		return fmt.Errorf("SQLite configuration: journal=%s synchronous=%d error=%v", journal, synchronous, err)
	}
	return nil
}

func (s *sqliteStore) put(key string, body []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("INSERT INTO objects (key, body) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET body=excluded.body", key, body)
	return err
}

func (s *sqliteStore) open(key string) (io.ReadCloser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var body []byte
	err := s.db.QueryRow("SELECT body FROM objects WHERE key = ?", key).Scan(&body)
	if err == sql.ErrNoRows {
		return nil, os.ErrNotExist
	}
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

func (s *sqliteStore) remove(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM objects WHERE key = ?", key)
	return err
}

func (s *sqliteStore) close() error { return s.db.Close() }

func sqliteEngineVersion() string {
	version, _, _ := sqlite3.Version()
	return version
}
