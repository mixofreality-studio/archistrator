package harness

// Per-run Temporal namespace isolation.
//
// Every harness server registers workers on fixed task-queue names, so two
// server processes sharing one namespace steal each other's activity tasks and
// execute them against their own fakes (observed live: seven pollers on
// aiarch-test/system-design, a dispatch recorded against a foreign fake's
// repo, and retries against an already-shutdown fake port). A unique
// namespace per test PROCESS removes the whole interference class while the
// backing Temporal dev server stays shared and long-lived.
//
// The namespace is created with the `temporal` CLI — the dev server ships in
// that same binary, so anywhere the stack runs the CLI exists. When the CLI is
// missing (or creation fails for a reason other than "already exists") we fall
// back to the shared static namespace rather than failing the suite; the
// fallback is the pre-isolation behavior, just no longer the default.

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var (
	nsOnce     sync.Once
	nsResolved string
)

// processNamespace returns this test process's Temporal namespace, creating
// and propagating it on first use. hostPort addresses the shared dev server.
func processNamespace(hostPort string) string {
	nsOnce.Do(func() {
		nsResolved = registerFreshNamespace(hostPort)
	})
	return nsResolved
}

func registerFreshNamespace(hostPort string) string {
	const fallback = "aiarch-test"
	ns := fmt.Sprintf("aiarch-test-%d-%d", os.Getpid(), time.Now().UnixNano()%100000)

	if _, err := exec.LookPath("temporal"); err != nil {
		fmt.Fprintf(os.Stderr, "harness: temporal CLI not on PATH — falling back to shared namespace %q (cross-process task-queue interference possible)\n", fallback)
		return fallback
	}

	create := exec.Command("temporal", "operator", "namespace", "create",
		"--namespace", ns, "--address", hostPort, "--retention", "24h")
	if out, err := create.CombinedOutput(); err != nil && !strings.Contains(string(out), "already exists") {
		fmt.Fprintf(os.Stderr, "harness: namespace create failed (%v: %s) — falling back to shared namespace %q\n", err, strings.TrimSpace(string(out)), fallback)
		return fallback
	}

	// Registration propagates asynchronously; wait until describe succeeds so
	// the server subprocess never races an unknown-namespace error.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		describe := exec.Command("temporal", "operator", "namespace", "describe",
			"--namespace", ns, "--address", hostPort)
		if describe.Run() == nil {
			return ns
		}
		time.Sleep(500 * time.Millisecond)
	}
	fmt.Fprintf(os.Stderr, "harness: namespace %q never became describable — falling back to shared namespace %q\n", ns, fallback)
	return fallback
}
