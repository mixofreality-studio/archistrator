package projectstate

import (
	"fmt"
	"time"
)

// RecordOrigin says how a TaskAttempt came to exist. It is REQUIRED on every attempt
// and is never omitempty.
//
// The zero value is OriginSynthesized on purpose. A missing or dropped stamp must fail
// SUSPICIOUS, never blessed — an omitempty field whose zero value meant "observed" is
// exactly how a fabricated row would launder itself through a round-trip.
//
// Three values, not two: some rows genuinely can be reconstructed from real evidence
// (a frozen contract in .serviceContracts, a merged commit). Those are not lies and
// must not be tarred as fakes; they were also not observed.
type RecordOrigin string

// The three origins. OriginSynthesized MUST remain the empty string.
const (
	// OriginSynthesized — fabricated for the UI wave; no event backs it. ZERO VALUE.
	OriginSynthesized RecordOrigin = ""
	// OriginBackfilled — reconstructed from real evidence recorded elsewhere.
	OriginBackfilled RecordOrigin = "backfilled"
	// OriginObserved — written by the running system from a real event.
	OriginObserved RecordOrigin = "observed"
)

// AttemptProvenance carries the reason, not just the flag: what produced this record
// and what it was derived from.
//
// This is deliberately NOT the existing Provenance type in projectstateaccess.go — that
// one answers a different question (who approved a design commit). Do not overload it.
type AttemptProvenance struct {
	// Origin is required; the zero value is OriginSynthesized.
	Origin RecordOrigin `json:"origin"`
	// Generator names the producing tool and sha, e.g. "cmd/backfill-attempts@a1b2c3d".
	Generator string `json:"generator,omitempty"`
	// GeneratedAt is when the record was produced (not when the work happened).
	GeneratedAt *time.Time `json:"generatedAt,omitempty"`
	// Basis names what this was derived from, e.g. "serviceContracts[artifactAccess]".
	// Empty for pure fiction; REQUIRED for OriginBackfilled.
	Basis string `json:"basis,omitempty"`
}

// Validate enforces the closed enum and the backfilled-needs-a-basis rule.
func (p AttemptProvenance) Validate() error {
	switch p.Origin {
	case OriginSynthesized, OriginObserved:
	case OriginBackfilled:
		if p.Basis == "" {
			return fmt.Errorf("provenance: origin %q requires a non-empty basis", p.Origin)
		}
	default:
		return fmt.Errorf("provenance: unknown origin %q", p.Origin)
	}
	return nil
}

// originRank orders origins worst-first for the contagion rule.
func originRank(o RecordOrigin) int {
	switch o {
	case OriginSynthesized:
		return 0
	case OriginBackfilled:
		return 1
	case OriginObserved:
		return 2
	default:
		return 0 // unknown is as bad as synthesized
	}
}

// WorstOrigin implements the contagion rule: a value derived from any synthesized input
// is itself synthesized. PhaseCompletion.Completed, activity progress and project earned
// value all inherit the worst origin among their inputs.
//
// Without this, rows are honestly badged while the header launders a fabricated
// aggregate — the single worst lie available to this work.
//
// No inputs means nothing was derived from anything unknown: OriginObserved.
func WorstOrigin(origins ...RecordOrigin) RecordOrigin {
	worst := OriginObserved
	for _, o := range origins {
		if originRank(o) < originRank(worst) {
			worst = o
		}
	}
	return worst
}
