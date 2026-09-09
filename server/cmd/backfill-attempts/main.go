// cmd/backfill-attempts derives TaskAttempt records for activities whose construction
// history is recoverable from evidence that already exists in project.json — a frozen
// service contract, a built component recorded in the implementation log.
//
// It is a ONE-SHOT tool whose output is a REVIEWABLE COMMITTED DIFF. No server code
// path may fabricate an attempt at request time; that is rule 4 of the provenance
// contract, and it is what stops "the UI generates plausible history" from becoming
// permanent architecture. Synthesis that lives in a read path is invisible, unversioned
// and unrevertable; synthesis that lives in a commit is none of those things.
//
// Every attempt it writes is stamped OriginBackfilled with a basis naming the evidence,
// and every attempt is run through AttemptProvenance.Validate before anything is
// written — a backfilled record with an empty basis is a hard error, not a silent nil
// on the wire. Activities with NO evidence get NO attempts: absence stays absence. The
// list view renders those as an honest unknown skeleton, which is the point.
//
// Usage (from server/):
//
//	GOWORK=off go run ./cmd/backfill-attempts -repo .. [-dry-run]
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// statePath is the project.json location relative to the repository root. It is
// compiler input as much as it is state (it drives the Go contract layer, the OpenAPI
// document and the TS client), which is why this tool rewrites exactly one key inside
// it and splices the result back into the original bytes.
var statePath = filepath.Join(".aiarch", "state", "project.json")

// evidence is what we could actually find for one activity, read from its own committed
// .activityConstruction record. Nothing here is inferred from an id, a naming
// convention or a status field: every field is set only because a produced artifact
// says so.
type evidence struct {
	// HasServiceContract is set by a produced artifact of kind "service-contract".
	HasServiceContract bool
	// ContractRef is the live .serviceContracts key when the contract file resolves to
	// one, and the corpus path otherwise.
	ContractRef string
	// ContractBasis overrides the default serviceContracts[<ContractRef>] basis. It is
	// set for the rows whose frozen contract names a component that no longer has a
	// .serviceContracts entry — pointing those at serviceContracts[…] would be a
	// dangling reference dressed up as provenance.
	ContractBasis string
	// HasMergedCode is set by a produced artifact of kind "code".
	HasMergedCode bool
	// GitRef is the ref recorded for the built component. In the committed corpus that
	// is a path into the implementation log, not a commit sha.
	GitRef string
	// CodeKind is the evidence kind for GitRef; EvidenceGit when unset. The committed
	// rows set it to EvidenceArtifact because the kind is the UI's click dispatch, and
	// telling the UI to open "implementation/log" as a git ref would simply be wrong.
	CodeKind projectstate.EvidenceKind
	// CodeBasis overrides the default activityGit[<GitRef>] basis for the same reason
	// ContractBasis exists: the basis must name a place that exists.
	CodeBasis string
}

// generatorID identifies this tool in every provenance stamp it writes.
var generatorID = "cmd/backfill-attempts"

// attemptsFor derives the recoverable attempts for one activity.
//
// The mapping is deliberately conservative — only two inferences are made, and each
// names the artifact it was read from:
//   - a frozen service contract means Detailed Design ran and Design Review passed
//   - merged code means Construction ran and Code Review passed
//
// Everything else stays unknown. We do NOT infer SRS, STP, Test Client, Integration or
// Testing from anything, because no evidence for them exists in the committed state.
//
// The task vocabulary comes from the PROFILE, never from the evidence: an inference is
// dropped when the activity's type has no phase for it.
func attemptsFor(activityID string, typ projectstate.ActivityType, ev evidence) []projectstate.TaskAttempt {
	profile := projectstate.ProfileFor(typ, projectstate.TestVariantPlan)
	allowed := map[projectstate.MethodTask]bool{}
	for _, task := range projectstate.TasksForProfile(profile) {
		allowed[task] = true
	}

	now := time.Now().UTC()
	out := []projectstate.TaskAttempt{}

	add := func(task projectstate.MethodTask, ref projectstate.EvidenceRef, basis string) {
		if !allowed[task] {
			return
		}
		out = append(out, projectstate.TaskAttempt{
			AttemptID: projectstate.AttemptID(activityID, task, 1),
			Task:      task,
			Phase:     projectstate.PhaseForTask(task),
			Attempt:   1,
			Actor:     projectstate.ActorAgent,
			Outcome:   projectstate.OutcomePassed,
			Evidence:  ref,
			Provenance: projectstate.AttemptProvenance{
				Origin:      projectstate.OriginBackfilled,
				Generator:   generatorID,
				GeneratedAt: &now,
				Basis:       basis,
			},
		})
	}

	if ev.HasServiceContract {
		ref := projectstate.EvidenceRef{Kind: projectstate.EvidenceContract, Ref: ev.ContractRef}
		basis := ev.ContractBasis
		if basis == "" {
			basis = fmt.Sprintf("serviceContracts[%s]", ev.ContractRef)
		}
		add(projectstate.TaskDetailedDesign, ref, basis)
		add(projectstate.TaskDesignReview, ref, basis)
	}
	if ev.HasMergedCode {
		kind := ev.CodeKind
		if kind == projectstate.EvidenceNone {
			kind = projectstate.EvidenceGit
		}
		ref := projectstate.EvidenceRef{Kind: kind, Ref: ev.GitRef}
		basis := ev.CodeBasis
		if basis == "" {
			basis = fmt.Sprintf("activityGit[%s]", ev.GitRef)
		}
		add(projectstate.TaskConstruction, ref, basis)
		add(projectstate.TaskCodeReview, ref, basis)
	}
	return out
}

// evidenceFromRow reads one activity's produced-artifact list. contracts is the set of
// live .serviceContracts keys, used only to decide whether a contract basis can point
// at a real entry.
func evidenceFromRow(row projectstate.ActivityConstructionStatus, contracts map[string]bool) evidence {
	var ev evidence
	for _, artifact := range row.Produced {
		if !artifact.Produced {
			continue
		}
		switch artifact.Kind {
		case "service-contract":
			ev.HasServiceContract = true
			ev.ContractRef = artifact.Source
			ev.ContractBasis = producedBasis(row.ActivityID, artifact)
			if component := contractComponent(artifact.Source); component != "" && contracts[component] {
				ev.ContractRef = component
				ev.ContractBasis = "" // serviceContracts[<component>] resolves.
			}
		case "code":
			ev.HasMergedCode = true
			ev.GitRef = artifact.Source
			ev.CodeKind = projectstate.EvidenceArtifact
			ev.CodeBasis = producedBasis(row.ActivityID, artifact)
		}
	}
	return ev
}

// contractComponent recovers the component name a contract file is named for. An empty
// source yields an empty name rather than path.Base's "." — a produced entry that names
// no file cannot be matched to a live .serviceContracts key, and "." would be a
// reference to nothing dressed up as one.
func contractComponent(source string) string {
	if source == "" {
		return ""
	}
	return strings.TrimSuffix(path.Base(source), ".md")
}

// producedBasis names the produced entry an inference was read from. When the entry
// carries no source the basis stops at the entry itself: it is still true, still
// non-empty, and still traceable to a specific record — it just cannot claim a path
// that the corpus does not contain.
func producedBasis(activityID string, artifact projectstate.ProducedArtifact) string {
	basis := fmt.Sprintf("activityConstruction[%s].produced[%s]", activityID, artifact.Kind)
	if artifact.Source == "" {
		return basis
	}
	return basis + "=" + artifact.Source
}

// activityMetaByID reads the committed Phase-2 activity list (worker class + coding
// flag), the two signals ClassifyType needs for an activity that produced no contract.
// A missing or unreadable slot yields an empty map: every such row is then
// unclassifiable and gets no attempts, which is the correct conservative answer.
func activityMetaByID(doc map[string]json.RawMessage) map[string]projectstate.ActivityItem {
	out := map[string]projectstate.ActivityItem{}
	var slots map[string]struct {
		Model struct {
			Activities []projectstate.ActivityItem `json:"activities"`
		} `json:"model"`
	}
	if err := json.Unmarshal(doc["slots"], &slots); err != nil {
		return out
	}
	slot, ok := slots[strconv.Itoa(int(projectstate.KindActivityList))]
	if !ok {
		return out
	}
	for _, item := range slot.Model.Activities {
		out[item.Name] = item
	}
	return out
}

// serviceContractKeys reads the set of live .serviceContracts component keys.
func serviceContractKeys(doc map[string]json.RawMessage) map[string]bool {
	out := map[string]bool{}
	var contracts map[string]json.RawMessage
	if err := json.Unmarshal(doc["serviceContracts"], &contracts); err != nil {
		return out
	}
	for key := range contracts {
		out[key] = true
	}
	return out
}

// valueSpan returns the byte range of a top-level key's VALUE in the raw document.
// Rewriting one span and leaving every other byte untouched is what keeps the diff
// reviewable: top-level key order, 2-space indentation and every unrelated key survive
// verbatim, which a whole-document round-trip through Go's map marshalling would not.
func valueSpan(raw []byte, want string) (start, end int, err error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil { // the opening '{'
		return 0, 0, fmt.Errorf("read document: %w", err)
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return 0, 0, fmt.Errorf("read key: %w", err)
		}
		key, _ := keyTok.(string)
		afterKey := dec.InputOffset()
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return 0, 0, fmt.Errorf("read value for %q: %w", key, err)
		}
		afterValue := dec.InputOffset()
		if key != want {
			continue
		}
		open := bytes.IndexByte(raw[afterKey:afterValue], '{')
		if open < 0 {
			return 0, 0, fmt.Errorf("value for %q is not an object", want)
		}
		return int(afterKey) + open, int(afterValue), nil
	}
	return 0, 0, fmt.Errorf("key %q not found", want)
}

// kv is one key/value pair of a JSON object, in document order.
type kv struct {
	key   string
	value json.RawMessage
}

// objectPairs decodes a JSON object into its ordered key/value pairs. Order is carried
// explicitly because a Go map has none: the committed .activityConstruction keys are
// not alphabetical, and a handful of records do not carry their fields in struct order
// either. Both orders are preserved so the backfill diff shows only the backfill.
func objectPairs(raw json.RawMessage) ([]kv, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil { // the opening '{'
		return nil, err
	}
	var out []kv
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := keyTok.(string)
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, fmt.Errorf("read value for %q: %w", key, err)
		}
		out = append(out, kv{key: key, value: value})
	}
	return out, nil
}

// compactObject re-emits ordered pairs as a compact object. Callers hand the result to
// renderRows, which re-indents it; the two steps together are what let a record keep
// its own field order through a rewrite.
func compactObject(pairs []kv) (json.RawMessage, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, p := range pairs {
		if i > 0 {
			b.WriteByte(',')
		}
		key, err := json.Marshal(p.key)
		if err != nil {
			return nil, err
		}
		b.Write(key)
		b.WriteByte(':')
		b.Write(p.value)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// withAttempts returns the record with an "attempts" key carrying the derived ledger,
// positioned where ActivityConstructionStatus declares it (immediately after "phases",
// or after "phase" when the record has no phase set) so the diff reads in place.
//
// A record that already carries an attempts key has that key REPLACED, not shadowed by
// a second one: re-running the tool must produce the same document shape as running it
// once. A duplicate key would parse (Go keeps the last) while making the committed
// state ambiguous to every other reader of this file, which is a worse failure than the
// one it papers over.
func withAttempts(record json.RawMessage, attempts []projectstate.TaskAttempt) (json.RawMessage, error) {
	pairs, err := objectPairs(record)
	if err != nil {
		return nil, err
	}
	value, err := json.Marshal(attempts)
	if err != nil {
		return nil, err
	}
	for i, p := range pairs {
		if p.key == "attempts" {
			pairs[i].value = value
			return compactObject(pairs)
		}
	}
	at := len(pairs)
	for i, p := range pairs {
		if p.key == "phases" {
			at = i + 1
			break
		}
		if p.key == "phase" {
			at = i + 1
		}
	}
	merged := make([]kv, 0, len(pairs)+1)
	merged = append(merged, pairs[:at]...)
	merged = append(merged, kv{key: "attempts", value: value})
	merged = append(merged, pairs[at:]...)
	return compactObject(merged)
}

// decodeRows decodes the .activityConstruction object into its ordered activity ids and
// the raw bytes of each record.
func decodeRows(raw []byte) ([]string, map[string]json.RawMessage, error) {
	pairs, err := objectPairs(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("decode activityConstruction: %w", err)
	}
	order := make([]string, 0, len(pairs))
	rows := make(map[string]json.RawMessage, len(pairs))
	for _, p := range pairs {
		order = append(order, p.key)
		rows[p.key] = p.value
	}
	return order, rows, nil
}

// renderRows re-emits the .activityConstruction object at its original depth (one level
// in, 2-space indentation) and in its original key order.
func renderRows(order []string, rows map[string]json.RawMessage) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("{\n")
	for i, id := range order {
		key, err := json.Marshal(id)
		if err != nil {
			return nil, err
		}
		body, err := json.MarshalIndent(rows[id], "    ", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal %s: %w", id, err)
		}
		b.WriteString("    ")
		b.Write(key)
		b.WriteString(": ")
		b.Write(body)
		if i < len(order)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString("  }")
	return b.Bytes(), nil
}

// derived is one activity's outcome, for the run report.
type derived struct {
	activityID string
	attempts   int
	reason     string
}

func run(repo string, dryRun bool) error {
	file := filepath.Join(repo, statePath)
	raw, err := os.ReadFile(file) //nolint:gosec // a one-shot CLI reading the path it was told to read.
	if err != nil {
		return fmt.Errorf("read %s: %w", file, err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("parse %s: %w", file, err)
	}

	start, end, err := valueSpan(raw, "activityConstruction")
	if err != nil {
		return err
	}
	order, rows, err := decodeRows(raw[start:end])
	if err != nil {
		return err
	}

	// Fidelity gate: re-rendering the UNTOUCHED rows must reproduce the committed bytes
	// exactly. If it does not, this tool would smuggle a reformat into the same diff as
	// the backfill, and a reviewer could no longer see what it actually changed.
	roundTrip, err := renderRows(order, rows)
	if err != nil {
		return err
	}
	if !bytes.Equal(roundTrip, raw[start:end]) {
		return fmt.Errorf("re-rendering activityConstruction unchanged is not byte-identical; " +
			"writing would reformat unrelated state — refusing")
	}

	contracts := serviceContractKeys(doc)
	meta := activityMetaByID(doc)

	report := make([]derived, 0, len(order))
	total := 0
	for _, id := range order {
		var row projectstate.ActivityConstructionStatus
		if err := json.Unmarshal(rows[id], &row); err != nil {
			return fmt.Errorf("decode activityConstruction[%s]: %w", id, err)
		}
		ev := evidenceFromRow(row, contracts)
		if !ev.HasServiceContract && !ev.HasMergedCode {
			report = append(report, derived{id, 0, "no contract and no code artifact"})
			continue
		}
		item := meta[id]
		typ, ok := projectstate.ClassifyType(row.ActivityID, item.WorkerClass, item.Coding, ev.HasServiceContract)
		if !ok {
			// An unclassifiable activity has no profile, so it has no task vocabulary,
			// so there is nothing honest to write against it.
			report = append(report, derived{id, 0, "unclassifiable: ClassifyType refused"})
			continue
		}
		attempts := attemptsFor(row.ActivityID, typ, ev)
		for _, attempt := range attempts {
			if err := attempt.Provenance.Validate(); err != nil {
				return fmt.Errorf("%s: %w", attempt.AttemptID, err)
			}
			if attempt.Provenance.Origin != projectstate.OriginBackfilled {
				return fmt.Errorf("%s: origin %q — this tool observes nothing",
					attempt.AttemptID, attempt.Provenance.Origin)
			}
		}
		if len(attempts) == 0 {
			report = append(report, derived{id, 0, "profile has no phase for the available evidence"})
			continue
		}
		updated, err := withAttempts(rows[id], attempts)
		if err != nil {
			return fmt.Errorf("activityConstruction[%s]: %w", id, err)
		}
		rows[id] = updated
		total += len(attempts)
		report = append(report, derived{id, len(attempts), evidenceSummary(ev)})
	}

	printReport(report, total, dryRun)
	if dryRun {
		return nil
	}

	rendered, err := renderRows(order, rows)
	if err != nil {
		return err
	}
	var out bytes.Buffer
	out.Write(raw[:start])
	out.Write(rendered)
	out.Write(raw[end:])
	if err := os.WriteFile(file, out.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", file, err)
	}
	fmt.Printf("wrote %s\n", file)
	return nil
}

func evidenceSummary(ev evidence) string {
	var parts []string
	if ev.HasServiceContract {
		parts = append(parts, "contract="+ev.ContractRef)
	}
	if ev.HasMergedCode {
		parts = append(parts, "code="+ev.GitRef)
	}
	return strings.Join(parts, " ")
}

func printReport(report []derived, total int, dryRun bool) {
	mode := "write"
	if dryRun {
		mode = "dry-run"
	}
	withAttempts := make([]derived, 0, len(report))
	reasons := map[string]int{}
	for _, r := range report {
		if r.attempts > 0 {
			withAttempts = append(withAttempts, r)
			continue
		}
		reasons[r.reason]++
	}
	fmt.Printf("backfill-attempts (%s)\n", mode)
	for _, r := range withAttempts {
		fmt.Printf("  %-28s %d attempts  (%s)\n", r.activityID, r.attempts, r.reason)
	}
	fmt.Printf("\n  %d activities with attempts, %d with none, %d attempts total\n",
		len(withAttempts), len(report)-len(withAttempts), total)
	keys := make([]string, 0, len(reasons))
	for k := range reasons {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  none: %-46s %d\n", k, reasons[k])
	}
}

func main() {
	repo := flag.String("repo", "..", "path to the repository root containing .aiarch/state/project.json")
	dryRun := flag.Bool("dry-run", false, "report what would be written without writing")
	flag.Parse()

	if err := run(*repo, *dryRun); err != nil {
		fmt.Fprintf(os.Stderr, "backfill-attempts: %v\n", err)
		os.Exit(1)
	}
}
