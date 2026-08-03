package automod

import (
	"testing"
)

func TestSpamDetection(t *testing.T) {
	e := New(Rules{
		SpamEnabled:       true,
		SpamMaxMessages:   3,
		SpamWindowSeconds: 10,
		SpamAction:        ActionWarn,
	})

	// First two messages: no violation
	for i := 0; i < 2; i++ {
		v := e.Check("user1", "hello world", nil)
		if v != nil {
			t.Fatalf("message %d should not trigger spam: %+v", i+1, v)
		}
	}
	// Third message: violation
	v := e.Check("user1", "hello again", nil)
	if v == nil {
		t.Fatal("third message should trigger spam violation")
	}
	if v.Rule != "spam" {
		t.Fatalf("expected rule=spam, got %s", v.Rule)
	}
}

func TestSpamDisabledByDefault(t *testing.T) {
	e := New(Rules{})
	v := e.Check("user1", "spam spam spam", nil)
	if v != nil {
		t.Fatal("spam should be disabled with empty rules")
	}
}

func TestLinkFilter(t *testing.T) {
	e := New(Rules{
		LinkEnabled: true,
		LinkAction:  ActionWarn,
	})

	tests := []struct {
		content string
		violat  bool
	}{
		{"hello world", false},
		{"check out https://example.com", true},
		{"visit www.google.com now", true},
		{"go to discord.gg/myserver", true},
		{"no links here just text", false},
	}

	for _, tt := range tests {
		v := e.Check("user1", tt.content, nil)
		if tt.violat && v == nil {
			t.Errorf("expected violation for %q", tt.content)
		}
		if !tt.violat && v != nil {
			t.Errorf("unexpected violation for %q: %+v", tt.content, v)
		}
	}
}

func TestLinkFilterAllowedDomains(t *testing.T) {
	e := New(Rules{
		LinkEnabled:        true,
		LinkAllowedDomains: []string{"github.com", "discord.com"},
		LinkAction:         ActionWarn,
	})

	// Allowed domain: no violation
	v := e.Check("user1", "see https://github.com/repo", nil)
	if v != nil {
		t.Fatalf("allowed domain should not trigger: %+v", v)
	}
	// Non-allowed domain: violation
	v = e.Check("user1", "see https://evil.com", nil)
	if v == nil {
		t.Fatal("non-allowed domain should trigger violation")
	}
}

func TestLinkFilterExemptRole(t *testing.T) {
	e := New(Rules{
		LinkEnabled:     true,
		LinkExemptRoles: []string{"123"},
		LinkAction:      ActionWarn,
	})

	v := e.Check("user1", "see https://example.com", []string{"123"})
	if v != nil {
		t.Fatal("exempt role should not trigger link filter")
	}
}

func TestWordFilter(t *testing.T) {
	e := New(Rules{
		WordFilterEnabled: true,
		BannedWords:       []string{"badword", "spam"},
		WordAction:        ActionWarn,
	})

	tests := []struct {
		content string
		violat  bool
	}{
		{"this is a badword here", true},
		{"BADWORD in caps", true},
		{"this is fine", false},
		{"badwords plural should not match", false}, // whole-word only
		{"say spam please", true},
		{"spammer should not match", false}, // whole-word only
	}

	for _, tt := range tests {
		v := e.Check("user1", tt.content, nil)
		if tt.violat && v == nil {
			t.Errorf("expected word violation for %q", tt.content)
		}
		if !tt.violat && v != nil {
			t.Errorf("unexpected violation for %q: %+v", tt.content, v)
		}
	}
}

func TestExemptRoleBypass(t *testing.T) {
	e := New(Rules{
		WordFilterEnabled: true,
		BannedWords:       []string{"bad"},
		WordAction:        ActionWarn,
		ExemptRoles:       []string{"admin"},
	})

	v := e.Check("user1", "say bad word", []string{"admin"})
	if v != nil {
		t.Fatal("exempt role should bypass all rules")
	}
}

func TestEmptyContent(t *testing.T) {
	e := New(Rules{
		WordFilterEnabled: true,
		BannedWords:       []string{"bad"},
		LinkEnabled:       true,
		SpamEnabled:       true,
		SpamMaxMessages:   1,
	})

	v := e.Check("user1", "", nil)
	if v != nil {
		t.Fatal("empty content should not trigger any rule")
	}
}

func TestSpamReset(t *testing.T) {
	e := New(Rules{
		SpamEnabled:       true,
		SpamMaxMessages:   2,
		SpamWindowSeconds: 60,
		SpamAction:        ActionMute,
	})

	e.Check("user1", "msg1", nil)
	v := e.Check("user1", "msg2", nil)
	if v == nil {
		t.Fatal("second message should trigger spam")
	}
	// After reset, counter is cleared
	e.ResetSpam("user1")
	v = e.Check("user1", "msg3", nil)
	if v != nil {
		t.Fatal("first message after reset should not trigger spam")
	}
}
