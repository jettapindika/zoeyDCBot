package logging

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestLevelFromString(t *testing.T) {
	cases := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"", slog.LevelInfo},
		{"invalid", slog.LevelInfo},
		{"trace", slog.LevelInfo}, // unknown falls back to info
	}
	for _, tc := range cases {
		got := LevelFromString(tc.input)
		if got != tc.want {
			t.Errorf("LevelFromString(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestInitSetsLevel(t *testing.T) {
	// Save and restore default logger
	orig := slog.Default()
	defer slog.SetDefault(orig)

	// Init with debug should set the level to debug — verify it doesn't panic
	Init("debug")
	Init("info")
	Init("warn")
	Init("error")
	Init("invalid") // should fall back to info
}

func TestComponentLoggerHasTag(t *testing.T) {
	// Use a test handler to capture output
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(handler))
	defer func() {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	}()

	log := Component("music")
	log.Info("test message", "key", "value")

	output := buf.String()
	if !strings.Contains(output, "component=music") {
		t.Errorf("expected component=music in output, got: %s", output)
	}
	if !strings.Contains(output, "test message") {
		t.Errorf("expected 'test message' in output, got: %s", output)
	}
	if !strings.Contains(output, "key=value") {
		t.Errorf("expected 'key=value' in output, got: %s", output)
	}
}

func TestComponentDifferentNames(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(handler))
	defer func() {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	}()

	for _, name := range []string{"ai", "music", "admin", "gateway", "config"} {
		buf.Reset()
		log := Component(name)
		log.Info("msg")
		if !strings.Contains(buf.String(), "component="+name) {
			t.Errorf("component=%s missing in output: %s", name, buf.String())
		}
	}
}
