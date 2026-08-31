package log

import (
	"context"
	"encoding"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ANSI escape sequences used by the console handler. Output is plain
// text when color is disabled, so no sequences are emitted on pipes
// or redirected files.
const (
	ansiReset  = "\x1b[0m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiCyan   = "\x1b[36m"
	ansiGray   = "\x1b[90m"
)

// consoleHandler renders slog records as single-line human-readable
// text, optionally colored with ANSI escapes:
//
//	20:30:00 INF web listening addr=:8080
//	20:30:01 ERR register err="username taken"
type consoleHandler struct {
	w      io.Writer
	mu     *sync.Mutex
	level  slog.Leveler
	color  bool
	groups []string    // WithGroup path
	fixed  []slog.Attr // attrs pinned via WithAttrs
}

// newConsoleHandler returns a handler writing to w. level may be nil
// (defaults to Info). color enables ANSI styling.
func newConsoleHandler(w io.Writer, level slog.Leveler, color bool) *consoleHandler {
	return &consoleHandler{w: w, mu: &sync.Mutex{}, level: level, color: color}
}

func (h *consoleHandler) Enabled(_ context.Context, l slog.Level) bool {
	min := slog.LevelInfo
	if h.level != nil {
		min = h.level.Level()
	}
	return l >= min
}

func (h *consoleHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	if h.color {
		b.WriteString(ansiGray)
	}
	b.WriteString(r.Time.Format("15:04:05"))
	if h.color {
		b.WriteString(ansiReset)
	}
	b.WriteByte(' ')

	label, color := levelStyle(r.Level)
	if h.color {
		b.WriteString(color)
	}
	b.WriteString(label)
	if h.color {
		b.WriteString(ansiReset)
	}
	b.WriteByte(' ')

	b.WriteString(r.Message)

	pref := strings.Join(h.groups, ".")
	for _, a := range h.fixed {
		h.appendAttr(&b, pref, a)
	}
	r.Attrs(func(a slog.Attr) bool {
		h.appendAttr(&b, pref, a)
		return true
	})
	b.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, b.String())
	return err
}

// appendAttr writes one "key=value" pair, descending into groups with
// dotted keys and skipping empty attrs (slog convention).
func (h *consoleHandler) appendAttr(b *strings.Builder, pref string, a slog.Attr) {
	a.Value = a.Value.Resolve()
	if a.Equal(slog.Attr{}) {
		return
	}
	key := a.Key
	if pref != "" {
		key = pref + "." + key
	}
	if a.Value.Kind() == slog.KindGroup {
		for _, ga := range a.Value.Group() {
			h.appendAttr(b, key, ga)
		}
		return
	}
	b.WriteByte(' ')
	if h.color {
		b.WriteString(ansiCyan)
	}
	b.WriteString(key)
	if h.color {
		b.WriteString(ansiReset)
	}
	b.WriteByte('=')
	h.writeValue(b, a.Value)
}

func (h *consoleHandler) writeValue(b *strings.Builder, v slog.Value) {
	switch v.Kind() {
	case slog.KindString:
		h.writeStr(b, v.String())
	case slog.KindInt64:
		b.WriteString(strconv.FormatInt(v.Int64(), 10))
	case slog.KindUint64:
		b.WriteString(strconv.FormatUint(v.Uint64(), 10))
	case slog.KindFloat64:
		b.WriteString(strconv.FormatFloat(v.Float64(), 'g', -1, 64))
	case slog.KindBool:
		b.WriteString(strconv.FormatBool(v.Bool()))
	case slog.KindDuration:
		h.writeStr(b, v.Duration().String())
	case slog.KindTime:
		h.writeStr(b, v.Time().Format(time.RFC3339Nano))
	case slog.KindAny:
		any := v.Any()
		if tm, ok := any.(encoding.TextMarshaler); ok {
			data, err := tm.MarshalText()
			if err != nil {
				h.writeErr(b, err.Error())
				return
			}
			h.writeStr(b, string(data))
			return
		}
		if err, ok := any.(error); ok {
			h.writeErr(b, err.Error())
			return
		}
		h.writeStr(b, fmt.Sprintf("%+v", any))
	}
}

func (h *consoleHandler) writeStr(b *strings.Builder, s string) {
	if needsQuoting(s) {
		b.WriteString(strconv.Quote(s))
		return
	}
	b.WriteString(s)
}

// writeErr renders error values in red so failures stand out.
func (h *consoleHandler) writeErr(b *strings.Builder, s string) {
	if !h.color {
		h.writeStr(b, s)
		return
	}
	b.WriteString(ansiRed)
	if needsQuoting(s) {
		b.WriteString(strconv.Quote(s))
	} else {
		b.WriteString(s)
	}
	b.WriteString(ansiReset)
}

func needsQuoting(s string) bool {
	if s == "" {
		return true
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == '"' || c == '=' || c == ' ' || c == 0x7f {
			return true
		}
	}
	return false
}

func levelStyle(l slog.Level) (label, color string) {
	switch {
	case l >= slog.LevelError:
		return "ERR", ansiRed
	case l >= slog.LevelWarn:
		return "WRN", ansiYellow
	case l >= slog.LevelInfo:
		return "INF", ansiGreen
	default:
		return "DBG", ansiGray
	}
}

func (h *consoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	h2 := *h
	h2.fixed = append(append([]slog.Attr{}, h.fixed...), attrs...)
	return &h2
}

func (h *consoleHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	h2 := *h
	h2.groups = append(append([]string{}, h.groups...), name)
	return &h2
}

// isTerminal reports whether f is attached to a character device
// (i.e. an interactive console). Colors are disabled for pipes and
// redirected files.
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
