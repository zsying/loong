package log

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestConsoleHandlerPlain(t *testing.T) {
	var buf bytes.Buffer
	h := newConsoleHandler(&buf, slog.LevelDebug, false)
	logger := slog.New(h)
	logger.Info("web listening", "addr", ":8080")
	logger.Warn("disk low", "free_mb", 512)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %q", len(lines), buf.String())
	}
	if !strings.Contains(lines[0], "INF web listening addr=:8080") {
		t.Errorf("unexpected line: %q", lines[0])
	}
	if strings.Contains(lines[0], "\x1b[") {
		t.Error("color disabled but ANSI escape present")
	}
	if !strings.Contains(lines[1], "WRN disk low free_mb=512") {
		t.Errorf("unexpected line: %q", lines[1])
	}
}

func TestConsoleHandlerColored(t *testing.T) {
	var buf bytes.Buffer
	h := newConsoleHandler(&buf, slog.LevelDebug, true)
	slog.New(h).Error("boom", "err", errors.New("connection refused"), "op", "register")

	out := buf.String()
	if !strings.Contains(out, "\x1b[31mERR\x1b[0m") {
		t.Errorf("expected red ERR level: %q", out)
	}
	if !strings.Contains(out, "\x1b[31m\"connection refused\"\x1b[0m") {
		t.Errorf("expected red quoted error value: %q", out)
	}
	// Plain string values must NOT be colored red.
	if !strings.Contains(out, "\x1b[36mop\x1b[0m=register") {
		t.Errorf("expected plain string value: %q", out)
	}
}

func TestConsoleHandlerGroups(t *testing.T) {
	var buf bytes.Buffer
	h := newConsoleHandler(&buf, slog.LevelDebug, false)
	slog.New(h.WithGroup("http")).Info("req", "path", "/api/greet", "status", 200)

	want := "INF req http.path=/api/greet http.status=200"
	if !strings.Contains(buf.String(), want) {
		t.Errorf("got %q, want substring %q", buf.String(), want)
	}
}

func TestConsoleHandlerLevelFilter(t *testing.T) {
	var buf bytes.Buffer
	h := newConsoleHandler(&buf, slog.LevelWarn, false)
	slog.New(h).Debug("hidden")
	if buf.Len() != 0 {
		t.Errorf("debug record should be filtered, got %q", buf.String())
	}
}
