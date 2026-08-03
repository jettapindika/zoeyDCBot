// Package memory holds per-channel rolling conversation state. It is purely
// in-memory with a TTL so stale channels are forgotten and memory never grows
// unbounded.
package memory

import (
	"sync"
	"time"
)

// Turn is one message in the rolling history: "user" or "assistant".
type Turn struct {
	Role    string    `json:"role"`
	Content string    `json:"content"`
	At      time.Time `json:"at"`
}

// Channel keeps the recent turns for a single channel plus the last time the
// bot touched it (for TTL eviction).
type Channel struct {
	turns  []Turn
	lastAt time.Time
}

// Store is a concurrency-safe per-channel history keyed by channel ID.
type Store struct {
	mu       sync.Mutex
	channels map[string]*Channel
	ttl      time.Duration
	maxTurns int
}

// NewStore builds a history store keeping at most maxTurns turns per channel,
// evicting channels idle for longer than ttl.
func NewStore(maxTurns int, ttl time.Duration) *Store {
	return &Store{
		channels: make(map[string]*Channel),
		ttl:      ttl,
		maxTurns: maxTurns,
	}
}

// Append records a turn and returns the trimmed history (oldest first).
func (s *Store) Append(channelID, role, content string) []Turn {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.evictLocked()
	ch := s.channels[channelID]
	if ch == nil {
		ch = &Channel{}
		s.channels[channelID] = ch
	}
	ch.turns = append(ch.turns, Turn{Role: role, Content: content, At: time.Now()})
	if len(ch.turns) > s.maxTurns {
		// Trim to maxTurns, rounding down to an even count so a
		// user/assistant pair is never split at the front.
		drop := len(ch.turns) - s.maxTurns
		if (len(ch.turns)-drop)%2 != 0 {
			drop++
		}
		ch.turns = ch.turns[drop:]
	}
	ch.lastAt = time.Now()

	out := make([]Turn, len(ch.turns))
	copy(out, ch.turns)
	return out
}

// History returns the current turns for a channel without modifying it.
func (s *Store) History(channelID string) []Turn {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := s.channels[channelID]
	if ch == nil {
		return nil
	}
	ch.lastAt = time.Now()
	out := make([]Turn, len(ch.turns))
	copy(out, ch.turns)
	return out
}

// Clear forgets all history for a channel. Returns how many turns were dropped.
func (s *Store) Clear(channelID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := s.channels[channelID]
	if ch == nil {
		return 0
	}
	n := len(ch.turns)
	delete(s.channels, channelID)
	return n
}

// evictLocked removes channels that have been idle past the TTL. Caller must
// hold the mutex.
func (s *Store) evictLocked() {
	if s.ttl <= 0 {
		return
	}
	cutoff := time.Now().Add(-s.ttl)
	for id, ch := range s.channels {
		if ch.lastAt.Before(cutoff) {
			delete(s.channels, id)
		}
	}
}
