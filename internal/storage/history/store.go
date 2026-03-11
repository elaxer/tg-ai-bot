package history

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
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
		return nil, fmt.Errorf("history path is required")
	}
	if maxPerChat <= 0 {
		return nil, fmt.Errorf("maxPerChat must be > 0")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create history dir: %w", err)
	}

	dsn := path + "?_pragma=busy_timeout=5000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("set journal_mode: %w", err)
	}
	if _, err := db.Exec(`PRAGMA synchronous=NORMAL;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("set synchronous: %w", err)
	}
	createHistoryTable := `CREATE TABLE IF NOT EXISTS conversation_history (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        chat_id INTEGER NOT NULL,
        role TEXT NOT NULL,
        text TEXT NOT NULL,
        created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
    );`
	if _, err := db.Exec(createHistoryTable); err != nil {
		db.Close()
		return nil, fmt.Errorf("create conversation_history: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_conversation_history_chat_id ON conversation_history(chat_id, id);`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create index: %w", err)
	}

	createPersonaTable := `CREATE TABLE IF NOT EXISTS user_personas (
        user_id INTEGER PRIMARY KEY,
        user_name TEXT,
        persona TEXT,
        updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
    );`
	if _, err := db.Exec(createPersonaTable); err != nil {
		db.Close()
		return nil, fmt.Errorf("create user_personas: %w", err)
	}
	if _, err := db.Exec(`ALTER TABLE user_personas ADD COLUMN user_name TEXT`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
			db.Close()
			return nil, fmt.Errorf("alter user_personas add user_name: %w", err)
		}
	}
	if err := migratePersonaNotNull(db); err != nil {
		db.Close()
		return nil, err
	}

	return &Store{db: db, maxPerChat: maxPerChat}, nil
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
		return fmt.Errorf("history store is nil")
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
	if _, err := tx.ExecContext(ctx, `INSERT INTO conversation_history (chat_id, role, text) VALUES (?, ?, ?)`, chatID, role, text); err != nil {
		tx.Rollback()
		return fmt.Errorf("insert history: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM conversation_history WHERE chat_id = ? AND id NOT IN (
        SELECT id FROM conversation_history WHERE chat_id = ? ORDER BY id DESC LIMIT ?
    )`, chatID, chatID, s.maxPerChat); err != nil {
		tx.Rollback()
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
		return nil, fmt.Errorf("history store is nil")
	}
	if limit <= 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT role, text FROM conversation_history WHERE chat_id = ? ORDER BY id DESC LIMIT ?`, chatID, limit)
	if err != nil {
		return nil, fmt.Errorf("query history: %w", err)
	}
	defer rows.Close()

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
		return fmt.Errorf("history store is nil")
	}
	userName = strings.TrimSpace(userName)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.ExecContext(ctx, `INSERT INTO user_personas (user_id, user_name) VALUES (?, ?)
        ON CONFLICT(user_id) DO UPDATE SET user_name=COALESCE(NULLIF(excluded.user_name, ''), user_personas.user_name)`, userID, userName)
	if err != nil {
		return fmt.Errorf("ensure user persona: %w", err)
	}
	return nil
}

// SetPersona upserts a persona for a specific user.
func (s *Store) SetPersona(ctx context.Context, userID int64, userName, persona string) error {
	if s == nil {
		return fmt.Errorf("history store is nil")
	}
	persona = strings.TrimSpace(persona)
	if persona == "" {
		return s.ClearPersona(ctx, userID)
	}
	userName = strings.TrimSpace(userName)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO user_personas (user_id, user_name, persona) VALUES (?, ?, ?)
        ON CONFLICT(user_id) DO UPDATE SET persona=excluded.persona, user_name=COALESCE(NULLIF(excluded.user_name, ''), user_personas.user_name), updated_at=CURRENT_TIMESTAMP`,
		userID, userName, persona); err != nil {
		return fmt.Errorf("set persona: %w", err)
	}
	return nil
}

// Persona returns the stored persona for the user, if any.
func (s *Store) Persona(ctx context.Context, userID int64) (string, error) {
	if s == nil {
		return "", fmt.Errorf("history store is nil")
	}
	var persona string
	err := s.db.QueryRowContext(ctx, `SELECT persona FROM user_personas WHERE user_id = ?`, userID).Scan(&persona)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("get persona: %w", err)
	}
	return persona, nil
}

// ClearPersona removes any stored persona for the user.
func (s *Store) ClearPersona(ctx context.Context, userID int64) error {
	if s == nil {
		return fmt.Errorf("history store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.ExecContext(ctx, `UPDATE user_personas SET persona=NULL, updated_at=CURRENT_TIMESTAMP WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("clear persona: %w", err)
	}
	return nil
}

func migratePersonaNotNull(db *sql.DB) error {
	type columnInfo struct {
		name    string
		notNull bool
	}
	rows, err := db.Query(`PRAGMA table_info(user_personas);`)
	if err != nil {
		return fmt.Errorf("pragma table_info user_personas: %w", err)
	}
	defer rows.Close()

	hasPersona := false
	personaNotNull := false
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("scan table_info: %w", err)
		}
		if name == "persona" {
			hasPersona = true
			personaNotNull = notnull == 1
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("table_info rows error: %w", err)
	}
	if !hasPersona {
		return nil
	}
	if !personaNotNull {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin persona migration: %w", err)
	}
	defer tx.Rollback()

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
