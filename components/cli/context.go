package cli

import (
	"io"
	"os"
	"strings"
)

// Context aggregates everything a command needs: the raw arguments,
// the output streams, and the process exit code. The cli component
// builds it once from os.Args and exposes it as a service, so
// subcommand components stay thin — they read Context instead of
// parsing os.Args themselves.
type Context struct {
	Args []string // full command-line arguments (os.Args[1:])
	Out  io.Writer
	Err  io.Writer

	exit int
}

// NewContext builds a command context. It is used by the cli
// component's Build and by tests.
func NewContext(args []string, out, errOut io.Writer) *Context {
	if out == nil {
		out = os.Stdout
	}
	if errOut == nil {
		errOut = os.Stderr
	}
	return &Context{Args: args, Out: out, Err: errOut}
}

// HasFlag reports whether "--name" is present in Args.
func (c *Context) HasFlag(name string) bool {
	flag := "--" + name
	for _, a := range c.Args {
		if a == flag {
			return true
		}
	}
	return false
}

// Flag returns the value of "--name=value", or ok=false when absent.
func (c *Context) Flag(name string) (string, bool) {
	prefix := "--" + name + "="
	for _, a := range c.Args {
		if strings.HasPrefix(a, prefix) {
			return strings.TrimPrefix(a, prefix), true
		}
	}
	return "", false
}

// Positional returns the arguments that are not flags (no leading
// dash), in order.
func (c *Context) Positional() []string {
	var out []string
	for _, a := range c.Args {
		if !strings.HasPrefix(a, "-") {
			out = append(out, a)
		}
	}
	return out
}

// SetArgs replaces the arguments with the command's own arguments
// (the command path has already been consumed by dispatch).
func (c *Context) SetArgs(args []string) { c.Args = args }

// SetExit records the process exit code to use.
func (c *Context) SetExit(code int) { c.exit = code }

// ExitCode returns the recorded exit code (zero by default).
func (c *Context) ExitCode() int { return c.exit }
