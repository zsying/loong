package loong

import (
	"os"
	"os/signal"
	"syscall"
)

// RunOption configures a single LoadAndRun call.
type RunOption func(*runConfig)

// runConfig holds the resolved options for one LoadAndRun invocation.
type runConfig struct {
	wait bool
}

// WithWait makes LoadAndRun block until the process receives SIGINT or
// SIGTERM, then gracefully shut the tree down and return. Without it,
// LoadAndRun returns immediately after the tree is running, leaving the
// caller to decide when to stop (e.g. drive Shutdown on its own signal
// loop). WithWait is the typical choice for a server-style binary.
func WithWait() RunOption {
	return func(c *runConfig) { c.wait = true }
}

// LoadAndRun is the one-call entry point: it loads the config tree from
// path, assembles a fresh Kernel, runs every component, and — when
// WithWait is set — blocks for a termination signal before shutting down.
// The lower-level LoadTree/New/Assemble remain available for callers that
// need to interpose (tests, embedding, custom kernels).
//
// On success the returned *Kernel is non-nil even when wait is false, so
// callers can trigger Shutdown themselves.
func LoadAndRun(path string, opts ...RunOption) (*Kernel, error) {
	var cfg runConfig
	for _, o := range opts {
		o(&cfg)
	}
	root, err := LoadTree(path)
	if err != nil {
		return nil, err
	}
	k := New()
	if err := k.Assemble(root); err != nil {
		return nil, err
	}
	if cfg.wait {
		if err := k.Wait(); err != nil {
			return k, err
		}
	}
	return k, nil
}

// Wait blocks until the process receives SIGINT or SIGTERM, then shuts
// the tree down. Useful when a caller assembles a Kernel directly and
// wants the standard signal-driven shutdown without LoadAndRun.
func (k *Kernel) Wait() error {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	signal.Stop(ch)
	return k.Shutdown()
}
