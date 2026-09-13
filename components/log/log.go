// Package log provides the platform logging component built on the
// standard library log/slog. Two output formats are supported:
// "console" (colored single-line text, default; colors are enabled
// only when the destination is an interactive terminal) and "json"
// (machine readable, for production log collectors).
//
// The component installs the process-wide slog default logger, and it
// is the only thing that writes that slot. An application steers the
// logger it mounted — SetLevel, SetWriter — instead of installing a
// second default logger beside it, which would leave the config block
// below describing a logger nobody reads.
package log

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"

	"github.com/zsying/loong"
)

// Config is the component's own config block, decoded by the component.
type Config struct {
	Level  string `yaml:"level"`  // debug | info | warn | error (default info)
	Format string `yaml:"format"` // console (default) | json
	Output string `yaml:"output"` // stdout (default) | stderr
}

// Log installs the process-wide slog default logger and owns it. Where
// records go is the mounting tree's decision (the config block), and
// what the application decides at runtime — a verbose flag, a channel
// that needs a quiet stdout — is a knob on this value rather than a
// second handler installed over it.
type Log struct {
	loong.Base

	mu     sync.Mutex
	level  *slog.LevelVar // read by the handler on every record
	format string         // "console" | "json", resolved
	writer io.Writer
}

func (l *Log) Build(scope *loong.Scope) error {
	cfg, err := scope.Config[Config]()
	if err != nil {
		return err
	}
	return l.configure(cfg)
}

// configure validates the whole block before installing anything: a
// rejected config must leave the process logger exactly as it found it.
func (l *Log) configure(cfg Config) error {
	level, err := parseLevel(cfg.Level)
	if err != nil {
		return err
	}
	format := cfg.Format
	if format == "" {
		format = "console"
	}
	if format != "console" && format != "json" {
		return fmt.Errorf("loong/log: unknown format %q (want console or json)", cfg.Format)
	}
	w, err := outputWriter(cfg.Output)
	if err != nil {
		return err
	}

	l.level = new(slog.LevelVar)
	l.level.Set(level)
	l.format = format
	l.writer = w
	l.install()
	return nil
}

// install builds the handler from the current fields and makes it the
// process default. It runs once per configuration and again on every
// SetWriter, because the writer is bound into the handler and a new
// destination means a new handler. The level is deliberately not part
// of that: the handler reads the shared LevelVar, which is what makes
// SetLevel take effect without a rebuild.
func (l *Log) install() {
	var h slog.Handler
	if l.format == "json" {
		h = slog.NewJSONHandler(l.writer, &slog.HandlerOptions{Level: l.level})
	} else {
		h = newConsoleHandler(l.writer, l.level, colorFor(l.writer))
	}
	slog.SetDefault(slog.New(h))
}

// SetLevel changes the level of the process default logger. It applies
// to the next record — the handler reads the shared LevelVar on every
// call — so nothing is rebuilt and no destination is disturbed.
func (l *Log) SetLevel(level slog.Level) {
	if l.level == nil {
		return
	}
	l.level.Set(level)
}

// SetWriter redirects the process default logger to w, rebuilding the
// handler because the destination is bound into it. Color is recomputed
// for the new destination: only a character device gets ANSI escapes,
// so swapping in a buffer or io.Discard yields plain text.
func (l *Log) SetWriter(w io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.level == nil {
		return
	}
	l.writer = w
	l.install()
}

func parseLevel(name string) (slog.Level, error) {
	switch name {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("loong/log: unknown level %q (want debug, info, warn or error)", name)
}

// outputWriter resolves the output key. stdout is the default so every
// tree written before this key existed keeps its behavior.
func outputWriter(name string) (io.Writer, error) {
	switch name {
	case "", "stdout":
		return os.Stdout, nil
	case "stderr":
		return os.Stderr, nil
	}
	return nil, fmt.Errorf("loong/log: unknown output %q (want stdout or stderr)", name)
}

// colorFor enables ANSI styling for a character device only. An
// arbitrary io.Writer is some kind of pipe or buffer by definition, and
// escapes must never reach a file or another program.
func colorFor(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && isTerminal(f)
}

func init() {
	loong.RegisterComponent("loong.log", func() loong.Component { return &Log{} },
		loong.WithConfig[Config](),
		loong.WithService(func(c loong.Component) *Log { return c.(*Log) }),
		loong.WithDesc("process-wide logging via slog, console/json formats"),
	)
}
