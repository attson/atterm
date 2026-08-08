package main

import (
	"log"
	"os"

	"github.com/attson/atterm/internal/logging"
)

// configureLogging points the shared logger at stderr — where a container
// runtime expects it — and sets the write threshold.
//
// The relay has no log file of its own: `docker logs` is the log. What this
// buys is the same `TS LEVEL [tag] message` shape the desktop app writes, so
// one grep works across a support bundle and a container's output.
func configureLogging(level string, debugFlag bool) {
	logging.SetSink(os.Stderr)

	resolved := logging.ParseLevelOr(level, logging.LevelInfo)
	// --debug / ATTERM_RELAY_DEBUG predates --log-level and is what operators
	// reach for, so it wins over a higher threshold rather than being silently
	// overridden by one.
	if debugFlag && resolved > logging.LevelDebug {
		resolved = logging.LevelDebug
	}
	logging.SetLevel(resolved)

	// Anything still using the standard logger — net/http's error log,
	// dependencies, and this package's own fatal paths — gets normalized into
	// the same format instead of showing up as a bare line.
	log.SetFlags(0)
	log.SetOutput(logging.StdlibWriter(logging.LevelError, "relay"))
}
