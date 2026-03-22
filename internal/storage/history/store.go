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
	createHistoryTableQuery = `CREATE TABLE IF NOT EXISTS conversation_history (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        chat_id INTEGER NOT NULL,
        role TEXT NOT NULL,
        text TEXT NOT NULL,
        created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
    );`
	createHistoryIndexQuery = "" +
		`CREATE INDEX IF NOT EXISTS idx_conversation_history_chat_id ` +
		`ON conversation_history(chat_id, id);`
	createPersonaTableQuery = `CREATE TABLE IF NOT EXISTS user_personas (
        user_id INTEGER PRIMARY KEY,
        user_name TEXT,
        persona TEXT,
        updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
    );`
	insertHistoryQuery = `INSERT INTO conversation_history (chat_id, role, text) VALUES (?, ?, ?)`
	trimHistoryQuery   = `DELETE FROM conversation_history WHERE chat_id = ? AND id NOT IN (
        SELECT id FROM conversation_history WHERE chat_id = ? ORDER BY id DESC LIMIT ?
    )`
	recentHistoryQuery = `SELECT role, text FROM conversation_history WHERE chat_id = ? ORDER BY id DESC LIMIT ?`
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
	if err := initDB(db); err != nil {
		closeDB(db)

		return nil, err
	}

	return &Store{db: db, maxPerChat: maxPerChat}, nil
}

func initDB(db *sql.DB) error {
	steps := []struct {
		query string
		label string
	}{
		{query: `PRAGMA journal_mode=WAL;`, label: "set journal_mode"},
		{query: `PRAGMA synchronous=NORMAL;`, label: "set synchronous"},
		{query: createHistoryTableQuery, label: "create conversation_history"},
		{query: createHistoryIndexQuery, label: "create index"},
		{query: createPersonaTableQuery, label: "create user_personas"},
	}
	for _, step := range steps {
		if _, err := db.Exec(step.query); err != nil {
			return fmt.Errorf("%s: %w", step.label, err)
		}
	}
	if err := addUserNameColumn(db); err != nil {
		return err
	}

	return migratePersonaNotNull(db)
}

func addUserNameColumn(db *sql.DB) error {
	if _, err := db.Exec(`ALTER TABLE user_personas ADD COLUMN user_name TEXT`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
			return fmt.Errorf("alter user_personas add user_name: %w", err)
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
	var persona string
	err := s.db.QueryRowContext(ctx, personaQuery, userID).Scan(&persona)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}

		return "", fmt.Errorf("get persona: %w", err)
	}

	return persona, nil
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

func migratePersonaNotNull(db *sql.DB) error {
	hasPersona, personaNotNull, err := personaColumnState(db)
	if err != nil {
		return err
	}
	if !hasPersona {
		return nil
	}
	if !personaNotNull {
		return nil
	}

	return rebuildPersonaTable(db)
}

func personaColumnState(db *sql.DB) (bool, bool, error) {
	rows, err := db.Query(`PRAGMA table_info(user_personas);`)
	if err != nil {
		return false, false, fmt.Errorf("pragma table_info user_personas: %w", err)
	}
	defer closeRows(rows)

	hasPersona := false
	personaNotNull := false
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, false, fmt.Errorf("scan table_info: %w", err)
		}
		if name == "persona" {
			hasPersona = true
			personaNotNull = notnull == 1
		}
	}
	if err := rows.Err(); err != nil {
		return false, false, fmt.Errorf("table_info rows error: %w", err)
	}

	return hasPersona, personaNotNull, nil
}

func rebuildPersonaTable(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin persona migration: %w", err)
	}
	defer rollbackTx(tx)

	createNew := `CREATE TABLE user_personas_new (
        user_id INTEGER PRIMARY KEY,
        user_name TEXT,
        persona TEXT,
        updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
    );`
	if _, err := tx.Exec(createNew); err != nil {
		return fmt.Errorf("create user_personas_new: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO user_personas_new (user_id, user_name, persona, updated_at)
        SELECT user_id, user_name, persona, updated_at FROM user_personas`); err != nil {
		return fmt.Errorf("copy user_personas: %w", err)
	}
	if _, err := tx.Exec(`DROP TABLE user_personas`); err != nil {
		return fmt.Errorf("drop old user_personas: %w", err)
	}
	if _, err := tx.Exec(`ALTER TABLE user_personas_new RENAME TO user_personas`); err != nil {
		return fmt.Errorf("rename user_personas_new: %w", err)
	}

	return tx.Commit()
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
