// Package log provides the platform logging component built on the
// standard library log/slog. Two output formats are supported:
// "console" (colored single-line text, default; colors are enabled
// only when stdout is an interactive terminal) and "json" (machine
// readable, for production log collectors).
package log

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/zsying/loong"
)

// Config is the component's own config block, decoded by the component.
type Config struct {
	Level  string `yaml:"level"`  // debug | info | warn | error (default info)
	Format string `yaml:"format"` // console (default) | json
}

// Log sets the process-wide slog default logger.
type Log struct {
	loong.Base
}

func (l *Log) Build(ctx *loong.Scope) error {
	var cfg Config
	if err := ctx.Config.Decode(&cfg); err != nil {
		return err
	}
	level := slog.LevelInfo
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	case "", "info":
	default:
		return fmt.Errorf("loong/log: unknown level %q (want debug, info, warn or error)", cfg.Level)
	}

	var h slog.Handler
	switch cfg.Format {
	case "", "console":
		h = newConsoleHandler(os.Stdout, level, isTerminal(os.Stdout))
	case "json":
		h = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	default:
		return fmt.Errorf("loong/log: unknown format %q (want console or json)", cfg.Format)
	}
	slog.SetDefault(slog.New(h))
	return nil
}

func init() {
	loong.RegisterComponent("log", func() loong.Component { return &Log{} })
}
