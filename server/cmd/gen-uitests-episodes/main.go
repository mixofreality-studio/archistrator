// cmd/gen-uitests-episodes seeds a local project-state repo's episode ledger
// with the two episodes uitests/tests/episodes-panel.spec.ts asserts against:
// one SUCCEEDED episode carrying a real captured trace, and one GAP episode
// carrying no trace at all ("the badges are the point" — a gap is a present,
// first-class outcome, not an absence).
//
// It is the episodes sibling of cmd/gen-uitests-fixtures (which refreshes the
// coreUseCases fixture from this repo's own committed project.json): a small,
// mechanical, re-runnable seeding tool for the black-box UI harness, NOT
// production code and NOT wired into the server.
//
// ── WHY A SEEDER AND NOT A REAL DISPATCH ────────────────────────────────────
// The preferred way to put episodes in the ledger is a real local agentic
// dispatch (the capture seam's own write path). Two things make that
// insufficient here:
//
//  1. The GAP record has NO organic production path in a healthy run. A gap is
//     synthesized (systemdesign.episodeGapRecord) exactly when a dispatch
//     terminates having reported no episode summary at all — a fault this tool
//     cannot provoke on demand, and a successful real dispatch never produces.
//     The spec's central acceptance ("the gap chip renders, with its reason")
//     therefore requires a seeded gap either way.
//  2. A real dispatch is nondeterministic (model output, token counts, wall
//     duration) and costs real tokens on every re-provision. Task 12 and the
//     founder must be able to re-create this exact state on demand.
//
// ── WHAT IS "REAL" HERE ─────────────────────────────────────────────────────
// Nothing about the succeeded episode is hand-authored. Every field is DERIVED
// from a REAL captured stream-json trace
// (internal/resourceaccess/agenticjob/testdata/streamjson/*.jsonl — the Task-1
// capture fixtures, scrubbed but otherwise verbatim CLI output): the model, the
// token usage, the cost, the turn count, the outcome and the tool-call counts
// all come out of that file's own events, and the trace the UI renders IS that
// file, copied byte for byte. The derivation below deliberately mirrors the
// miner's reduction (agenticjob's parseEpisodeStream) rather than inventing a
// second story about the same bytes; it cannot CALL that miner because it is
// unexported and this is a different package.
//
// The GAP record is composed to the exact shape the production synthesizer
// emits (systemdesign.episodeGapRecord): outcome gap, a GapReason built from
// the Manager's own episodeMissingSummaryReason constant joined to a rail
// diagnostic, equal StartedAt/EndedAt (the run's own clock is what was lost),
// and NO TracePath.
//
// The ledger write itself goes through the REAL episodeAccess RA
// (episode.NewLocalFSEpisodeAccess + AppendEpisode) — the same component and
// the same on-disk encoding the server reads back — rather than hand-writing
// episodes.jsonl. The per-episode TRACE file is written directly, because the
// RA has no write-trace op: in production agenticjob owns that file (episodeAccess
// only ever reads it), so copying the captured fixture into place is the
// faithful stand-in for that writer.
//
// ── USAGE ───────────────────────────────────────────────────────────────────
// Run from server/ against the SAME repo the server's
// ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL points at (episodeAccess resolves its
// traces dir as <repoRoot>/.aiarch/traces — see episodeaccess.go's STORE LAYOUT):
//
//	GOWORK=off go run ./cmd/gen-uitests-episodes -repo /tmp/uitests-episodes-repo
//
// Re-running is safe: AppendEpisode is append-time idempotent on EpisodeID.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/episode"
)

// defaultTraceFixture is the Task-1 capture whose main loop contains a real
// `Write` tool_use — the tool event uitests/tests/episodes-panel.spec.ts asserts
// the expanded timeline renders.
const defaultTraceFixture = "internal/resourceaccess/agenticjob/testdata/streamjson/success_with_tools.jsonl"

// seedStartedAt pins the succeeded episode's clock so the seed is byte-stable
// across re-provisions (the capture fixture records durations, not wall-clock
// timestamps — the run's own start time was scrubbed with the rest of its
// identifying detail).
var seedStartedAt = time.Date(2026, 8, 2, 17, 4, 0, 0, time.UTC)

// episodeMissingSummaryReason is systemdesign's own GapReason constant for the
// "the run terminated and reported no episode at all" case — the one the
// never-silent rule exists for. Duplicated as a literal (it is unexported there,
// and this tool must not reach into a Manager's internals).
const episodeMissingSummaryReason = "terminal observation carried no episode summary"

// gapDiagnostic stands in for the rail diagnostic the observation carries in
// production; episodeGapReason joins it to the reason with an em-dash.
const gapDiagnostic = "the per-episode trace is INCOMPLETE, writing it failed: stream closed before the terminal result event"

func main() {
	repo := flag.String("repo", "", "path (or file:// URL) of the project-state repo root the server is configured with — REQUIRED")
	projectID := flag.String("project", "archistrator", "project id to record the episodes under")
	targetRef := flag.String("target", "Mission", "ledger TargetRef — the PascalCase artifactKindString form the capture-seam write path stamps for a design artifact")
	trace := flag.String("trace", defaultTraceFixture, "captured stream-json trace fixture to derive the succeeded episode from")
	successID := flag.String("success-id", "uitests-episode-success", "EpisodeID for the succeeded episode")
	gapID := flag.String("gap-id", "uitests-episode-gap", "EpisodeID for the gap episode")
	flag.Parse()

	if *repo == "" {
		fmt.Fprintln(os.Stderr, "gen-uitests-episodes: -repo is required")
		os.Exit(2)
	}
	if err := run(*repo, *projectID, *targetRef, *trace, *successID, *gapID); err != nil {
		fmt.Fprintf(os.Stderr, "gen-uitests-episodes: %v\n", err)
		os.Exit(1)
	}
}

// episodeIDPattern is episodeAccess's OWN path-traversal guard for an
// EpisodeID (episodeaccess.go) — an id names a file under the traces
// directory, so it must never carry a separator or "..". Re-applied here
// BEFORE the id is joined into a path, so this tool cannot be pointed at a
// file outside the traces dir by a malformed -success-id.
var episodeIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func run(repo, projectID, targetRef, tracePath, successID, gapID string) error {
	for _, id := range []string{successID, gapID} {
		if !episodeIDPattern.MatchString(id) {
			return fmt.Errorf("episode id %q contains characters outside [A-Za-z0-9._-]", id)
		}
	}

	absTrace, err := filepath.Abs(tracePath)
	if err != nil {
		return fmt.Errorf("resolve trace fixture: %w", err)
	}
	raw, err := os.ReadFile(absTrace) // #nosec G304 -- operator-supplied fixture path, a dev seeding tool
	if err != nil {
		return fmt.Errorf("read trace fixture: %w", err)
	}

	mined, err := mineFixture(raw)
	if err != nil {
		return fmt.Errorf("derive episode from %s: %w", absTrace, err)
	}

	// episodeAccess derives <repoRoot>/.aiarch/traces itself; hand it the repo
	// root exactly as the server's ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL does.
	access := episode.NewLocalFSEpisodeAccess(repo)
	rc := fwra.Context{}

	tracesDir, err := tracesDirOf(repo)
	if err != nil {
		return err
	}
	seededTrace := filepath.Join(tracesDir, successID+".jsonl")

	success := mined.record(successID, targetRef, seededTrace)
	if err := access.AppendEpisode(rc, episode.ProjectID(projectID), success); err != nil {
		return fmt.Errorf("append succeeded episode: %w", err)
	}

	// AppendEpisode has now created the traces dir (and its self-ignoring
	// .gitignore). Write the trace agenticjob would have written: the captured
	// fixture, verbatim.
	// #nosec G304,G703 -- successID is traversal-guarded by episodeIDPattern at the
	// top of run(), so seededTrace stays confined to tracesDir.
	if err := os.WriteFile(seededTrace, raw, 0o600); err != nil {
		return fmt.Errorf("write seeded trace file: %w", err)
	}

	gapReason := episodeMissingSummaryReason + " — " + gapDiagnostic
	gap := episode.EpisodeRecord{
		EpisodeID: gapID,
		Kind:      episode.EpisodeKindDesign,
		TargetRef: targetRef,
		Lineage: &episode.EpisodeLineage{
			WorkflowID: "coauthor-" + projectID + "-" + targetRef,
			RunID:      "uitests-run-gap",
		},
		// The gap's own clock is exactly what was lost — production stamps both
		// ends with the workflow's `now`. Seeded one minute after the succeeded
		// episode so ListEpisodes' StartedAt ordering is deterministic.
		StartedAt: seedStartedAt.Add(time.Minute),
		EndedAt:   seedStartedAt.Add(time.Minute),
		Outcome:   episode.EpisodeGap,
		GapReason: &gapReason,
	}
	if err := access.AppendEpisode(rc, episode.ProjectID(projectID), gap); err != nil {
		return fmt.Errorf("append gap episode: %w", err)
	}

	fmt.Printf("seeded %d episodes for project %q targetRef %q under %s\n", 2, projectID, targetRef, tracesDir)
	fmt.Printf("  %s  outcome=succeeded  model=%s  turns=%d  tools=%v  trace=%s\n",
		successID, deref(mined.Model), deref64(mined.NumTurns), mined.ToolCallCounts, seededTrace)
	fmt.Printf("  %s  outcome=gap        gapReason=%q  (no trace file, by construction)\n", gapID, gapReason)
	return nil
}

// tracesDirOf mirrors episodeAccess's own repoURL → traces-dir translation
// (episodeaccess.go's resolveTracesDir/localRepoPath) so this tool can name the
// per-episode trace file it must write alongside the ledger. The RA owns the
// ledger path; it exposes no trace WRITE op, so the path has to be re-derived
// here rather than asked for.
func tracesDirOf(repoURL string) (string, error) {
	path, ok := strings.CutPrefix(repoURL, "file://")
	if !ok {
		if strings.Contains(repoURL, "://") {
			return "", fmt.Errorf("local FS episode store requires a local file:// or plain-path repo, got %s", repoURL)
		}
		path = repoURL
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve repo path: %w", err)
	}
	return filepath.Join(abs, ".aiarch", "traces"), nil
}

// ---------------------------------------------------------------------------
// The fixture reduction — a stand-in for agenticjob's unexported miner.
// ---------------------------------------------------------------------------

// minedEpisode is what one captured trace file reduces to. Field for field this
// is the subset of agenticjob.EpisodeSummary the ledger record carries.
type minedEpisode struct {
	Model          *string
	Usage          episode.EpisodeUsage
	CostUSD        *float64
	NumTurns       *int64
	ToolCallCounts map[string]int64
	Outcome        episode.EpisodeOutcome
	DurationMS     int64
}

// streamLine is the tolerant subset of the stream-json event shapes this
// reduction reads. Everything it does not name is ignored, exactly as the real
// miner tolerates unknown events.
type streamLine struct {
	Type            string          `json:"type"`
	Subtype         string          `json:"subtype"`
	ParentToolUseID *string         `json:"parent_tool_use_id"`
	Message         *streamMessage  `json:"message"`
	Usage           *streamUsage    `json:"usage"`
	TotalCostUSD    *float64        `json:"total_cost_usd"`
	NumTurns        *int64          `json:"num_turns"`
	DurationMS      *int64          `json:"duration_ms"`
	IsError         *bool           `json:"is_error"`
	Result          json.RawMessage `json:"result"`
}

type streamMessage struct {
	Model   string              `json:"model"`
	Content []streamContentItem `json:"content"`
}

type streamContentItem struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

type streamUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

// mineFixture reduces a captured trace to the episode fields the ledger record
// carries. It reads ONLY what the file actually says: the model off the first
// assistant message, the tool-call counts off MAIN-LOOP tool_use blocks
// (parent_tool_use_id null — subagent tool calls are excluded, matching the
// miner's documented rule), and the usage/cost/turns/duration/outcome off the
// terminal `result` event.
func mineFixture(raw []byte) (minedEpisode, error) {
	var out minedEpisode
	var sawTerminal bool

	for _, line := range splitLines(raw) {
		var ev streamLine
		if err := json.Unmarshal(line, &ev); err != nil {
			continue // tolerant, same posture as the real miner
		}

		switch ev.Type {
		case "assistant":
			out.observeAssistant(ev)
		case "result":
			sawTerminal = true
			out.observeTerminal(ev)
		}
	}

	if !sawTerminal {
		return minedEpisode{}, fmt.Errorf("no terminal `result` event in fixture — that is a GAP capture, not a succeeded one")
	}
	return out, nil
}

// observeAssistant takes the model (first assistant message wins) and the
// MAIN-LOOP tool-call counts off one assistant event. An event carrying a
// parent_tool_use_id is a subagent's own turn: its tools are deliberately NOT
// counted, matching the real miner's documented rule.
func (m *minedEpisode) observeAssistant(ev streamLine) {
	if ev.Message == nil {
		return
	}
	if m.Model == nil && ev.Message.Model != "" {
		model := ev.Message.Model
		m.Model = &model
	}
	if ev.ParentToolUseID != nil {
		return
	}
	for _, c := range ev.Message.Content {
		if c.Type != "tool_use" || c.Name == "" {
			continue
		}
		if m.ToolCallCounts == nil {
			m.ToolCallCounts = map[string]int64{}
		}
		m.ToolCallCounts[c.Name]++
	}
}

// observeTerminal takes the whole-episode usage, cost, turn count, duration and
// outcome off the terminal `result` event — the only event that reports them.
func (m *minedEpisode) observeTerminal(ev streamLine) {
	if ev.Usage != nil {
		m.Usage = episode.EpisodeUsage{
			In:          ev.Usage.InputTokens,
			Out:         ev.Usage.OutputTokens,
			CacheRead:   ev.Usage.CacheReadInputTokens,
			CacheCreate: ev.Usage.CacheCreationInputTokens,
		}
	}
	m.CostUSD = ev.TotalCostUSD
	m.NumTurns = ev.NumTurns
	if ev.DurationMS != nil {
		m.DurationMS = *ev.DurationMS
	}
	m.Outcome = episode.EpisodeSucceeded
	if ev.Subtype != "success" || (ev.IsError != nil && *ev.IsError) {
		m.Outcome = episode.EpisodeFailed
	}
}

// record stamps the Manager-known fields the RA fixture cannot know (Kind,
// TargetRef, Lineage) onto the mined summary, exactly as
// systemdesign.episodeRecordFromSummary does on the real write path.
// WorkerClass is deliberately LEFT UNSET, matching that function's own rule: a
// design dispatch carries the artifact kind and the job mode, never the Phase-2
// activity list's workerClass, so there is no honest value to put here.
func (m minedEpisode) record(episodeID, targetRef, tracePath string) episode.EpisodeRecord {
	return episode.EpisodeRecord{
		EpisodeID: episodeID,
		Kind:      episode.EpisodeKindDesign,
		TargetRef: targetRef,
		Lineage: &episode.EpisodeLineage{
			WorkflowID: "coauthor-archistrator-" + targetRef,
			RunID:      "uitests-run-success",
		},

		Model:          m.Model,
		Usage:          m.Usage,
		CostUSD:        m.CostUSD,
		NumTurns:       m.NumTurns,
		ToolCallCounts: m.ToolCallCounts,
		StartedAt:      seedStartedAt,
		EndedAt:        seedStartedAt.Add(time.Duration(m.DurationMS) * time.Millisecond),
		Outcome:        m.Outcome,
		TracePath:      &tracePath,
	}
}

func splitLines(raw []byte) [][]byte {
	var out [][]byte
	start := 0
	for i := 0; i <= len(raw); i++ {
		if i == len(raw) || raw[i] == '\n' {
			if i > start {
				out = append(out, raw[start:i])
			}
			start = i + 1
		}
	}
	return out
}

func deref(s *string) string {
	if s == nil {
		return "<none>"
	}
	return *s
}

func deref64(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}
