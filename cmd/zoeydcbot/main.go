// Command zoeydcbot is the entry point for the Discord AI bot.
package main

import (
	"log/slog"
	"os"

	"github.com/joho/godotenv"

	"github.com/jettapindika/zoeyDCBot/internal/bot"
	"github.com/jettapindika/zoeyDCBot/internal/config"
	"github.com/jettapindika/zoeyDCBot/internal/logging"
)

func main() {
	// .env is optional in production (systemd can inject env vars), but
	// convenient for local runs. Missing file is fine; missing vars are not.
	_ = godotenv.Load()

	// Load config first so we can use the configured log level.
	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuration error", "err", err)
		os.Exit(1)
	}

	// Initialize structured logging with the configured level.
	logging.Init(cfg.LogLevel)
	slog.Info("zoeydcbot starting", "log_level", cfg.LogLevel, "music_enabled", cfg.MusicEnabled)

	b, err := bot.New(cfg)
	if err != nil {
		slog.Error("bot init", "err", err)
		os.Exit(1)
	}
	if err := b.Run(); err != nil {
		slog.Error("bot run", "err", err)
		os.Exit(1)
	}
}
