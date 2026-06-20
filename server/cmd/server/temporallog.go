package main

import (
	"log/slog"

	temporallog "go.temporal.io/sdk/log"
)

// temporalLogger adapts the composition root's slog.Logger onto the Temporal SDK
// log.Logger interface so the embedded Worker / client logs flow through the same
// structured JSON sink as the rest of the server.
type temporalLogger struct {
	l *slog.Logger
}

func newTemporalLogger(l *slog.Logger) temporallog.Logger {
	return temporalLogger{l: l.With("source", "temporal")}
}

func (t temporalLogger) Debug(msg string, kv ...any) { t.l.Debug(msg, kv...) }
func (t temporalLogger) Info(msg string, kv ...any)  { t.l.Info(msg, kv...) }
func (t temporalLogger) Warn(msg string, kv ...any)  { t.l.Warn(msg, kv...) }
func (t temporalLogger) Error(msg string, kv ...any) { t.l.Error(msg, kv...) }
