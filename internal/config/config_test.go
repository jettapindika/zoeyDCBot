package config

import (
	"testing"
)

func setenv(t *testing.T, k, v string) {
	t.Helper()
	t.Setenv(k, v)
}

func TestLoad_OK(t *testing.T) {
	setenv(t, "DISCORD_BOT_TOKEN", "tok")
	setenv(t, "LLM_BASE_URL", "https://api.openai.com/v1/")
	setenv(t, "LLM_API_KEY", "key")
	setenv(t, "LLM_MODEL", "gpt-4o-mini")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LLMBaseURL != "https://api.openai.com/v1" {
		t.Fatalf("base URL not trimmed: %q", cfg.LLMBaseURL)
	}
	if !cfg.Allowed("any") {
		t.Fatal("empty allowlist must allow everything")
	}
	if cfg.ContextTurns != 20 {
		t.Fatalf("ContextTurns = %d, want default 20", cfg.ContextTurns)
	}
	if cfg.QueueSize != 128 || cfg.LLMTimeoutSeconds != 120 || cfg.MusicEnabled {
		t.Fatalf("unexpected defaults: queue=%d timeout=%d music=%v", cfg.QueueSize, cfg.LLMTimeoutSeconds, cfg.MusicEnabled)
	}
}

func TestLoad_MissingSecrets(t *testing.T) {
	t.Setenv("DISCORD_BOT_TOKEN", "")
	t.Setenv("LLM_BASE_URL", "")
	t.Setenv("LLM_API_KEY", "")
	t.Setenv("LLM_MODEL", "")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error on missing secrets")
	}
	msg := err.Error()
	for _, want := range []string{"DISCORD_BOT_TOKEN", "LLM_BASE_URL", "LLM_API_KEY", "LLM_MODEL"} {
		if !contains(msg, want) {
			t.Fatalf("error missing %q: %s", want, msg)
		}
	}
}

func TestAllowedFilter(t *testing.T) {
	setenv(t, "DISCORD_BOT_TOKEN", "tok")
	setenv(t, "LLM_BASE_URL", "u")
	setenv(t, "LLM_API_KEY", "k")
	setenv(t, "LLM_MODEL", "m")
	setenv(t, "ALLOWED_CHANNEL_IDS", "111, 222")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Allowed("111") || !cfg.Allowed("222") {
		t.Fatal("allowed channel rejected")
	}
	if cfg.Allowed("333") {
		t.Fatal("non-allowed channel accepted")
	}
}

func TestLogLevel(t *testing.T) {
	setenv(t, "DISCORD_BOT_TOKEN", "tok")
	setenv(t, "LLM_BASE_URL", "u")
	setenv(t, "LLM_API_KEY", "k")
	setenv(t, "LLM_MODEL", "m")

	t.Run("default", func(t *testing.T) {
		setenv(t, "LOG_LEVEL", "")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.LogLevel != "info" {
			t.Fatalf("expected default log level 'info', got %q", cfg.LogLevel)
		}
	})

	t.Run("debug", func(t *testing.T) {
		setenv(t, "LOG_LEVEL", "debug")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.LogLevel != "debug" {
			t.Fatalf("expected 'debug', got %q", cfg.LogLevel)
		}
	})
}

func TestMusicEnabled(t *testing.T) {
	setenv(t, "DISCORD_BOT_TOKEN", "tok")
	setenv(t, "LLM_BASE_URL", "u")
	setenv(t, "LLM_API_KEY", "k")
	setenv(t, "LLM_MODEL", "m")

	t.Run("disabled by default", func(t *testing.T) {
		setenv(t, "MUSIC_ENABLED", "")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.MusicEnabled {
			t.Fatal("music should be disabled by default")
		}
	})

	t.Run("enabled with true", func(t *testing.T) {
		setenv(t, "MUSIC_ENABLED", "true")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !cfg.MusicEnabled {
			t.Fatal("music should be enabled when MUSIC_ENABLED=true")
		}
		if cfg.YtdlpPath != "yt-dlp" {
			t.Fatalf("expected default ytdlp path, got %q", cfg.YtdlpPath)
		}
		if cfg.FfmpegPath != "ffmpeg" {
			t.Fatalf("expected default ffmpeg path, got %q", cfg.FfmpegPath)
		}
	})
}

func TestEnvBool(t *testing.T) {
	cases := []struct {
		env    string
		result bool
	}{
		{"true", true},
		{"1", true},
		{"yes", true},
		{"on", true},
		{"TRUE", true},
		{"false", false},
		{"0", false},
		{"", false},
		{"random", false},
	}
	for _, tc := range cases {
		t.Setenv("TEST_BOOL", tc.env)
		got := envBool("TEST_BOOL", false)
		if got != tc.result {
			t.Errorf("envBool(%q) = %v, want %v", tc.env, got, tc.result)
		}
	}
}

func TestEnvInt(t *testing.T) {
	t.Setenv("TEST_INT", "42")
	if got := envInt("TEST_INT", 0); got != 42 {
		t.Errorf("envInt = %d, want 42", got)
	}
	t.Setenv("TEST_INT", "invalid")
	if got := envInt("TEST_INT", 99); got != 99 {
		t.Errorf("envInt with invalid = %d, want default 99", got)
	}
	t.Setenv("TEST_INT", "")
	if got := envInt("TEST_INT", 99); got != 99 {
		t.Errorf("envInt with empty = %d, want default 99", got)
	}
}

func TestSplitCSV(t *testing.T) {
	cases := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"a", 1},
		{"a,b,c", 3},
		{" a , b , c ", 3},
		{",,", 0},
		{"a,,b", 2},
	}
	for _, tc := range cases {
		got := splitCSV(tc.input)
		if len(got) != tc.want {
			t.Errorf("splitCSV(%q) = %v (len %d), want len %d", tc.input, got, len(got), tc.want)
		}
	}
}

func TestLLMBaseURLTrailingSlash(t *testing.T) {
	setenv(t, "DISCORD_BOT_TOKEN", "tok")
	setenv(t, "LLM_API_KEY", "k")
	setenv(t, "LLM_MODEL", "m")

	setenv(t, "LLM_BASE_URL", "https://api.openai.com/v1/")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LLMBaseURL != "https://api.openai.com/v1" {
		t.Fatalf("trailing slash not trimmed: %q", cfg.LLMBaseURL)
	}

	setenv(t, "LLM_BASE_URL", "https://api.openai.com/v1")
	cfg2, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg2.LLMBaseURL != "https://api.openai.com/v1" {
		t.Fatalf("URL without slash changed: %q", cfg2.LLMBaseURL)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
