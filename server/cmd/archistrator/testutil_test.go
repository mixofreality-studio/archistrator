package main

import (
	"io"
	"log/slog"
	"testing"
)

// testLogger returns a slog.Logger that discards output unless the test is
// run verbosely (-v), in which case it writes to the test log via t.Log.
func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	if !testing.Verbose() {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return slog.New(slog.NewTextHandler(testWriter{t}, nil))
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(string(p))
	return len(p), nil
}
