// Package automod provides per-guild message moderation: spam detection,
// link filtering, and banned-word filtering. Rules are configured via env
// and enforced in onMessageCreate before the bot processes the message.
package automod

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Action is the moderation action taken when a rule is violated.
type Action int

const (
	ActionNone Action = iota // no action (rule disabled)
	ActionWarn               // warn the user (delete message + send warning)
	ActionMute               // timeout the user
)

func (a Action) String() string {
	switch a {
	case ActionWarn:
		return "warn"
	case ActionMute:
		return "mute"
	default:
		return "none"
	}
}

// Rules holds the AutoMod configuration. All fields are set from env at
// startup and are read-only after initialization.
type Rules struct {
	SpamEnabled       bool
	SpamMaxMessages   int
	SpamWindowSeconds int
	SpamAction        Action
	SpamMuteMinutes   int

	LinkEnabled        bool
	LinkExemptRoles    []string
	LinkAllowedDomains []string
	LinkAction         Action

	WordFilterEnabled bool
	BannedWords       []string
	WordAction        Action

	ExemptRoles []string
}

// Violation describes a rule violation detected for a message.
type Violation struct {
	Rule   string // "spam", "link", or "word"
	Action Action
	Detail string // human-readable detail
}

// Engine enforces AutoMod rules per-guild.
type Engine struct {
	rules  Rules
	mu     sync.Mutex
	sentAt map[string][]time.Time
}

// New creates an AutoMod engine from the given rules.
func New(rules Rules) *Engine {
	return &Engine{
		rules:  rules,
		sentAt: make(map[string][]time.Time),
	}
}

// urlPattern matches http(s) URLs and bare domains like example.com.
var urlPattern = regexp.MustCompile(`(?i)\b(?:https?://|www\.)[^\s]+|[a-z0-9][-a-z0-9]*\.(?:com|net|org|io|gg|dev|xyz|me|tv|co|us|uk|de|fr|nl|ru|cn|jp|kr|br|ca|au|in)\b`)

// Check evaluates a message against all enabled rules. Returns a Violation
// if a rule was violated, or nil if the message is clean.
func (e *Engine) Check(userID, content string, roles []string) *Violation {
	if e.isExempt(roles) {
		return nil
	}

	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	// 1. Word filter (cheapest)
	if v := e.checkWords(content); v != nil {
		return v
	}

	// 2. Link filter
	if v := e.checkLinks(content, roles); v != nil {
		return v
	}

	// 3. Spam filter (record + check)
	if v := e.checkSpam(userID); v != nil {
		return v
	}

	return nil
}

func (e *Engine) isExempt(roles []string) bool {
	exempt := make(map[string]bool, len(e.rules.ExemptRoles))
	for _, r := range e.rules.ExemptRoles {
		exempt[r] = true
	}
	for _, r := range roles {
		if exempt[r] {
			return true
		}
	}
	return false
}

func (e *Engine) checkWords(content string) *Violation {
	if !e.rules.WordFilterEnabled || len(e.rules.BannedWords) == 0 {
		return nil
	}
	lower := strings.ToLower(content)
	for _, word := range e.rules.BannedWords {
		word = strings.ToLower(strings.TrimSpace(word))
		if word == "" {
			continue
		}
		if containsWord(lower, word) {
			return &Violation{
				Rule:   "word",
				Action: e.rules.WordAction,
				Detail: "message contains banned word",
			}
		}
	}
	return nil
}

func (e *Engine) checkLinks(content string, roles []string) *Violation {
	if !e.rules.LinkEnabled {
		return nil
	}

	for _, r := range roles {
		for _, er := range e.rules.LinkExemptRoles {
			if r == er {
				return nil
			}
		}
	}

	matches := urlPattern.FindAllString(content, -1)
	if len(matches) == 0 {
		return nil
	}

	if len(e.rules.LinkAllowedDomains) > 0 {
		allowed := make(map[string]bool, len(e.rules.LinkAllowedDomains))
		for _, d := range e.rules.LinkAllowedDomains {
			allowed[strings.ToLower(d)] = true
		}
		allAllowed := true
		for _, m := range matches {
			domain := extractDomain(m)
			if !allowed[domain] {
				allAllowed = false
				break
			}
		}
		if allAllowed {
			return nil
		}
	}

	return &Violation{
		Rule:   "link",
		Action: e.rules.LinkAction,
		Detail: fmt.Sprintf("message contains %d link(s)", len(matches)),
	}
}

func (e *Engine) checkSpam(userID string) *Violation {
	if !e.rules.SpamEnabled {
		return nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-time.Duration(e.rules.SpamWindowSeconds) * time.Second)

	times := e.sentAt[userID]
	kept := times[:0]
	for _, t := range times {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}

	kept = append(kept, now)
	e.sentAt[userID] = kept

	if len(kept) >= e.rules.SpamMaxMessages {
		e.sentAt[userID] = nil
		return &Violation{
			Rule:   "spam",
			Action: e.rules.SpamAction,
			Detail: fmt.Sprintf("sent %d messages in %d seconds", len(kept), e.rules.SpamWindowSeconds),
		}
	}

	return nil
}

// containsWord checks if lower contains word as a whole word.
func containsWord(lower, word string) bool {
	idx := 0
	for {
		pos := strings.Index(lower[idx:], word)
		if pos == -1 {
			return false
		}
		absPos := idx + pos
		if absPos > 0 {
			left := lower[absPos-1]
			if isWordChar(left) {
				idx = absPos + 1
				continue
			}
		}
		rightPos := absPos + len(word)
		if rightPos < len(lower) {
			right := lower[rightPos]
			if isWordChar(right) {
				idx = absPos + 1
				continue
			}
		}
		return true
	}
}

func isWordChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_'
}

// extractDomain extracts the domain from a URL-like string.
func extractDomain(s string) string {
	s = strings.ToLower(s)
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "www.")
	if idx := strings.IndexAny(s, "/?#"); idx >= 0 {
		s = s[:idx]
	}
	return s
}

// ResetSpam clears spam tracking for a user.
func (e *Engine) ResetSpam(userID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.sentAt, userID)
}
