package projectdesign

// findings.go owns the SESSION-TRANSIENT validation-finding value types this Manager
// surfaces on its getSessionState read (SessionStateView.Findings). They were
// formerly imported from engine/artifactvalidation, but that Engine's validation
// SUITE is no longer run in-process — the Method gate moved to framework-go/methodcheck
// (the seated `go test`), so the Engine was removed. The managers retained only a LIVE
// need for the small Finding VALUE TYPE (the SPA renders findings[] to explain "why
// it's being redrafted"), so the minimal types are RELOCATED here — owned by the
// Manager that surfaces them — rather than keeping a whole dead Engine for a value type.
//
// Defined LOCALLY (mirroring the project.ArtifactStage pattern) because a Manager
// importing another Manager is a sideways edge the layer model forbids
// (TestMethodLayering NoSideways); systemdesign and projectdesign each own their own
// copy. The JSON wire shape (severity as a camelCase string NAME, not an ordinal) is
// preserved byte-for-byte from the former engine type so the SPA's generated client
// decodes findings identically.

import (
	"encoding/json"
	"fmt"
)

// Severity is a finding severity. Only SeverityError fails a verdict; Warning/Info
// ride along advisory. (Relocated from engine/artifactvalidation, unchanged.)
type Severity int

const (
	// SeverityInfo is advisory-only.
	SeverityInfo Severity = iota
	// SeverityWarning is advisory-only.
	SeverityWarning
	// SeverityError fails a verdict.
	SeverityError
)

// severityNames maps each Severity to its canonical camelCase wire name — the single
// source of truth the SPA reads (findings[].severity is a STRING name, not an integer).
var severityNames = map[Severity]string{
	SeverityInfo:    "info",
	SeverityWarning: "warning",
	SeverityError:   "error",
}

var severityByName = func() map[string]Severity {
	m := make(map[string]Severity, len(severityNames))
	for v, n := range severityNames {
		m[n] = v
	}
	return m
}()

// MarshalJSON encodes the Severity as its camelCase wire name.
func (s Severity) MarshalJSON() ([]byte, error) {
	name, ok := severityNames[s]
	if !ok {
		return nil, fmt.Errorf("projectdesign: Severity(%d) has no wire name", int(s))
	}
	return json.Marshal(name)
}

// UnmarshalJSON decodes a wire name (or, for backward compatibility, a bare integer
// ordinal) into a Severity.
func (s *Severity) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		v, ok := severityByName[name]
		if !ok {
			return fmt.Errorf("projectdesign: %q is not a recognized Severity wire name", name)
		}
		*s = v
		return nil
	}
	var ordinal int
	if err := json.Unmarshal(data, &ordinal); err != nil {
		return fmt.Errorf("projectdesign: Severity must be a string wire name or integer ordinal: %w", err)
	}
	*s = Severity(ordinal)
	return nil
}

// RuleID is the stable, namespaced id of a validation rule. Stable across runs for
// finding-diff and worker-prompt continuity.
type RuleID string

// Location locates a finding within a typed model. NO Line field: the input is a
// typed model, not bytes.
type Location struct {
	Ordinal int    `json:"ordinal"` // stable position used for deterministic finding ordering
	Section string `json:"section"` // human-readable locus, e.g. "Objective 4"
}

// Finding is a single machine-checkable rule violation surfaced to the SPA.
type Finding struct {
	RuleID   RuleID    `json:"ruleId"`
	Severity Severity  `json:"severity"`
	Message  string    `json:"message"`            // human-readable; safe to weave into a redraft prompt; no PII
	Location *Location `json:"location,omitempty"` // optional; where in the model the finding sits
}
