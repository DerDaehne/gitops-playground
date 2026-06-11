// Package log wires up the global slog logger.
//
// The Groovy code uses logback with three modes: default, --debug and --trace.
// We mirror that with three slog levels: Info, Debug, Trace (custom level
// below Debug). The handler uses a terse text format to match the simplified
// Groovy pattern produced by GitopsPlaygroundCli.setSimpleLogPattern.
package log

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
)

// LevelTrace is one step finer than slog.LevelDebug.
const LevelTrace = slog.Level(-8)

// Mode controls the global verbosity.
type Mode int

const (
	ModeInfo Mode = iota
	ModeDebug
	ModeTrace
)

var (
	mu      sync.Mutex
	current Mode
)

// Configure installs a global slog logger writing to w with the given mode.
// Calling Configure twice replaces the previous logger.
func Configure(w io.Writer, m Mode) {
	mu.Lock()
	defer mu.Unlock()

	if w == nil {
		w = os.Stderr
	}

	var lvl slog.Level
	switch m {
	case ModeTrace:
		lvl = LevelTrace
	case ModeDebug:
		lvl = slog.LevelDebug
	default:
		lvl = slog.LevelInfo
	}

	handler := slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: lvl,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Drop the source attribute (we never enabled it) and shorten time.
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	})
	current = m
	slog.SetDefault(slog.New(handler))
}

// CurrentMode returns the active verbosity mode.
func CurrentMode() Mode {
	mu.Lock()
	defer mu.Unlock()
	return current
}

// Trace logs at the custom trace level.
func Trace(msg string, args ...any) {
	slog.Default().Log(context.Background(), LevelTrace, msg, args...)
}
