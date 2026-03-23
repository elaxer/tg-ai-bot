// Package history persists chat history and personas in SQLite.
package history

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

const (
	insertHistoryQuery = `INSERT INTO conversation_history (chat_id, role, text) VALUES (?, ?, ?)`
	trimHistoryQuery   = `DELETE FROM conversation_history WHERE chat_id = ? AND id NOT IN (
        SELECT id FROM conversation_history WHERE chat_id = ? ORDER BY id DESC LIMIT ?
    )`
	recentHistoryQuery = `SELECT role, text FROM conversation_history WHERE chat_id = ? ORDER BY id DESC LIMIT ?`
	clearHistoryQuery  = `DELETE FROM conversation_history WHERE chat_id = ?`
	ensureUserQuery    = `INSERT INTO user_personas (user_id, user_name) VALUES (?, ?)
        ON CONFLICT(user_id) DO UPDATE SET user_name=COALESCE(NULLIF(excluded.user_name, ''), user_personas.user_name)`
	setPersonaQuery = `INSERT INTO user_personas (user_id, user_name, persona) VALUES (?, ?, ?)
        ON CONFLICT(user_id) DO UPDATE SET persona=excluded.persona,
        user_name=COALESCE(NULLIF(excluded.user_name, ''), user_personas.user_name),
        updated_at=CURRENT_TIMESTAMP`
	personaQuery      = `SELECT persona FROM user_personas WHERE user_id = ?`
	clearPersonaQuery = `UPDATE user_personas SET persona=NULL, updated_at=CURRENT_TIMESTAMP WHERE user_id = ?`
)

var (
	errHistoryPathRequired = errors.New("history path is required")
	errInvalidMaxPerChat   = errors.New("maxPerChat must be > 0")
	errNilHistoryStore     = errors.New("history store is nil")
)

// Turn represents a single role/text pair in a conversation history.
type Turn struct {
	Role string
	Text string
}

// Store persists chat conversation history in SQLite.
type Store struct {
	db         *sql.DB
	maxPerChat int
	mu         sync.Mutex
}

// Open initializes (or creates) the SQLite database at the provided path.
func Open(path string, maxPerChat int) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errHistoryPathRequired
	}
	if maxPerChat <= 0 {
		return nil, errInvalidMaxPerChat
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create history dir: %w", err)
	}

	dsn := path + "?_pragma=busy_timeout=5000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := configureDB(db); err != nil {
		closeDB(db)

		return nil, err
	}

	return &Store{db: db, maxPerChat: maxPerChat}, nil
}

func configureDB(db *sql.DB) error {
	steps := []struct {
		query string
		label string
	}{
		{query: `PRAGMA journal_mode=WAL;`, label: "set journal_mode"},
		{query: `PRAGMA synchronous=NORMAL;`, label: "set synchronous"},
	}
	for _, step := range steps {
		if _, err := db.Exec(step.query); err != nil {
			return fmt.Errorf("%s: %w", step.label, err)
		}
	}

	return nil
}

func closeDB(db *sql.DB) {
	if db != nil {
		_ = db.Close()
	}
}

// Close shuts down the underlying database connection.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}

	return s.db.Close()
}

// Append inserts a new conversation turn for the chat and trims history to the configured limit.
func (s *Store) Append(ctx context.Context, chatID int64, role, text string) error {
	if s == nil {
		return errNilHistoryStore
	}
	role = strings.TrimSpace(role)
	text = strings.TrimSpace(text)
	if role == "" || text == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if _, err := tx.ExecContext(ctx, insertHistoryQuery, chatID, role, text); err != nil {
		rollbackTx(tx)

		return fmt.Errorf("insert history: %w", err)
	}
	if _, err := tx.ExecContext(ctx, trimHistoryQuery, chatID, chatID, s.maxPerChat); err != nil {
		rollbackTx(tx)

		return fmt.Errorf("trim history: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit history tx: %w", err)
	}

	return nil
}

// Recent returns the most recent limit turns for the chat in chronological order.
func (s *Store) Recent(ctx context.Context, chatID int64, limit int) ([]Turn, error) {
	if s == nil {
		return nil, errNilHistoryStore
	}
	if limit <= 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, recentHistoryQuery, chatID, limit)
	if err != nil {
		return nil, fmt.Errorf("query history: %w", err)
	}
	defer closeRows(rows)

	turns := make([]Turn, 0, limit)
	for rows.Next() {
		var t Turn
		if err := rows.Scan(&t.Role, &t.Text); err != nil {
			return nil, fmt.Errorf("scan history: %w", err)
		}
		turns = append(turns, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	// Reverse to chronological order.
	for i, j := 0, len(turns)-1; i < j; i, j = i+1, j-1 {
		turns[i], turns[j] = turns[j], turns[i]
	}

	return turns, nil
}

// ClearChat deletes all stored conversation history for the chat.
func (s *Store) ClearChat(ctx context.Context, chatID int64) error {
	if s == nil {
		return errNilHistoryStore
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.ExecContext(ctx, clearHistoryQuery, chatID); err != nil {
		return fmt.Errorf("clear chat history: %w", err)
	}

	return nil
}

// EnsureUser ensures the user exists in user_personas with a default NULL persona.
func (s *Store) EnsureUser(ctx context.Context, userID int64, userName string) error {
	if s == nil {
		return errNilHistoryStore
	}
	userName = strings.TrimSpace(userName)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.ExecContext(ctx, ensureUserQuery, userID, userName)
	if err != nil {
		return fmt.Errorf("ensure user persona: %w", err)
	}

	return nil
}

// SetPersona upserts a persona for a specific user.
func (s *Store) SetPersona(ctx context.Context, userID int64, userName, persona string) error {
	if s == nil {
		return errNilHistoryStore
	}
	persona = strings.TrimSpace(persona)
	if persona == "" {
		return s.ClearPersona(ctx, userID)
	}
	userName = strings.TrimSpace(userName)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.ExecContext(ctx, setPersonaQuery, userID, userName, persona); err != nil {
		return fmt.Errorf("set persona: %w", err)
	}

	return nil
}

// Persona returns the stored persona for the user, if any.
func (s *Store) Persona(ctx context.Context, userID int64) (string, error) {
	if s == nil {
		return "", errNilHistoryStore
	}
	var persona sql.NullString
	err := s.db.QueryRowContext(ctx, personaQuery, userID).Scan(&persona)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}

		return "", fmt.Errorf("get persona: %w", err)
	}

	return strings.TrimSpace(persona.String), nil
}

// ClearPersona removes any stored persona for the user.
func (s *Store) ClearPersona(ctx context.Context, userID int64) error {
	if s == nil {
		return errNilHistoryStore
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.ExecContext(ctx, clearPersonaQuery, userID); err != nil {
		return fmt.Errorf("clear persona: %w", err)
	}

	return nil
}

func rollbackTx(tx *sql.Tx) {
	if tx != nil {
		_ = tx.Rollback()
	}
}

func closeRows(rows *sql.Rows) {
	if rows != nil {
		_ = rows.Close()
	}
}
