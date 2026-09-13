package log

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/zsying/loong"
)

// restoreDefaultLogger puts the process default logger back when the
// test ends. The default is a process-wide slot and these tests exist
// because something has to own it: a test that left it changed would
// leak into the rest of the package.
func restoreDefaultLogger(t *testing.T) {
	t.Helper()
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
}

// builtLog configures the component exactly as Build does.
func builtLog(t *testing.T, cfg Config) *Log {
	t.Helper()
	restoreDefaultLogger(t)
	l := &Log{}
	if err := l.configure(cfg); err != nil {
		t.Fatalf("configure(%+v): %v", cfg, err)
	}
	return l
}

// TestConfigureRejectsUnknownValues pins that the whole config block is
// validated before anything is installed. A typo in the tree has to
// fail the build rather than fall back to a default the yaml never
// asked for — and a rejected block must leave the process logger
// exactly as it found it.
func TestConfigureRejectsUnknownValues(t *testing.T) {
	restoreDefaultLogger(t)
	before := slog.Default()

	for _, cfg := range []Config{
		{Level: "loud"},
		{Format: "xml"},
		{Output: "kube"},
	} {
		l := &Log{}
		if err := l.configure(cfg); err == nil {
			t.Errorf("configure(%+v) accepted an unknown value", cfg)
		}
	}

	if slog.Default() != before {
		t.Error("a rejected config still replaced the process default logger")
	}
}

// TestOutputSelectsTheStream pins the new key: the tree decides where
// records go. The default keeps every tree written before the key
// existed on stdout.
func TestOutputSelectsTheStream(t *testing.T) {
	want := map[string]io.Writer{
		"":       os.Stdout,
		"stdout": os.Stdout,
		"stderr": os.Stderr,
	}
	for output, w := range want {
		t.Run("output="+output, func(t *testing.T) {
			l := builtLog(t, Config{Output: output})
			if l.writer != w {
				t.Errorf("writer = %v, want %v", l.writer, w)
			}
		})
	}
}

// TestRecordsFollowTheConfiguredStream is the behavioral half of the
// test above: a record really arrives on the configured stream rather
// than merely in a field. os.Stderr is swapped for a pipe first, because
// the component resolves its destination when it is configured.
func TestRecordsFollowTheConfiguredStream(t *testing.T) {
	restoreDefaultLogger(t)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	l := &Log{}
	cfgErr := l.configure(Config{Level: "info", Output: "stderr"})
	os.Stderr = old
	if cfgErr != nil {
		t.Fatalf("configure: %v", cfgErr)
	}

	slog.Info("routed to the configured stream")
	w.Close()
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "routed to the configured stream") {
		t.Errorf("record did not reach the configured stream: %q", got)
	}
	// A pipe is not a character device, so the record must be plain
	// text: escape sequences in a redirected stream corrupt it.
	if strings.Contains(string(got), "\x1b[") {
		t.Errorf("ANSI escapes reached a non-terminal destination: %q", got)
	}
}

// TestSetLevelAppliesAtRuntime pins that the level is a knob on the
// installed logger instead of a value baked into the handler when it
// was built: the record after SetLevel is filtered by the new level,
// with nothing rebuilt and no writer disturbed. Both formats read the
// shared LevelVar, so both are covered.
func TestSetLevelAppliesAtRuntime(t *testing.T) {
	for _, format := range []string{"console", "json"} {
		t.Run(format, func(t *testing.T) {
			l := builtLog(t, Config{Format: format, Level: "info"})

			var buf bytes.Buffer
			l.SetWriter(&buf)

			slog.Debug("hidden")
			if buf.Len() != 0 {
				t.Fatalf("debug record passed an info level: %q", buf.String())
			}

			l.SetLevel(slog.LevelDebug)
			if !slog.Default().Enabled(context.Background(), slog.LevelDebug) {
				t.Error("the process default logger still rejects debug after SetLevel")
			}
			slog.Debug("visible")
			if !strings.Contains(buf.String(), "visible") {
				t.Errorf("SetLevel had no effect on the next record: %q", buf.String())
			}
		})
	}
}

// TestSetWriterRedirectsTheProcessDefault pins the silencing knob: after
// a swap the process default logger writes to the new destination and
// the replaced one goes quiet. That is what lets a TUI own the screen
// without installing a default of its own.
func TestSetWriterRedirectsTheProcessDefault(t *testing.T) {
	l := builtLog(t, Config{Level: "info"})

	var first, second bytes.Buffer
	l.SetWriter(&first)
	slog.Info("first destination")
	l.SetWriter(&second)
	slog.Info("second destination")

	if !strings.Contains(first.String(), "first destination") {
		t.Errorf("the first destination missed its record: %q", first.String())
	}
	if strings.Contains(first.String(), "second destination") {
		t.Error("the replaced writer still received records")
	}
	if !strings.Contains(second.String(), "second destination") {
		t.Errorf("the second destination missed its record: %q", second.String())
	}
}

// TestLogRegistersAService pins the declaration an application resolves
// this component by. Without it a consumer's TryGet[*Log]() fails at
// runtime with "no node provides *log.Log" even though the node is
// mounted and the logger is installed — a wiring mistake that would
// otherwise only show up in the consumer.
func TestLogRegistersAService(t *testing.T) {
	for _, m := range loong.Components() {
		if m.Type != "loong.log" {
			continue
		}
		if !m.Service {
			t.Fatal("log declares no service, so no consumer can reach the mounted logger")
		}
		for _, s := range m.ServiceTypes {
			if s == "*log.Log" {
				return
			}
		}
		t.Errorf("service types = %v, want *log.Log", m.ServiceTypes)
		return
	}
	t.Fatal(`component type "loong.log" is not registered`)
}
