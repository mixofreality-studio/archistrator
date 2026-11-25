package main

// refusals.go is the typed channel a write refusal takes out of this session. The refusal exists
// nowhere else a supervisor can read: applyConstructionMutation returns it to the agent as a tool
// error, so in RUN-028 the eight UC-OUTCOME-AT-ASKING-SURFACE refusals of every N-ATP attempt reached
// supervision only inside the agent's truncated prose. The venue names a sidecar file in
// AIARCH_REFUSALS_FILE; each refusal is appended to it as one agenticjob.WriteRefused JSON line, its
// findings attributed to the design slot that owns each rule (projectstate.AttributeRule), and the
// venue reads it once the run is over.

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// refusalSeverity is the severity every recorded finding carries: the write gate keeps only Errors.
const refusalSeverity = "error"

// recordRefusal appends the refusal of the write named what, for the findings errs, to the session's
// sidecar. A session with no sidecar records nothing. A sidecar that cannot be written is warned about,
// never fatal: the agent still gets the refusal, and the venue falls back to what it knew before.
func (s *Session) recordRefusal(what string, errs []methodcheck.Finding) {
	if s.RefusalsFile == "" {
		return
	}
	line, err := json.Marshal(writeRefusal(what, errs))
	if err == nil {
		err = appendLine(s.RefusalsFile, append(line, '\n'))
	}
	if err != nil {
		warnf("the write refusal could not be recorded for the venue: %v", err)
	}
}

// writeRefusal is the typed cause of a refused write of the named kind: every Error finding, each with
// the design slot that owns its rule, or "" for a rule no slot owns.
func writeRefusal(what string, errs []methodcheck.Finding) agenticjob.WriteRefused {
	out := agenticjob.WriteRefused{Verb: what, Findings: make([]agenticjob.RefusedFinding, 0, len(errs))}
	for _, f := range errs {
		owner := ""
		if kind, class := projectstate.AttributeRule(string(f.RuleID)); class == projectstate.AttribSlot {
			owner = kind.WireName()
		}
		out.Findings = append(out.Findings, agenticjob.RefusedFinding{
			RuleID: string(f.RuleID), Severity: refusalSeverity, Message: f.Message, OwnerSlot: owner,
		})
	}
	return out
}

// appendLine appends b to the file at path in ONE write, so concurrent sessions never interleave lines.
func appendLine(path string, b []byte) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // the venue-named sidecar
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}
