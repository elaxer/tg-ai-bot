package history

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

const testSchemaSQL = `
CREATE TABLE conversation_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id INTEGER NOT NULL,
    role TEXT NOT NULL,
    text TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_conversation_history_chat_id
ON conversation_history(chat_id, id);
CREATE TABLE user_personas (
    user_id INTEGER PRIMARY KEY,
    user_name TEXT,
    persona TEXT,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);`

func TestOpenValidation(t *testing.T) {
	t.Parallel()

	_, err := Open("", 1)
	if !errors.Is(err, errHistoryPathRequired) {
		t.Fatalf("Open() error = %v, want %v", err, errHistoryPathRequired)
	}

	_, err = Open(filepath.Join(t.TempDir(), "history.db"), 0)
	if !errors.Is(err, errInvalidMaxPerChat) {
		t.Fatalf("Open() error = %v, want %v", err, errInvalidMaxPerChat)
	}
}

func TestStoreAppendRecentAndTrim(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, 2)
	defer func() {
		_ = store.Close()
	}()

	ctx := context.Background()
	if err := store.Append(ctx, 1, "user", "first"); err != nil {
		t.Fatalf("Append(first) error = %v", err)
	}
	if err := store.Append(ctx, 1, "assistant", "second"); err != nil {
		t.Fatalf("Append(second) error = %v", err)
	}
	if err := store.Append(ctx, 1, "user", "third"); err != nil {
		t.Fatalf("Append(third) error = %v", err)
	}

	turns, err := store.Recent(ctx, 1, 5)
	if err != nil {
		t.Fatalf("Recent() error = %v", err)
	}
	if len(turns) != 2 {
		t.Fatalf("Recent() len = %d, want 2", len(turns))
	}
	if turns[0].Text != "second" || turns[1].Text != "third" {
		t.Fatalf("Recent() = %#v, want chronological trimmed turns", turns)
	}
}

func TestStorePersonaLifecycle(t *testing.T) {
	t.Parallel()

	store := newTestStore(t, 5)
	defer func() {
		_ = store.Close()
	}()

	ctx := context.Background()
	if err := store.EnsureUser(ctx, 7, "alice"); err != nil {
		t.Fatalf("EnsureUser() error = %v", err)
	}
	if err := store.SetPersona(ctx, 7, "alice", "be sarcastic"); err != nil {
		t.Fatalf("SetPersona() error = %v", err)
	}

	persona, err := store.Persona(ctx, 7)
	if err != nil {
		t.Fatalf("Persona() error = %v", err)
	}
	if persona != "be sarcastic" {
		t.Fatalf("Persona() = %q, want stored persona", persona)
	}

	if err := store.ClearPersona(ctx, 7); err != nil {
		t.Fatalf("ClearPersona() error = %v", err)
	}

	persona, err = store.Persona(ctx, 7)
	if err != nil {
		t.Fatalf("Persona() after clear error = %v", err)
	}
	if persona != "" {
		t.Fatalf("Persona() after clear = %q, want empty", persona)
	}
}

func TestNilStoreMethods(t *testing.T) {
	t.Parallel()

	var store *Store
	ctx := context.Background()

	if err := store.Append(ctx, 1, "user", "hello"); !errors.Is(err, errNilHistoryStore) {
		t.Fatalf("Append() error = %v, want %v", err, errNilHistoryStore)
	}
	if _, err := store.Recent(ctx, 1, 1); !errors.Is(err, errNilHistoryStore) {
		t.Fatalf("Recent() error = %v, want %v", err, errNilHistoryStore)
	}
	if err := store.EnsureUser(ctx, 1, "alice"); !errors.Is(err, errNilHistoryStore) {
		t.Fatalf("EnsureUser() error = %v, want %v", err, errNilHistoryStore)
	}
	if err := store.SetPersona(ctx, 1, "alice", "persona"); !errors.Is(err, errNilHistoryStore) {
		t.Fatalf("SetPersona() error = %v, want %v", err, errNilHistoryStore)
	}
	if _, err := store.Persona(ctx, 1); !errors.Is(err, errNilHistoryStore) {
		t.Fatalf("Persona() error = %v, want %v", err, errNilHistoryStore)
	}
	if err := store.ClearPersona(ctx, 1); !errors.Is(err, errNilHistoryStore) {
		t.Fatalf("ClearPersona() error = %v, want %v", err, errNilHistoryStore)
	}
}

func newTestStore(t *testing.T, maxPerChat int) *Store {
	t.Helper()

	store, err := Open(filepath.Join(t.TempDir(), "history.db"), maxPerChat)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if _, err := store.db.Exec(testSchemaSQL); err != nil {
		t.Fatalf("Exec(testSchemaSQL) error = %v", err)
	}

	return store
}
