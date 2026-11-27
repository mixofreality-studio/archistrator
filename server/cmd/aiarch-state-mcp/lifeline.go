package main

// lifeline.go — what ends a one-shot gate (run-checks) that its parent no longer wants.
// run-checks starts make → testgen → go test, each with its own Temporal dev server, every
// child in its own process group (execCommand). Those groups are killed only when the
// context is cancelled, so the root context must be cancelled by the two ways the gate is
// told to stop: a termination signal, and the death of its parent.

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// parentPollInterval is how often the parent-death watch looks at the parent pid. macOS has
// no PR_SET_PDEATHSIG, so a child cannot ask the kernel to signal it when its parent dies;
// the one signal it gets is that its ppid changes (to 1, or to a subreaper).
const parentPollInterval = 500 * time.Millisecond

// lifelineContext derives from parent a context cancelled by SIGTERM, SIGINT or SIGHUP, or by
// the death of the process that started this one. stop releases the signal handler and the
// watcher.
func lifelineContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, stopSignals := signal.NotifyContext(parent, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	ctx, cancel := context.WithCancel(ctx)
	go watchParent(ctx, cancel, os.Getppid, parentPollInterval)
	return ctx, func() { cancel(); stopSignals() }
}

// watchParent records getppid at its start and calls cancel when it later returns a different
// pid. A start already at ppid 1 does not fire it: only a change does. It returns when ctx is
// done.
func watchParent(ctx context.Context, cancel context.CancelFunc, getppid func() int, every time.Duration) {
	start := getppid()
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if getppid() != start {
				cancel()
				return
			}
		}
	}
}
