// Package config loads and validates ZoeyDCBot configuration from the
// environment. It fails fast on missing secrets so the bot never starts
// half-configured.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds every knob the bot needs. All values come from the
// environment; see .env.example for the full list.
type Config struct {
	DiscordToken   string
	CommandGuildID string // optional: register slash commands to one guild for fast iteration

	LLMBaseURL        string
	LLMAPIKey         string
	LLMModel          string
	SystemPrompt      string
	LLMTimeoutSeconds int
	LLMMaxRetries     int

	AllowedChannel []string // empty = respond everywhere the bot can see
	ContextTurns   int      // rolling history per channel
	HistoryTTLMin  int      // minutes before inactive channel history is evicted
	MaxWorkers     int      // concurrent LLM generations
	QueueSize      int      // pending LLM requests
	EditInterval   int      // ms between streaming message edits

	AdminRoleIDs  []string // optional roles allowed to use admin commands
	ModLogChannel string   // optional channel for moderation audit messages

	MusicEnabled  bool   // enables music slash commands
	YtdlpPath     string // path to yt-dlp binary (default: yt-dlp)
	FfmpegPath    string // path to ffmpeg binary (default: ffmpeg)
	MusicMaxQueue int    // maximum queued tracks per guild

	LogLevel string // log level: debug, info, warn, error (default: info)
}

// Allowed returns true if the channel is in the allowlist, or if no
// allowlist is configured (empty = allow everywhere).
func (c *Config) Allowed(channelID string) bool {
	if len(c.AllowedChannel) == 0 {
		return true
	}
	for _, id := range c.AllowedChannel {
		if id == channelID {
			return true
		}
	}
	return false
}

// Load reads the environment and validates it. On any missing or invalid
// required value it returns an error listing everything wrong, so the caller
// can log one clear message and exit.
func Load() (*Config, error) {
	c := &Config{
		DiscordToken:      strings.TrimSpace(os.Getenv("DISCORD_BOT_TOKEN")),
		CommandGuildID:    strings.TrimSpace(os.Getenv("COMMAND_GUILD_ID")),
		LLMBaseURL:        strings.TrimRight(strings.TrimSpace(os.Getenv("LLM_BASE_URL")), "/"),
		LLMAPIKey:         strings.TrimSpace(os.Getenv("LLM_API_KEY")),
		LLMModel:          strings.TrimSpace(os.Getenv("LLM_MODEL")),
		SystemPrompt:      os.Getenv("SYSTEM_PROMPT"),
		LLMTimeoutSeconds: envInt("LLM_TIMEOUT_SECONDS", 120),
		LLMMaxRetries:     envInt("LLM_MAX_RETRIES", 1),
		AllowedChannel:    splitCSV(os.Getenv("ALLOWED_CHANNEL_IDS")),
		ContextTurns:      envInt("CONTEXT_TURNS", 20),
		HistoryTTLMin:     envInt("HISTORY_TTL_MINUTES", 30),
		MaxWorkers:        envInt("MAX_WORKERS", 4),
		QueueSize:         envInt("QUEUE_SIZE", 128),
		EditInterval:      envInt("EDIT_INTERVAL_MS", 1000),
		AdminRoleIDs:      splitCSV(os.Getenv("ADMIN_ROLE_IDS")),
		ModLogChannel:     strings.TrimSpace(os.Getenv("MOD_LOG_CHANNEL_ID")),
		MusicEnabled:      envBool("MUSIC_ENABLED", false),
		YtdlpPath:         strings.TrimSpace(os.Getenv("YTDLP_PATH")),
		FfmpegPath:        strings.TrimSpace(os.Getenv("FFMPEG_PATH")),
		MusicMaxQueue:     envInt("MUSIC_MAX_QUEUE", 50),
		LogLevel:          strings.TrimSpace(os.Getenv("LOG_LEVEL")),
	}

	if c.LogLevel == "" {
		c.LogLevel = "info"
	}
	if c.MusicMaxQueue < 1 {
		c.MusicMaxQueue = 50
	}
	if c.MaxWorkers < 1 {
		c.MaxWorkers = 4
	}
	if c.QueueSize < 1 {
		c.QueueSize = 128
	}
	if c.EditInterval < 500 {
		c.EditInterval = 1000
	}
	if c.ContextTurns < 1 {
		c.ContextTurns = 20
	}
	if c.HistoryTTLMin < 1 {
		c.HistoryTTLMin = 30
	}
	if c.LLMTimeoutSeconds < 10 {
		c.LLMTimeoutSeconds = 120
	}
	if c.LLMMaxRetries < 0 {
		c.LLMMaxRetries = 1
	}

	var errs []string
	if c.DiscordToken == "" {
		errs = append(errs, "DISCORD_BOT_TOKEN is required")
	}
	if c.LLMBaseURL == "" {
		errs = append(errs, "LLM_BASE_URL is required")
	}
	if c.LLMAPIKey == "" {
		errs = append(errs, "LLM_API_KEY is required")
	}
	if c.LLMModel == "" {
		errs = append(errs, "LLM_MODEL is required")
	}
	if c.MusicEnabled {
		if c.YtdlpPath == "" {
			c.YtdlpPath = "yt-dlp"
		}
		if c.FfmpegPath == "" {
			c.FfmpegPath = "ffmpeg"
		}
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("config: %s", strings.Join(errs, "; "))
	}
	return c, nil
}

func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envBool(key string, def bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if v == "" {
		return def
	}
	return v == "true" || v == "1" || v == "yes" || v == "on"
}

func splitCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
