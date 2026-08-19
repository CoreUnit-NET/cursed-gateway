package applog

/*
Package applog owns process logging setup and shared levels.

Policy (verbose = -b / --verbose / VERBOSE):

Default (info+):
  info  — events and changes (handled, started, changed); not repeating loop noise
          HTTP access logs for handled requests; HTTP 4xx handler errors
  warn  — near limits, missing optional data with fallback, report-worthy but non-blocking
  error — real / unexpected failures (including HTTP 5xx handler errors)

Verbose also enables:
  debug — repeating / loop actions, /healthz access logs, and other low-interest chatter
  trace — deeper decision points with no existing log; never duplicate an info/debug line

Logging always uses log/slog with a text handler on stderr.
cmd_handler.Dispatch installs the process logger via slog.SetDefault.
*/

import (
	"log/slog"
	"os"
)

// LevelTrace sits below slog.LevelDebug so verbose turns on both.
const LevelTrace = slog.Level(-8)

// New builds the process logger. Default threshold is info; verbose enables trace+.
func New(verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = LevelTrace
	}
	opts := &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.LevelKey {
				if lvl, ok := a.Value.Any().(slog.Level); ok && lvl == LevelTrace {
					a.Value = slog.StringValue("TRACE")
				}
			}
			return a
		},
	}
	return slog.New(slog.NewTextHandler(os.Stderr, opts))
}
