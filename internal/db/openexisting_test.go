package db

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOpenExistingMissing asserts a missing --db path yields ErrDBNotFound
// (wrapping the os.Stat result) naming the path, and that no database file
// is created as a side effect (PAR-311).
func TestOpenExistingMissing(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "nope.db")

	err := OpenExisting(dbPath)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDBNotFound)
	assert.ErrorIs(t, err, fs.ErrNotExist, "the os.Stat result must stay wrapped")
	assert.Contains(t, err.Error(), dbPath, "the message must name the path")

	_, statErr := os.Stat(dbPath)
	assert.ErrorIs(t, statErr, fs.ErrNotExist, "OpenExisting must not create the file")
	assert.Nil(t, dbConn, "a failed open must not publish a connection")
}

// TestOpenExistingEmptyPath asserts the empty-path guard matches InitDB.
func TestOpenExistingEmptyPath(t *testing.T) {
	err := OpenExisting("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database path cannot be empty")
}

// TestOpenExistingValid asserts an existing database written by InitDB opens
// with OpenExisting and its recorded rows stay readable.
func TestOpenExistingValid(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "valid.db")

	require.NoError(t, InitDB(dbPath))
	require.NoError(t, UpsertSession("sess-open-1", "spec.json", "T", "tx.json", "tx",
		"", "", "", "", "closed", false))
	require.NoError(t, Close())

	require.NoError(t, OpenExisting(dbPath))
	defer func() { _ = Close() }()

	rec, err := GetSessionByID("sess-open-1")
	require.NoError(t, err)
	assert.Equal(t, "sess-open-1", rec.SessionID)
}

// TestOpenExistingTableless asserts an existing but table-less database keeps
// the current InitDB-equivalent behavior: it opens and reports as empty
// instead of erroring (PAR-311 requirement 2).
func TestOpenExistingTableless(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "empty.db")

	f, err := os.Create(dbPath)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	require.NoError(t, OpenExisting(dbPath))
	defer func() { _ = Close() }()

	sessions, err := GetSessionsList()
	require.NoError(t, err)
	assert.Empty(t, sessions)
}

// TestOpenExistingGarbage asserts an existing file that is not a SQLite
// database fails with an open-class error naming the database problem — not
// ErrDBNotFound, since the file does exist (PAR-311).
func TestOpenExistingGarbage(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "garbage.db")

	require.NoError(t, os.WriteFile(dbPath, []byte("this is not a sqlite database"), 0o600))

	err := OpenExisting(dbPath)
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrDBNotFound), "the file exists; not-found is wrong")
	assert.Contains(t, err.Error(), "database")
}
