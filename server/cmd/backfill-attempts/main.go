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
// Every attempt it writes is stamped OriginBackfilled with a basis naming what it was
// derived from, and every attempt is run through AttemptProvenance.Validate before
// anything is written — a backfilled record with an empty basis is a hard error, not a
// silent nil on the wire. Activities with NO evidence get NO attempts: absence stays
// absence. The list view renders those as an honest unknown skeleton, which is the point.
//
// One inference is NOT read off a file. Where an activity has BOTH a frozen contract and
// merged code, the founder has ruled that such a component is done, reviewed and
// integrated — ground truth about their own project that this tool cannot derive. Those
// rows derive their whole profile, and their basis says so, naming the ruling alongside
// the two artifacts rather than pretending the extra tasks were read off disk. See
// attemptsFor.
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

// founderRuling is the ruling that widens the both-artifacts case, quoted verbatim so
// the sentence a reader finds in a committed provenance basis is the sentence the
// founder actually said — not a paraphrase this tool invented.
const founderRuling = "assume any component that is fully implemented is done and reviewed and integrated"

// founderRulingRef is how that ruling is cited inside a basis string.
var founderRulingRef = "founderRuling[2026-09-09]=" + founderRuling

// contractRef / contractBasis / codeRef / codeBasis resolve one evidence kind into the
// EvidenceRef the UI clicks through and the basis string that names where it was read
// from. Both are shared by all three inferences, so a basis can never drift between the
// narrow and the widened form of the same evidence.
func contractRef(ev evidence) projectstate.EvidenceRef {
	return projectstate.EvidenceRef{Kind: projectstate.EvidenceContract, Ref: ev.ContractRef}
}

func contractBasis(ev evidence) string {
	if ev.ContractBasis != "" {
		return ev.ContractBasis
	}
	return fmt.Sprintf("serviceContracts[%s]", ev.ContractRef)
}

func codeRef(ev evidence) projectstate.EvidenceRef {
	kind := ev.CodeKind
	if kind == projectstate.EvidenceNone {
		kind = projectstate.EvidenceGit
	}
	return projectstate.EvidenceRef{Kind: kind, Ref: ev.GitRef}
}

func codeBasis(ev evidence) string {
	if ev.CodeBasis != "" {
		return ev.CodeBasis
	}
	return fmt.Sprintf("activityGit[%s]", ev.GitRef)
}

// fullyImplementedBasis is the provenance basis for the widened inference, and it is
// deliberately NOT just a list of artifacts.
//
// Most of the tasks this basis stamps have no artifact behind them at all — srs, stp and
// testing were never produced as files, and no committed record says they ran. What says
// they ran is the FOUNDER, asserting ground truth about their own project that this tool
// cannot derive. A basis citing only serviceContracts[…] and produced[code] would claim
// those rows were read off disk, which is precisely the class of false provenance this
// tool exists to prevent — and a claim this codebase has already had to go back and fix
// once. So the basis names both halves: the two artifacts that establish "fully
// implemented", and the ruling that turns "fully implemented" into "done, reviewed and
// integrated".
func fullyImplementedBasis(ev evidence) string {
	return contractBasis(ev) + " + " + codeBasis(ev) + " + " + founderRulingRef
}

// ruledEvidenceFor points a widened attempt at the artifact that actually backs it, and
// at NOTHING when none does. Detailed design and its review were read off the frozen
// contract; construction and its review off the merged code. The remaining tasks —
// srs, srsReview, stp, stpReview, integration, testing — exist because of the ruling,
// not because of a file, so they carry no evidence ref: handing the UI a contract to
// open under a row labelled "Testing" would be a click-through that lies about what it
// is showing. The basis still says exactly where each row came from.
//
// Every one of the twelve tasks is listed, with no default case, so `exhaustive` fails
// the build the moment a thirteenth is added without a conscious call about what backs
// it — the same discipline projectstate's conditionalTasks and taskLabels maps keep.
func ruledEvidenceFor(task projectstate.MethodTask, ev evidence) projectstate.EvidenceRef {
	switch task {
	case projectstate.TaskDetailedDesign, projectstate.TaskDesignReview:
		return contractRef(ev)
	case projectstate.TaskConstruction, projectstate.TaskCodeReview:
		return codeRef(ev)
	case projectstate.TaskSRS, projectstate.TaskSRSReview,
		projectstate.TaskSTP, projectstate.TaskSTPReview,
		projectstate.TaskIntegration, projectstate.TaskTesting:
		// The ruling is the evidence; there is no artifact to point at.
		return projectstate.EvidenceRef{}
	case projectstate.TaskSomeConstruction, projectstate.TaskTestClient:
		// Conditional-emit; the widened inference never asks about these.
		return projectstate.EvidenceRef{}
	}
	return projectstate.EvidenceRef{}
}

// attemptsFor derives the recoverable attempts for one activity.
//
// THREE inferences, and no fourth. The first two are read off artifacts; the third is a
// founder ruling applied to a pair of artifacts:
//
//   - a frozen service contract ALONE means Detailed Design ran and Design Review passed.
//     A contract with no code is a design that was never built, and it stays two tasks.
//   - merged code ALONE means Construction ran and Code Review passed.
//   - a frozen contract AND merged code means the component is FULLY IMPLEMENTED, and
//     the founder has ruled that such a component is "done and reviewed and integrated".
//     Those activities derive their WHOLE profile as passed.
//
// The widened case skips the CONDITIONAL tasks (someConstruction, testClient). Those are
// conditional-emit by design — the UI renders them only when a real attempt exists — and
// inventing one would assert a pre-design spike or a test client that may never have
// existed. The ruling says the component is done, reviewed and integrated; it does not
// say how it got there, and this tool must not fill that in.
//
// Activities with NO evidence still get NOTHING. The ruling is about fully implemented
// components specifically; stretching it further would be exactly the over-inference
// this tool was built to avoid.
//
// The task vocabulary comes from the PROFILE, never from the evidence: an inference is
// dropped when the activity's type has no lifecycle stage for it.
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

	switch {
	case ev.HasServiceContract && ev.HasMergedCode:
		basis := fullyImplementedBasis(ev)
		for _, task := range projectstate.TasksForProfile(profile) {
			if projectstate.IsConditionalTask(task) {
				continue
			}
			add(task, ruledEvidenceFor(task, ev), basis)
		}
	case ev.HasServiceContract:
		add(projectstate.TaskDetailedDesign, contractRef(ev), contractBasis(ev))
		add(projectstate.TaskDesignReview, contractRef(ev), contractBasis(ev))
	case ev.HasMergedCode:
		add(projectstate.TaskConstruction, codeRef(ev), codeBasis(ev))
		add(projectstate.TaskCodeReview, codeRef(ev), codeBasis(ev))
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

// activityConstructionDoc bundles the raw project.json bytes together with the
// decoded .activityConstruction rows this tool rewrites. Read/parse and
// render/write each become one function operating on this value, instead of
// run() threading raw/doc/start/end/order/rows through by hand.
type activityConstructionDoc struct {
	raw   []byte
	doc   map[string]json.RawMessage
	start int
	end   int
	order []string
	rows  map[string]json.RawMessage
}

// readActivityConstructionDoc loads project.json and isolates the
// .activityConstruction object as both raw bytes and decoded rows.
//
// Fidelity gate: re-rendering the UNTOUCHED rows must reproduce the committed bytes
// exactly. If it does not, this tool would smuggle a reformat into the same diff as
// the backfill, and a reviewer could no longer see what it actually changed.
func readActivityConstructionDoc(file string) (activityConstructionDoc, error) {
	raw, err := os.ReadFile(file) //nolint:gosec // a one-shot CLI reading the path it was told to read.
	if err != nil {
		return activityConstructionDoc{}, fmt.Errorf("read %s: %w", file, err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return activityConstructionDoc{}, fmt.Errorf("parse %s: %w", file, err)
	}

	start, end, err := valueSpan(raw, "activityConstruction")
	if err != nil {
		return activityConstructionDoc{}, err
	}
	order, rows, err := decodeRows(raw[start:end])
	if err != nil {
		return activityConstructionDoc{}, err
	}

	roundTrip, err := renderRows(order, rows)
	if err != nil {
		return activityConstructionDoc{}, err
	}
	if !bytes.Equal(roundTrip, raw[start:end]) {
		return activityConstructionDoc{}, fmt.Errorf("re-rendering activityConstruction unchanged is not byte-identical; " +
			"writing would reformat unrelated state — refusing")
	}
	return activityConstructionDoc{raw: raw, doc: doc, start: start, end: end, order: order, rows: rows}, nil
}

// write re-renders the (possibly updated) rows and splices them back into the
// original document bytes at [start:end], leaving everything outside
// .activityConstruction byte-for-byte untouched.
func (d activityConstructionDoc) write(file string) error {
	rendered, err := renderRows(d.order, d.rows)
	if err != nil {
		return err
	}
	var out bytes.Buffer
	out.Write(d.raw[:d.start])
	out.Write(rendered)
	out.Write(d.raw[d.end:])
	if err := os.WriteFile(file, out.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", file, err)
	}
	fmt.Printf("wrote %s\n", file)
	return nil
}

// validateAttempts enforces the two invariants every backfilled attempt must satisfy
// before it is written: Validate() must pass (a backfilled record with an empty basis
// is a hard error, not a silent nil on the wire), and the origin must be
// OriginBackfilled — this tool observes evidence, it never fabricates a live one.
func validateAttempts(attempts []projectstate.TaskAttempt) error {
	for _, attempt := range attempts {
		if err := attempt.Provenance.Validate(); err != nil {
			return fmt.Errorf("%s: %w", attempt.AttemptID, err)
		}
		if attempt.Provenance.Origin != projectstate.OriginBackfilled {
			return fmt.Errorf("%s: origin %q — this tool observes nothing",
				attempt.AttemptID, attempt.Provenance.Origin)
		}
	}
	return nil
}

// deriveRowAttempts resolves one activityConstruction row into the TaskAttempt
// records its evidence supports. An activity with no evidence, an unclassifiable
// activity/profile combination, or an evidence set that maps to no phase in the
// profile yields zero attempts and an explanatory report line — never an error and
// never a fabricated record. A decode failure or a failed attempt validation is a
// hard abort, returned as an error instead of a report line.
func deriveRowAttempts(id string, rawRow json.RawMessage, item projectstate.ActivityItem, contracts map[string]bool) ([]projectstate.TaskAttempt, derived, error) {
	var row projectstate.ActivityConstructionStatus
	if err := json.Unmarshal(rawRow, &row); err != nil {
		return nil, derived{}, fmt.Errorf("decode activityConstruction[%s]: %w", id, err)
	}
	ev := evidenceFromRow(row, contracts)
	if !ev.HasServiceContract && !ev.HasMergedCode {
		return nil, derived{id, 0, "no contract and no code artifact"}, nil
	}
	typ, ok := projectstate.ClassifyType(row.ActivityID, item.WorkerClass, item.Coding, ev.HasServiceContract)
	if !ok {
		// An unclassifiable activity has no profile, so it has no task vocabulary,
		// so there is nothing honest to write against it.
		return nil, derived{id, 0, "unclassifiable: ClassifyType refused"}, nil
	}
	attempts := attemptsFor(row.ActivityID, typ, ev)
	if err := validateAttempts(attempts); err != nil {
		return nil, derived{}, err
	}
	if len(attempts) == 0 {
		return nil, derived{id, 0, "profile has no phase for the available evidence"}, nil
	}
	return attempts, derived{id, len(attempts), evidenceSummary(ev)}, nil
}

func run(repo string, dryRun bool) error {
	file := filepath.Join(repo, statePath)
	acd, err := readActivityConstructionDoc(file)
	if err != nil {
		return err
	}

	contracts := serviceContractKeys(acd.doc)
	meta := activityMetaByID(acd.doc)

	report := make([]derived, 0, len(acd.order))
	total := 0
	for _, id := range acd.order {
		attempts, note, err := deriveRowAttempts(id, acd.rows[id], meta[id], contracts)
		if err != nil {
			return err
		}
		report = append(report, note)
		if len(attempts) == 0 {
			continue
		}
		updated, err := withAttempts(acd.rows[id], attempts)
		if err != nil {
			return fmt.Errorf("activityConstruction[%s]: %w", id, err)
		}
		acd.rows[id] = updated
		total += len(attempts)
	}

	printReport(report, total, dryRun)
	if dryRun {
		return nil
	}
	return acd.write(file)
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
