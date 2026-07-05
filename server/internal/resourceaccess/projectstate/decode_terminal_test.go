package projectstate_test

import (
	"errors"
	"strings"
	"testing"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	ps "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// decode_terminal_test.go — QA F36 regression. A committed slot model that will not decode
// (free prose in a CLOSED-ENUM field — the live incident was a sentence written into a
// use case's "trigger", a closed Trigger enum) must be classified TERMINAL (ContractMisuse,
// non-retryable) by the shared project.json codec, NOT Infrastructure (retryable). Pre-fix
// it was Infrastructure, so the Manager's read-back Activity retried the same immutable
// bytes every ~100s forever with no failure surface.

// A valid CoreUseCases document round-trips; the same document with its "trigger" wire name
// overwritten by free prose fails decode as a TERMINAL ContractMisuse carrying the decode
// diagnostic — exactly what the Manager read-back needs to route to the human failure gate.
func TestDecodeProjectJSON_MalformedClosedEnum_IsTerminal(t *testing.T) {
	id := ps.ProjectID("11111111-1111-1111-1111-111111111111")

	// A minimal but VALID CoreUseCases slot model — the trigger is the closed-enum wire name
	// "busMessage", so it appears verbatim in the encoded JSON for the surgical overwrite.
	cuc := &ps.CoreUseCases{Decisions: []ps.UseCaseDecision{{
		UseCase: ps.UseCase{
			Name:           "Capture a commitment",
			Trigger:        ps.TriggerBusMessage,
			Classification: ps.ClassCore,
		},
		RejectionReason: "",
	}}}
	state := ps.Project{ID: id}
	state.CoreUseCases = ps.ArtifactSlot{Status: ps.ReviewCommitted, Model: cuc}

	raw, err := ps.EncodeProjectJSON(state)
	if err != nil {
		t.Fatalf("EncodeProjectJSON: %v", err)
	}

	// Sanity: the valid document decodes cleanly.
	if _, _, err := ps.DecodeProjectJSON(raw, id); err != nil {
		t.Fatalf("valid document should decode: %v", err)
	}

	// Reproduce F36: overwrite the closed-enum wire name with the exact free prose the live
	// drafting agent committed. CI validate (a Go mirror typing trigger as a free string)
	// accepts this; the server codec must reject it.
	const prose = "A commitment of any size appears, however it arrives, and is still held only in the person's memory."
	poisoned := strings.Replace(string(raw), `"busMessage"`, `"`+prose+`"`, 1)
	if poisoned == string(raw) {
		t.Fatalf("test fixture invalid: %q not found in encoded document", "busMessage")
	}

	_, _, derr := ps.DecodeProjectJSON([]byte(poisoned), id)
	if derr == nil {
		t.Fatalf("malformed closed-enum document must FAIL decode; got nil error")
	}
	if k := kindOf(t, derr); k != fwra.ContractMisuse {
		t.Fatalf("decode error kind = %v, want ContractMisuse (terminal); Infrastructure would retry forever (F36)", k)
	}
	// Terminal = non-retryable: this is what lets the Manager read-back retry policy stop.
	var e *fwra.Error
	_ = errors.As(derr, &e)
	if e.Retryable {
		t.Fatalf("decode error must be NON-retryable; got Retryable=true (F36 loop-forever bug)")
	}
	// The decode diagnostic (the wire-name rejection) must survive so it can be shown at the
	// human StageDraftFailed gate as the failureReason.
	if !strings.Contains(derr.Error(), "is not a recognized Trigger wire name") {
		t.Errorf("decode error must carry the wire-name diagnostic; got: %v", derr)
	}
}
