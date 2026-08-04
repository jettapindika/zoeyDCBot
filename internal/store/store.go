// Package store provides SQLite-backed persistence for conversation history
// and music queues. It uses modernc.org/sqlite (pure Go, no CGO).
package store

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jettapindika/zoeyDCBot/internal/logging"
)

var storeLog = logging.Component("store")

// Store wraps a SQLite database for persistence.
type Store struct {
	db *sql.DB
	mu sync.Mutex
}

// Open opens (or creates) a SQLite database at the given path.
func Open(path string) (*Store, error) {
	if path == "" {
		path = "zoeydcbot.db"
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// SQLite performance pragmas.
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("pragma %q: %w", pragma, err)
		}
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	storeLog.Info("sqlite store opened", "path", path)
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS conversations (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	channel_id  TEXT NOT NULL,
	role        TEXT NOT NULL,  -- 'user' or 'assistant'
	content     TEXT NOT NULL,
	created_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_conv_channel ON conversations(channel_id, created_at DESC);

CREATE TABLE IF NOT EXISTS music_queue (
	guild_id    TEXT NOT NULL,
	position    INTEGER NOT NULL,
	title       TEXT NOT NULL,
	url         TEXT NOT NULL,
	author      TEXT NOT NULL,
	duration    TEXT NOT NULL,
	thumbnail   TEXT NOT NULL,
	source      TEXT NOT NULL,
	added_by    TEXT NOT NULL,
	added_at    INTEGER NOT NULL,
	PRIMARY KEY (guild_id, position)
);

CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
`)
	return err
}

// --- Conversation history ---

// Message is a single conversation turn.
type Message struct {
	Role      string
	Content   string
	CreatedAt time.Time
}

// SaveMessage persists a conversation turn.
func (s *Store) SaveMessage(channelID, role, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		"INSERT INTO conversations (channel_id, role, content, created_at) VALUES (?, ?, ?, ?)",
		channelID, role, content, time.Now().UnixNano(),
	)
	return err
}

// LoadHistory loads the last N conversation turns for a channel.
func (s *Store) LoadHistory(channelID string, limit int) ([]Message, error) {
	rows, err := s.db.Query(
		"SELECT role, content, created_at FROM conversations WHERE channel_id = ? ORDER BY created_at DESC LIMIT ?",
		channelID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		var ts int64
		if err := rows.Scan(&m.Role, &m.Content, &ts); err != nil {
			return nil, err
		}
		m.CreatedAt = time.Unix(0, ts)
		msgs = append(msgs, m)
	}

	// Reverse to chronological order.
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, rows.Err()
}

// ClearHistory deletes all conversation history for a channel.
func (s *Store) ClearHistory(channelID string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec("DELETE FROM conversations WHERE channel_id = ?", channelID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// --- Music queue ---

// QueuedTrack is a music queue entry persisted to SQLite.
type QueuedTrack struct {
	Position int
	Title    string
	URL      string
	Author   string
	Duration string
	Thumbnail string
	Source   string
	AddedBy  string
}

// SaveQueue replaces the persisted queue for a guild.
func (s *Store) SaveQueue(guildID string, tracks []QueuedTrack) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM music_queue WHERE guild_id = ?", guildID); err != nil {
		tx.Rollback()
		return err
	}
	for i, t := range tracks {
		if _, err := tx.Exec(
			`INSERT INTO music_queue (guild_id, position, title, url, author, duration, thumbnail, source, added_by, added_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			guildID, i, t.Title, t.URL, t.Author, t.Duration, t.Thumbnail, t.Source, t.AddedBy, time.Now().UnixNano(),
		); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// LoadQueue loads the persisted queue for a guild.
func (s *Store) LoadQueue(guildID string) ([]QueuedTrack, error) {
	rows, err := s.db.Query(
		"SELECT position, title, url, author, duration, thumbnail, source, added_by FROM music_queue WHERE guild_id = ? ORDER BY position",
		guildID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []QueuedTrack
	for rows.Next() {
		var t QueuedTrack
		if err := rows.Scan(&t.Position, &t.Title, &t.URL, &t.Author, &t.Duration, &t.Thumbnail, &t.Source, &t.AddedBy); err != nil {
			return nil, err
		}
		tracks = append(tracks, t)
	}
	return tracks, rows.Err()
}

// ClearQueue deletes the persisted queue for a guild.
func (s *Store) ClearQueue(guildID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM music_queue WHERE guild_id = ?", guildID)
	return err
}

// --- Settings ---

// GetSetting returns a setting value by key.
func (s *Store) GetSetting(key string) (string, error) {
	var val string
	err := s.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&val)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return val, err
}

// SetSetting stores a setting value by key.
func (s *Store) SetSetting(key, val string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		"INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		key, val,
	)
	return err
}
