// Package log provides the platform logging component built on the
// standard library log/slog: JSON output, configurable level.
package log

import (
	"log/slog"
	"os"

	"github.com/zsying/loong"
)

// Config is the component's own config block, decoded by the component.
type Config struct {
	Level string `yaml:"level"`
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
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))
	return nil
}

func init() {
	loong.Register("log", func() loong.Component { return &Log{} })
}
