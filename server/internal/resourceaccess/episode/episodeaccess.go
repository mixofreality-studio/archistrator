// Package episode is the episodeAccess component of the ResourceAccess layer
// — the sidecar store for captured agentic episodes (SP1 capture-seam: one
// EpisodeRecord per Manager-dispatched agentic run, plus the raw per-episode
// trace events agenticjob writes alongside it). See episode.md.
package episode

// episodeaccess.go carries BOTH hand-written variant constructors this
// component needs (Rule-1 single impl file — the 2026-07-11 layer
// file-layout standard): NewLocalFSEpisodeAccess, the LOCAL-PROFILE
// filesystem-backed realisation, and NewNoOpEpisodeAccess, the permanent
// no-op fallback. Neither has a generated New<Infra><Component> entry — the
// contract carries no `infra` binding for this component (see
// contract.gen.go's doc header) — so both are hand-written per the Task 3
// brief, same VARIANT-CONSTRUCTOR category as
// agenticjob.NewLocalExecAgenticJobAccess / usage.NewNoOpUsageAccess.
//
// STORE LAYOUT (local rail: ONE repo per server config, name-as-identity —
// there is no projectID→path mapping; projectID is recorded/validated on
// each ledger entry, never used for path resolution):
//
//	<repoRoot>/.aiarch/traces/episodes.jsonl     — append-only ledger, one
//	                                                storedEpisode JSON object
//	                                                per line.
//	<repoRoot>/.aiarch/traces/<episodeId>.jsonl  — raw per-episode trace
//	                                                events, written by
//	                                                agenticjob; read-only here.
//	<repoRoot>/.aiarch/traces/.gitignore         — self-ignoring ("*\n"),
//	                                                written on first use so
//	                                                operated repos need no
//	                                                scaffold change.
//
// AppendEpisode is APPEND-TIME idempotent on EpisodeID: Temporal's
// at-least-once redelivery means the same episode can be appended more than
// once, so a redelivered EpisodeID is detected BEFORE the write and dropped
// (a true no-op), not merely folded away later by ListEpisodes' last-wins —
// see TestAppendDedupesOnEpisodeID.

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// episodeIDPattern is the path-traversal guard for episodeID: it names a file
// under the traces directory (both the ledger dedupe key and the raw trace
// filename), so it must never contain a path separator or "..".
var episodeIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// ---------------------------------------------------------------------------
// LocalFS variant — the local-profile filesystem-backed realisation.
// ---------------------------------------------------------------------------

// localFSEpisodeAccess is the concrete, filesystem-backed implementation of
// EpisodeAccess for the local rail. UNEXPORTED — the package's only public
// surface is the generated EpisodeAccess interface + models plus the two
// hand-written constructors below (option-1 delegated DI, same shape as
// every other variant-constructor RA in this codebase). mu serialises every
// store access: AppendEpisode's dedupe check is scan-then-write, which must
// be atomic against concurrent callers in this process.
//
// repoURL is stored VERBATIM, unvalidated — see NewLocalFSEpisodeAccess.
type localFSEpisodeAccess struct {
	repoURL string
	mu      sync.Mutex
}

var _ EpisodeAccess = (*localFSEpisodeAccess)(nil)

// NewLocalFSEpisodeAccess builds the local-profile episodeAccess over
// repoURL, the SAME shared repo the local rail's other components bind to
// (NewGitLocalProjectStateAccess et al.) — one repo per server config,
// name-as-identity.
//
// SINGLE-RETURN, no error: this component's contract declares no `infra`
// binding, so composegen's generated main only ever calls this constructor as
// `episodeAccess = episode.NewLocalFSEpisodeAccess(repoURL)` — a plain
// single-value assignment, never `v, err :=` (there is no inverse escape
// hatch; see NewGitLocalConstructionTransitionAccess in
// projectstateaccess.go, the composegen precedent this mirrors).
//
// TRUE fidelity to that precedent means more than matching its arity: like
// NewGitLocalProjectStateAccess (via its gitRepoLocator closure), this
// constructor performs NO reachable validation — it stores repoURL as given
// and never fails. repoURL is translated to a filesystem path (a file:// URL
// stripped to its path, a plain path used as-is, anything else a
// configuration error) LAZILY, at first use in each of AppendEpisode /
// ListEpisodes / ReadTraceEvents (resolveTracesDir below) — exactly like
// agenticjob.localRepoPath's sibling validation in
// agenticjob.NewLocalExecAgenticJobAccess runs at ITS call time, not
// deferred, except here the caller with an error-return channel to use is
// the OP, not the constructor. A malformed
// ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL therefore surfaces as a clean typed
// fwra.ContractMisuse error from the first op that runs, not an unrecovered
// panic at boot (cmd/server installs no recover()).
func NewLocalFSEpisodeAccess(repoURL string) EpisodeAccess {
	return &localFSEpisodeAccess{repoURL: repoURL}
}

// resolveTracesDir lazily derives the traces directory from a.repoURL,
// duplicating the SAME translation NewLocalFSEpisodeAccess used to run
// eagerly (see that constructor's doc comment for why it moved here): a
// file:// URL is stripped to its path, a plain path is used as-is, and an
// empty or non-file-scheme repoURL is a ContractMisuse error tagged with the
// calling op.
func (a *localFSEpisodeAccess) resolveTracesDir(op string) (string, error) {
	repoPath, err := localRepoPath(op, a.repoURL)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(repoPath) == "" {
		return "", fwra.New(fwra.ContractMisuse, op+": empty repoURL")
	}
	return filepath.Join(repoPath, ".aiarch", "traces"), nil
}

// localRepoPath derives the shared repo's local filesystem path from the
// configured repoURL, tagging any error with op (the calling public verb —
// resolution now runs lazily at first use, see resolveTracesDir, so the
// caller is AppendEpisode/ListEpisodes/ReadTraceEvents, not the constructor).
// Copied from agenticjob.localRepoPath (agenticjobaccess.go:1249) — see that
// function's doc comment for the full rationale; duplicated rather than
// shared because the two RAs must not import each other (NoSideways).
func localRepoPath(op, repoURL string) (string, error) {
	if p, ok := strings.CutPrefix(repoURL, "file://"); ok && p != "" {
		return p, nil
	}
	if strings.Contains(repoURL, "://") {
		return "", fwra.New(fwra.ContractMisuse, op+": local FS episode store requires a local file:// or plain-path repoURL, got "+repoURL)
	}
	return repoURL, nil
}

// storedEpisode is the on-disk ledger line shape. EpisodeRecord (the
// generated contract type) carries no ProjectID field — AppendEpisode takes
// it as a separate parameter per the Task 2 contract — but the ledger is ONE
// shared file per repo backing ListEpisodes' ProjectID-scoped queries, so the
// project id must be persisted alongside each record.
type storedEpisode struct {
	ProjectID ProjectID     `json:"ProjectID"`
	Record    EpisodeRecord `json:"Record"`
}

func ledgerPath(tracesDir string) string {
	return filepath.Join(tracesDir, "episodes.jsonl")
}

// AppendEpisode appends one EpisodeRecord to the ledger, fsync'd. A
// redelivered EpisodeID (Temporal at-least-once dispatch) is detected before
// the write and dropped as a no-op — see the file header.
func (a *localFSEpisodeAccess) AppendEpisode(_ fwra.Context, projectID ProjectID, record EpisodeRecord) error {
	const op = "episode.AppendEpisode"
	if strings.TrimSpace(string(projectID)) == "" {
		return fwra.New(fwra.ContractMisuse, op+": empty projectID")
	}
	if strings.TrimSpace(record.EpisodeID) == "" {
		return fwra.New(fwra.ContractMisuse, op+": empty EpisodeID")
	}
	if !episodeIDPattern.MatchString(record.EpisodeID) {
		return fwra.New(fwra.ContractMisuse, op+": EpisodeID contains characters outside [A-Za-z0-9._-]")
	}

	dir, err := a.resolveTracesDir(op)
	if err != nil {
		return err
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if err := ensureTracesDirLocked(dir); err != nil {
		return fwra.Wrap(fwra.Infrastructure, err, op+": ensure traces dir")
	}

	entries, err := readLedgerLocked(dir)
	if err != nil {
		return fwra.Wrap(fwra.Infrastructure, err, op+": dedupe scan")
	}
	for _, e := range entries {
		if e.Record.EpisodeID == record.EpisodeID {
			return nil // at-least-once redelivery: same EpisodeID, append-time no-op.
		}
	}

	// If the ledger's last line is torn (a crash mid-write left it with no
	// trailing newline — the same artifact readLedgerLocked tolerates by
	// skipping the line on read), a plain O_APPEND write would glue this
	// new record onto the torn tail and corrupt IT too. Lead with a
	// newline in that case so the new record always starts its own line.
	needsLeadingNL, err := ledgerTailNeedsNewline(ledgerPath(dir))
	if err != nil {
		return fwra.Wrap(fwra.Infrastructure, err, op+": check ledger tail")
	}

	line, err := json.Marshal(storedEpisode{ProjectID: projectID, Record: record})
	if err != nil {
		return fwra.Wrap(fwra.ContractMisuse, err, op+": marshal record")
	}
	if needsLeadingNL {
		line = append([]byte("\n"), line...)
	}
	line = append(line, '\n')

	f, err := os.OpenFile(ledgerPath(dir), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fwra.Wrap(fwra.Infrastructure, err, op+": open ledger")
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(line); err != nil {
		return fwra.Wrap(fwra.Infrastructure, err, op+": write ledger")
	}
	if err := f.Sync(); err != nil {
		return fwra.Wrap(fwra.Infrastructure, err, op+": fsync ledger")
	}
	return nil
}

// ListEpisodes returns the ProjectID-scoped (optionally TargetRef-filtered)
// episodes, deduped by EpisodeID (last wins) and sorted by StartedAt. Dedupe
// here is belt-and-suspenders over AppendEpisode's append-time skip — it
// tolerates any duplicate lines that reach the ledger by another path (e.g.
// hand edits, a second uncoordinated writer) without ever double-counting.
func (a *localFSEpisodeAccess) ListEpisodes(_ fwra.Context, query EpisodeQuery) ([]EpisodeRecord, error) {
	const op = "episode.ListEpisodes"
	if strings.TrimSpace(string(query.ProjectID)) == "" {
		return nil, fwra.New(fwra.ContractMisuse, op+": empty ProjectID")
	}

	dir, err := a.resolveTracesDir(op)
	if err != nil {
		return nil, err
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	entries, err := readLedgerLocked(dir)
	if err != nil {
		return nil, fwra.Wrap(fwra.Infrastructure, err, op+": read ledger")
	}

	byID := make(map[string]EpisodeRecord, len(entries))
	order := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.ProjectID != query.ProjectID {
			continue
		}
		if query.TargetRef != nil && e.Record.TargetRef != *query.TargetRef {
			continue
		}
		if _, seen := byID[e.Record.EpisodeID]; !seen {
			order = append(order, e.Record.EpisodeID)
		}
		byID[e.Record.EpisodeID] = e.Record // last wins
	}

	out := make([]EpisodeRecord, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	sortByStartedAt(out)
	return out, nil
}

// sortByStartedAt sorts records ascending by StartedAt with a plain
// insertion sort — the ledger is a local-profile dogfood store (episode
// counts are small), so pulling in "sort" for one call site isn't worth it.
func sortByStartedAt(out []EpisodeRecord) {
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].StartedAt.Before(out[j-1].StartedAt); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
}

// ReadTraceEvents returns the raw JSON events written to
// <episodeId>.jsonl by agenticjob, one per line, in file order. A missing
// trace file is fwra.NotFound.
func (a *localFSEpisodeAccess) ReadTraceEvents(_ fwra.Context, projectID ProjectID, episodeID string) ([]json.RawMessage, error) {
	const op = "episode.ReadTraceEvents"
	if strings.TrimSpace(string(projectID)) == "" {
		return nil, fwra.New(fwra.ContractMisuse, op+": empty projectID")
	}
	if !episodeIDPattern.MatchString(episodeID) {
		return nil, fwra.New(fwra.ContractMisuse, op+": episodeID contains characters outside [A-Za-z0-9._-]")
	}

	dir, err := a.resolveTracesDir(op)
	if err != nil {
		return nil, err
	}

	// episodeID is validated against episodeIDPattern above (no "/" or ".."), so
	// path stays confined to dir — the traversal guard gosec is asking for.
	path := filepath.Join(dir, episodeID+".jsonl")
	data, err := os.ReadFile(path) // #nosec G304 -- episodeID traversal-guarded above
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fwra.Wrap(fwra.NotFound, err, op+": no trace file for episode "+episodeID)
		}
		return nil, fwra.Wrap(fwra.Infrastructure, err, op+": read trace file")
	}

	var out []json.RawMessage
	for _, l := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(l)) == 0 {
			continue
		}
		raw := make(json.RawMessage, len(l))
		copy(raw, l)
		out = append(out, raw)
	}
	return out, nil
}

// ensureTracesDirLocked creates the traces directory if absent and writes
// the self-ignoring .gitignore ("*\n") on first use, iff it does not already
// exist. Caller must hold a.mu.
func ensureTracesDirLocked(tracesDir string) error {
	if err := os.MkdirAll(tracesDir, 0o750); err != nil {
		return err
	}
	gi := filepath.Join(tracesDir, ".gitignore")
	f, err := os.OpenFile(gi, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // #nosec G304 -- fixed literal filename under tracesDir
	if err != nil {
		if os.IsExist(err) {
			return nil
		}
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.WriteString("*\n")
	return err
}

// readLedgerLocked reads and parses every ledger line. A missing ledger
// (nothing appended yet) is an empty, non-nil-error result — NOT NotFound.
//
// A malformed line does NOT brick the store: it is skipped rather than
// failing the whole read. A truncated FINAL line is the expected artifact of
// a crash mid-append; mid-file corruption gets the same tolerant treatment —
// this is a local dogfood store where availability beats strictness, and
// list-time last-wins dedupe (ListEpisodes) already tolerates extra/
// duplicate lines, so tolerating a corrupt one is the same posture. Every
// skip is logged (non-fatal) with its 1-based line number so corruption is
// still observable without taking ListEpisodes or AppendEpisode's dedupe
// scan down. Caller must hold a.mu.
func readLedgerLocked(tracesDir string) ([]storedEpisode, error) {
	path := ledgerPath(tracesDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []storedEpisode
	var skipped int
	for i, l := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(l)) == 0 {
			continue
		}
		var e storedEpisode
		if err := json.Unmarshal(l, &e); err != nil {
			skipped++
			slog.Warn("episode ledger: skipping unparseable line (tolerant scan)",
				"path", path, "line", i+1, "error", err)
			continue
		}
		out = append(out, e)
	}
	if skipped > 0 {
		slog.Warn("episode ledger: scan completed with skipped line(s)",
			"path", path, "skipped", skipped, "kept", len(out))
	}
	return out, nil
}

// ledgerTailNeedsNewline reports whether path exists, is non-empty, and does
// NOT already end with a newline — i.e. its last line is torn (a crash
// mid-append, the same artifact readLedgerLocked tolerates on read). A
// missing ledger needs no leading newline (O_CREATE starts it fresh).
func ledgerTailNeedsNewline(path string) (bool, error) {
	f, err := os.Open(path) // #nosec G304 -- fixed ledger path, not user input
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return false, err
	}
	if info.Size() == 0 {
		return false, nil
	}
	last := make([]byte, 1)
	if _, err := f.ReadAt(last, info.Size()-1); err != nil {
		return false, err
	}
	return last[0] != '\n', nil
}

// ---------------------------------------------------------------------------
// No-op variant.
// ---------------------------------------------------------------------------

// noopEpisodeAccess is the permanent no-op EpisodeAccess: appends and lists
// nothing, logs each call at debug, and NEVER errors — unlike
// operatedsystemstate/usage's no-op siblings, it does not even enforce
// caller-misuse preconditions, since episode capture is a best-effort
// observability seam, not a contract the Manager's happy path depends on.
type noopEpisodeAccess struct{}

var _ EpisodeAccess = noopEpisodeAccess{}

// NewNoOpEpisodeAccess returns the permanent no-op EpisodeAccess. It takes no
// arguments — there is no infrastructure binding.
func NewNoOpEpisodeAccess() EpisodeAccess { return noopEpisodeAccess{} }

func (noopEpisodeAccess) AppendEpisode(rc fwra.Context, projectID ProjectID, record EpisodeRecord) error {
	slog.DebugContext(logCtx(rc), "episode AppendEpisode no-op (episodeAccess is no-op)",
		"projectID", projectID, "episodeID", record.EpisodeID)
	return nil
}

func (noopEpisodeAccess) ListEpisodes(rc fwra.Context, query EpisodeQuery) ([]EpisodeRecord, error) {
	slog.DebugContext(logCtx(rc), "episode ListEpisodes no-op (episodeAccess is no-op)",
		"projectID", query.ProjectID)
	return []EpisodeRecord{}, nil
}

func (noopEpisodeAccess) ReadTraceEvents(rc fwra.Context, projectID ProjectID, episodeID string) ([]json.RawMessage, error) {
	slog.DebugContext(logCtx(rc), "episode ReadTraceEvents no-op (episodeAccess is no-op)",
		"projectID", projectID, "episodeID", episodeID)
	return []json.RawMessage{}, nil
}

// logCtx returns the call's context.Context for structured logging,
// defaulting to context.Background when the RA context carries none. Same
// helper as sourcecontrol.logCtx (sourcecontrolaccess.go) — duplicated
// rather than shared (NoSideways).
func logCtx(rc fwra.Context) context.Context {
	if rc.Context != nil {
		return rc.Context
	}
	return context.Background()
}
