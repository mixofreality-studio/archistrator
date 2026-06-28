package systemdesign

// findings.go owns the SESSION-TRANSIENT validation-finding value types this Manager
// surfaces on its getSessionState read (SessionStateView.Findings). The SPA renders
// findings[] to explain "why it's being redrafted" (the PM-critique-unresolved
// warning is one). They are part of this component's OWN generated contract surface
// (registered in cmd/schemagen) — pure data, no methods.
//
// WIRE: severity is a camelCase STRING name ("info"|"warning"|"error"). It is now a
// STRING enum (was an int enum with a custom MarshalJSON) so the generated type is
// pure data AND the wire form is byte-identical — f.severity === 'error' / 'warning'
// in the SPA decodes unchanged.

// Severity is a finding severity. Only SeverityError fails a verdict; Warning/Info
// ride along advisory. The value IS its canonical camelCase wire name.

// RuleID is the stable, namespaced id of a validation rule. Stable across runs for
// finding-diff and worker-prompt continuity.

// Location locates a finding within a typed model. NO Line field: the input is a
// typed model, not bytes.

// stable position used for deterministic finding ordering
// human-readable locus, e.g. "core use case 3"

// Finding is a single machine-checkable rule violation surfaced to the SPA.

// human-readable; safe to weave into a redraft prompt; no PII
// optional; where in the model the finding sits
