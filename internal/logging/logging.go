// Package logging provides a structured logging system for ZoeyDCBot.
// It wraps slog with component-based loggers, log levels, and a unified
// format that makes it easy to monitor and debug every subsystem.
//
// Usage:
//
//	logging.Init("info")  // call once at startup
//	log := logging.Component("music")
//	log.Info("playback started", "guild", guildID, "track", title)
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// Init configures the global slog logger with the given level.
// Valid levels: debug, info, warn, error. Default: info.
func Init(level string) {
	var lvl slog.Level
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		lvl = slog.LevelDebug
	case "info":
		lvl = slog.LevelInfo
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level:     lvl,
		AddSource: false,
	})
	slog.SetDefault(slog.New(handler))
}

// Component returns a logger pre-tagged with the given component name.
// Every log line will include component=<name> so you can filter by
// subsystem: journalctl -u zoeydcbot | grep component=music
func Component(name string) *slog.Logger {
	return slog.Default().With("component", name)
}

// LevelFromString parses a level string into slog.Level.
func LevelFromString(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
