package main

// state.go holds the STATE-MUTATING verbs' cores: putDraftModel (the F36/F66 killer:
// validate a drafted model through the FULL projectstate codec + methodcheck for the
// ambient kind, and reject with typed, actionable errors so the agent self-corrects),
// respondToReviewComment, and setCritiqueVerdict. Each operates on the checked-out
// project.json through the strict server codec — a draft that survives putDraftModel is
// byte-for-byte what the server accepts on read-back.

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// putDraftModel validates the agent-supplied model for the AMBIENT kind and, only if it
// passes BOTH gates the server applies, writes it into the ambient slot on the checkout:
//
//  1. STRICT CODEC (server read-back parity): the model JSON is unmarshalled into the
//     concrete typed model — the SAME decode the server's decodeSlotsMap runs — so a
//     closed-enum field carrying free prose (F36) or a type mismatch is rejected HERE
//     with the exact same strictness as the server read-back, not accepted-then-stalled.
//  2. METHODCHECK (the required CI gate): the re-encoded whole-project document is run
//     through methodcheck.ValidateProjectJSON — the identical Method-invariant rules the
//     seated `go test` gate enforces (empty ids, missing activity diagrams, layering /
//     cardinality). Only SeverityError findings fail, matching the CI verdict exactly.
//
// On any failure NOTHING is written and a typed, human-actionable error is returned (the
// MCP SDK surfaces it as an IsError tool result the agent reads and corrects). On success
// the ambient slot is written with the validated model at status Committed (the status the
// CI methodcheck gate requires to validate a slot, and the status the server read-back
// reads the model from), preserving the slot's existing notes / review thread / revisions.
func (s *Session) putDraftModel(modelJSON []byte) error {
	if len(strings.TrimSpace(string(modelJSON))) == 0 {
		return fmt.Errorf("putDraftModel requires a non-empty %q model object", s.Kind.WireName())
	}

	proj, _, err := s.readProject()
	if err != nil {
		return err
	}
	slot, ok := slotFor(&proj, s.Kind)
	if !ok {
		return fmt.Errorf("no slot for artifact kind %s", s.Kind)
	}

	// GATE 1 — strict codec decode of the model into its concrete type (server parity).
	model, ok := projectstate.NewModelForKind(s.Kind)
	if !ok {
		return fmt.Errorf("no model type for artifact kind %s", s.Kind)
	}
	if err := json.Unmarshal(modelJSON, model); err != nil {
		return fmt.Errorf("the %s model does not conform to its schema (the server would reject this on read-back): %v"+
			" — a common cause is a closed-enum field carrying a free-text sentence instead of an exact wire value; fix the field and call putDraftModel again",
			s.Kind.WireName(), err)
	}

	// Stage the validated model into the ambient slot at Committed (the status the CI
	// methodcheck gate validates a slot at, and the status the server read-back reads the
	// model from). Existing notes / thread / revisions on the slot are preserved.
	slot.Model = model
	slot.Status = projectstate.ReviewCommitted

	// Re-encode the whole aggregate through the SAME codec the server commits with, so the
	// bytes we validate (and write) are byte-identical to what the server would produce.
	newBytes, err := projectstate.EncodeProjectJSON(proj)
	if err != nil {
		return fmt.Errorf("encode project state: %w", err)
	}

	// GATE 1 (parity re-decode): confirm the full document round-trips through the strict
	// server codec. This catches any cross-slot decode fault the single-model decode above
	// could miss and is exactly the read-back the server performs.
	if _, _, derr := projectstate.DecodeProjectJSON(newBytes, s.ProjectID); derr != nil {
		return fmt.Errorf("the drafted project state would be rejected by the server on read-back: %w", derr)
	}

	// GATE 2 — methodcheck (the required CI gate) over the whole re-encoded document.
	findings, ferr := methodcheck.ValidateProjectJSON(newBytes)
	if ferr != nil {
		return fmt.Errorf("the Method coherence check failed: %w", ferr)
	}
	if errs := filterErrorFindings(findings); len(errs) > 0 {
		return fmt.Errorf("the %s draft violates %d Method rule(s) that the required CI check enforces — fix them and call putDraftModel again:\n%s",
			s.Kind.WireName(), len(errs), formatFindings(errs))
	}

	// Both gates passed — write the validated draft to the checkout.
	if err := s.writeProjectBytes(newBytes); err != nil {
		return fmt.Errorf("write project state: %w", err)
	}
	s.wroteState = true
	return nil
}

// respondToReviewComment records the drafting agent's per-entry response on an OPEN
// review-ledger comment for the ambient kind (review-ledger response requirement). The
// server's normalizeReviewThread stays authoritative on read-back: a non-empty response
// flips the entry to addressed, an empty one leaves it open. The binary only records the
// response text + a proposed addressed status; it never invents, reorders, or deletes
// entries.
func (s *Session) respondToReviewComment(id, response string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("respondToReviewComment requires the comment id to respond to")
	}
	proj, _, err := s.readProject()
	if err != nil {
		return err
	}
	slot, ok := slotFor(&proj, s.Kind)
	if !ok {
		return fmt.Errorf("no slot for artifact kind %s", s.Kind)
	}
	found := false
	for i := range slot.ReviewThread {
		if slot.ReviewThread[i].ID == id {
			slot.ReviewThread[i].Response = response
			// Propose addressed when a non-empty response is given; the server reconciles
			// the effective status authoritatively on read-back.
			if strings.TrimSpace(response) != "" {
				slot.ReviewThread[i].Status = projectstate.ReviewCommentAddressed
			}
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("no review comment with id %q on the %s review thread", id, s.Kind.WireName())
	}
	newBytes, err := projectstate.EncodeProjectJSON(proj)
	if err != nil {
		return fmt.Errorf("encode project state: %w", err)
	}
	if err := s.writeProjectBytes(newBytes); err != nil {
		return fmt.Errorf("write project state: %w", err)
	}
	s.wroteState = true
	return nil
}

// setCritiqueVerdict records the PM-critique read-back carrier on the ambient slot
// (critique mode only). It writes critiqueVerdict (exactly approve|revise) and, on
// revise, critiqueNotes — the first-class carrier the systemDesignManager reads back. It
// never rewrites the model and never touches the slot's architect notes.
func (s *Session) setCritiqueVerdict(verdict, notes string) error {
	verdict = strings.TrimSpace(verdict)
	switch verdict {
	case projectstate.CritiqueVerdictApprove, projectstate.CritiqueVerdictRevise:
	default:
		return fmt.Errorf("critique verdict must be exactly %q or %q, got %q",
			projectstate.CritiqueVerdictApprove, projectstate.CritiqueVerdictRevise, verdict)
	}
	if verdict == projectstate.CritiqueVerdictRevise && strings.TrimSpace(notes) == "" {
		return fmt.Errorf("a %q verdict requires notes describing the concrete revision the architect should make", projectstate.CritiqueVerdictRevise)
	}
	proj, _, err := s.readProject()
	if err != nil {
		return err
	}
	slot, ok := slotFor(&proj, s.Kind)
	if !ok {
		return fmt.Errorf("no slot for artifact kind %s", s.Kind)
	}
	slot.CritiqueVerdict = verdict
	if verdict == projectstate.CritiqueVerdictApprove {
		slot.CritiqueNotes = ""
	} else {
		slot.CritiqueNotes = notes
	}
	newBytes, err := projectstate.EncodeProjectJSON(proj)
	if err != nil {
		return fmt.Errorf("encode project state: %w", err)
	}
	if err := s.writeProjectBytes(newBytes); err != nil {
		return fmt.Errorf("write project state: %w", err)
	}
	s.wroteState = true
	return nil
}

// filterErrorFindings keeps only SeverityError findings — the ones that fail the CI
// verdict (methodcheck: "Only SeverityError fails the verdict"). Info/Warning findings
// are advisory and must NOT block a draft, exactly as the CI gate treats them.
func filterErrorFindings(findings []methodcheck.Finding) []methodcheck.Finding {
	var out []methodcheck.Finding
	for _, f := range findings {
		if f.Severity == methodcheck.SeverityError {
			out = append(out, f)
		}
	}
	return out
}

// formatFindings renders methodcheck findings as a readable, actionable bullet list the
// agent can act on directly (rule id + locus + message).
func formatFindings(findings []methodcheck.Finding) string {
	var b strings.Builder
	for _, f := range findings {
		loc := ""
		if f.Location != nil && strings.TrimSpace(f.Location.Section) != "" {
			loc = " (" + f.Location.Section + ")"
		}
		fmt.Fprintf(&b, "  - [%s]%s %s\n", f.RuleID, loc, f.Message)
	}
	return strings.TrimRight(b.String(), "\n")
}
