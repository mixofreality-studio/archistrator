package episode

// SERVICE TEST PLAN (STP) — episodeAccess local-filesystem sidecar store
// (SP1 capture-seam Task 3). Black-box at the RA's public verbs against a
// t.TempDir() repo root; no fakes needed since the local rail IS the
// filesystem. NO live git, NO BDD.
//
//   PRE-CONDITION / CONTRACT-MISUSE:
//     U1  NewLocalFSEpisodeAccess rejects a non-local (non file://) repoURL
//     U2  AppendEpisode rejects an empty projectID
//     U3  AppendEpisode rejects an empty EpisodeID
//     U4  ListEpisodes rejects an empty ProjectID
//     U5  ReadTraceEvents rejects a malformed episodeID (path traversal)
//
//   HAPPY PATH / STORE SEMANTICS:
//     U6  Append then list round-trips a record
//     U7  ListEpisodes filters by TargetRef
//     U8  Append is append-only (ledger grows one line per distinct EpisodeID)
//     U9  First use writes the self-ignoring .gitignore ("*\n") exactly once
//     U10 ReadTraceEvents returns each line of the raw trace file as a raw event
//     U11 ReadTraceEvents on an unknown episodeID is an error, not an empty slice
//     U12 AppendEpisode dedupes on EpisodeID: a second append with the SAME id is
//         an APPEND-TIME no-op — it does not grow the ledger, and it does not
//         overwrite the first record's fields (proving the skip happens before
//         the write, not merely at list-time last-wins folding)
//
//   NO-OP VARIANT:
//     U13 NewNoOpEpisodeAccess never errors and persists nothing

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

func rc() fwra.Context { return fwra.Context{Context: context.Background()} }

// newTestAccess wires a LocalFS episodeAccess over a fresh t.TempDir() repo
// root, returning both the access and the root so tests can assert directly
// on-disk (ledger contents, .gitignore, raw trace files). NewLocalFSEpisodeAccess
// is single-return (Task 8: composegen's generated main threads it as a plain
// `episodeAccess = episode.NewLocalFSEpisodeAccess(repoURL)` assignment, no
// `v, err :=` — the contract declares no `infra` binding) — a valid repoURL never
// panics, so there is nothing to check here.
func newTestAccess(t *testing.T) (EpisodeAccess, string) {
	t.Helper()
	root := t.TempDir()
	a := NewLocalFSEpisodeAccess("file://" + root)
	return a, root
}

// testRecord builds a minimal valid EpisodeRecord for id/outcome.
func testRecord(id string, outcome EpisodeOutcome) EpisodeRecord {
	now := time.Now().UTC()
	return EpisodeRecord{
		EpisodeID: id,
		Kind:      EpisodeKindConstruction,
		TargetRef: "activity/1",
		Usage:     EpisodeUsage{In: 10, Out: 20},
		StartedAt: now,
		EndedAt:   now.Add(time.Minute),
		Outcome:   outcome,
	}
}

func tracesDir(root string) string { return filepath.Join(root, ".aiarch", "traces") }

func assertKind(t *testing.T, err error, want fwra.Kind) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error of kind %s, got nil", want)
	}
	var e *fwra.Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *fwra.Error, got %T: %v", err, err)
	}
	if e.Kind != want {
		t.Fatalf("expected kind %s, got %s (detail: %s)", want, e.Kind, e.Detail)
	}
}

// ---------------------------------------------------------------------------
// U1-U5: pre-condition / contract-misuse.
// ---------------------------------------------------------------------------

// TestNewLocalFSEpisodeAccessRejectsNonLocalRepoURL: single-return construction
// (Task 8) turns a bad repoURL into a panic-on-construct rather than a returned
// error — the same posture as the composegen precedent this constructor mirrors
// (NewGitLocalConstructionTransitionAccess in projectstateaccess.go).
func TestNewLocalFSEpisodeAccessRejectsNonLocalRepoURL(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected NewLocalFSEpisodeAccess to panic on a non-local repoURL")
		}
	}()
	NewLocalFSEpisodeAccess("https://example.com/repo.git")
}

func TestAppendRejectsEmptyProjectID(t *testing.T) {
	a, _ := newTestAccess(t)
	err := a.AppendEpisode(rc(), "", testRecord("ep-1", EpisodeSucceeded))
	assertKind(t, err, fwra.ContractMisuse)
}

func TestAppendRejectsEmptyEpisodeID(t *testing.T) {
	a, _ := newTestAccess(t)
	err := a.AppendEpisode(rc(), "p1", testRecord("", EpisodeSucceeded))
	assertKind(t, err, fwra.ContractMisuse)
}

func TestListRejectsEmptyProjectID(t *testing.T) {
	a, _ := newTestAccess(t)
	_, err := a.ListEpisodes(rc(), EpisodeQuery{})
	assertKind(t, err, fwra.ContractMisuse)
}

func TestReadTraceEventsRejectsPathTraversal(t *testing.T) {
	a, _ := newTestAccess(t)
	_, err := a.ReadTraceEvents(rc(), "p1", "../../etc/passwd")
	assertKind(t, err, fwra.ContractMisuse)
}

// ---------------------------------------------------------------------------
// U6-U12: happy path / store semantics.
// ---------------------------------------------------------------------------

func TestAppendThenList(t *testing.T) {
	a, _ := newTestAccess(t)
	rec := testRecord("ep-1", EpisodeSucceeded)
	if err := a.AppendEpisode(rc(), "p1", rec); err != nil {
		t.Fatal(err)
	}
	got, err := a.ListEpisodes(rc(), EpisodeQuery{ProjectID: "p1"})
	if err != nil || len(got) != 1 || got[0].EpisodeID != "ep-1" {
		t.Fatalf("got %v err %v", got, err)
	}
}

func TestListFiltersByTargetRef(t *testing.T) {
	a, _ := newTestAccess(t)
	r1 := testRecord("ep-1", EpisodeSucceeded)
	r1.TargetRef = "activity/1"
	r2 := testRecord("ep-2", EpisodeSucceeded)
	r2.TargetRef = "activity/2"
	if err := a.AppendEpisode(rc(), "p1", r1); err != nil {
		t.Fatal(err)
	}
	if err := a.AppendEpisode(rc(), "p1", r2); err != nil {
		t.Fatal(err)
	}

	want := "activity/2"
	got, err := a.ListEpisodes(rc(), EpisodeQuery{ProjectID: "p1", TargetRef: &want})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].EpisodeID != "ep-2" {
		t.Fatalf("got %v, want exactly ep-2", got)
	}
}

func TestAppendIsAppendOnly(t *testing.T) {
	a, root := newTestAccess(t)
	if err := a.AppendEpisode(rc(), "p1", testRecord("ep-1", EpisodeSucceeded)); err != nil {
		t.Fatal(err)
	}
	firstLine := readLedgerLines(t, root)[0]

	if err := a.AppendEpisode(rc(), "p1", testRecord("ep-2", EpisodeSucceeded)); err != nil {
		t.Fatal(err)
	}
	lines := readLedgerLines(t, root)
	if len(lines) != 2 {
		t.Fatalf("ledger has %d lines, want 2: %v", len(lines), lines)
	}
	if lines[0] != firstLine {
		t.Fatalf("first ledger line changed:\nwas:  %s\nnow:  %s", firstLine, lines[0])
	}
}

func TestSelfIgnoringGitignore(t *testing.T) {
	a, root := newTestAccess(t)
	if err := a.AppendEpisode(rc(), "p1", testRecord("ep-1", EpisodeSucceeded)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(tracesDir(root), ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if string(data) != "*\n" {
		t.Fatalf(".gitignore content = %q, want %q", string(data), "*\n")
	}
}

func TestReadTraceEvents(t *testing.T) {
	a, root := newTestAccess(t)
	dir := tracesDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := "{\"a\":1}\n{\"a\":2}\n{\"a\":3}\n"
	if err := os.WriteFile(filepath.Join(dir, "ep-1.jsonl"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := a.ReadTraceEvents(rc(), "p1", "ep-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3: %v", len(got), got)
	}
	var v struct{ A int }
	if err := json.Unmarshal(got[1], &v); err != nil || v.A != 2 {
		t.Fatalf("event[1] = %s, want {\"a\":2}", got[1])
	}
}

func TestReadTraceMissingIsError(t *testing.T) {
	a, _ := newTestAccess(t)
	got, err := a.ReadTraceEvents(rc(), "p1", "no-such-episode")
	if err == nil {
		t.Fatalf("expected error, got events %v", got)
	}
	assertKind(t, err, fwra.NotFound)
}

func TestAppendDedupesOnEpisodeID(t *testing.T) {
	a, root := newTestAccess(t)

	first := testRecord("ep-1", EpisodeSucceeded)
	if err := a.AppendEpisode(rc(), "p1", first); err != nil {
		t.Fatal(err)
	}

	// Redelivery of the SAME EpisodeID, carrying DIFFERENT data (a distinct
	// CostUSD and Outcome) — if the dedupe were only a list-time last-wins
	// fold, this second write would still land on disk and last-wins would
	// surface ITS values. The append-time no-op must instead discard it
	// entirely, so the ledger never grows and the ORIGINAL values survive.
	redelivered := testRecord("ep-1", EpisodeFailed)
	cost := 99.0
	redelivered.CostUSD = &cost
	if err := a.AppendEpisode(rc(), "p1", redelivered); err != nil {
		t.Fatal(err)
	}

	lines := readLedgerLines(t, root)
	if len(lines) != 1 {
		t.Fatalf("ledger has %d lines after redelivered append, want 1 (append-time no-op): %v", len(lines), lines)
	}

	got, err := a.ListEpisodes(rc(), EpisodeQuery{ProjectID: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d episodes, want 1", len(got))
	}
	if got[0].Outcome != EpisodeSucceeded || got[0].CostUSD != nil {
		t.Fatalf("got %+v, want the FIRST append's values (Outcome=Succeeded, CostUSD=nil) untouched by the redelivered append", got[0])
	}
}

// TestListToleratesCorruptTrailingLine proves a malformed ledger line (the
// expected artifact of a crash mid-write, or any mid-file corruption) does
// not brick the store: ListEpisodes must skip it and return the valid
// records with NO error, and a subsequent AppendEpisode — whose dedupe scan
// reads the same ledger — must still succeed (availability beats strictness
// for this local dogfood store; list-time last-wins dedupe already tolerates
// extra/duplicate lines, so tolerating a corrupt one is the same posture).
func TestListToleratesCorruptTrailingLine(t *testing.T) {
	a, root := newTestAccess(t)
	if err := a.AppendEpisode(rc(), "p1", testRecord("ep-1", EpisodeSucceeded)); err != nil {
		t.Fatal(err)
	}
	if err := a.AppendEpisode(rc(), "p1", testRecord("ep-2", EpisodeSucceeded)); err != nil {
		t.Fatal(err)
	}

	// Simulate a crash mid-write: a truncated JSON fragment as the ledger's
	// final line, written directly to disk (bypassing AppendEpisode, no
	// trailing newline — exactly what an interrupted append leaves behind).
	ledger := filepath.Join(tracesDir(root), "episodes.jsonl")
	f, err := os.OpenFile(ledger, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"ProjectID":"p1","Record":{"EpisodeID":"ep-3","Kind":0,"TargetR`); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := a.ListEpisodes(rc(), EpisodeQuery{ProjectID: "p1"})
	if err != nil {
		t.Fatalf("ListEpisodes returned an error on a corrupt trailing line, want a tolerant skip: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d episodes, want 2 (corrupt line skipped): %v", len(got), got)
	}

	// The dedupe scan inside AppendEpisode reads the same ledger — it must
	// also tolerate the corrupt line rather than permanently bricking future
	// appends.
	if err := a.AppendEpisode(rc(), "p1", testRecord("ep-4", EpisodeSucceeded)); err != nil {
		t.Fatalf("AppendEpisode after corrupt line: %v", err)
	}
	got, err = a.ListEpisodes(rc(), EpisodeQuery{ProjectID: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d episodes after new append, want 3: %v", len(got), got)
	}
}

// readLedgerLines reads the raw non-empty lines of episodes.jsonl under root.
func readLedgerLines(t *testing.T, root string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(tracesDir(root), "episodes.jsonl"))
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	var lines []string
	start := 0
	for i, b := range data {
		if b == '\n' {
			if i > start {
				lines = append(lines, string(data[start:i]))
			}
			start = i + 1
		}
	}
	return lines
}

// ---------------------------------------------------------------------------
// U13: no-op variant.
// ---------------------------------------------------------------------------

func TestNoOpEpisodeAccessNeverErrors(t *testing.T) {
	a := NewNoOpEpisodeAccess()

	if err := a.AppendEpisode(rc(), "", EpisodeRecord{}); err != nil {
		t.Fatalf("AppendEpisode: %v", err)
	}
	got, err := a.ListEpisodes(rc(), EpisodeQuery{})
	if err != nil {
		t.Fatalf("ListEpisodes: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListEpisodes = %v, want empty", got)
	}
	events, err := a.ReadTraceEvents(rc(), "", "")
	if err != nil {
		t.Fatalf("ReadTraceEvents: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("ReadTraceEvents = %v, want empty", events)
	}
}
