package engine

import (
	"database/sql"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// StoreFile is the name of the SQLite file, inside the data directory, that
// holds everything the Engine keeps.
const StoreFile = "pong.db"

func openStore(dir string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", filepath.Join(dir, StoreFile)+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
