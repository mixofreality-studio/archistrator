package main

// admission.go is the in-loop half of artifact admission (FPC Task 6.3, ruling 6).
//
// Tier 0 (projectmodel.Load and the codec read-back) refuses a document no generator can load.
// Tier 1 runs on every record: the review Engine's AdmitArtifact emits every generator that reads
// the artifact's kind, in process, and typechecks what they wrote against one another, so a
// contract the recorder accepts and a generator cannot build is found by the agent that wrote it,
// in the same call, with every problem at once. A `local` problem refuses the write; an
// `incomplete` one names a part of the design not recorded yet and is carried to the tool result
// as a warning (Tier 2 enforces it). Tier 2 is a venue check at commit (runAdmission): the whole
// generator chain over a scratch module. No record verb runs it.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-conformance"
	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
)

// admissionIncompleteNote leads the warning a tool result carries for an incomplete problem.
const admissionIncompleteNote = "Tier 1 (incomplete, enforced at Tier 2)"

// runChainOn runs the conformance chain over a project document in a scratch module; tests
// substitute it to count calls.
var runChainOn = conformance.RunChainOn

// admitArtifact is the review Engine's Tier 1 op, the one call every verb makes; tests substitute
// it to see which kinds a verb admits.
var admitArtifact = func(project, kind, appgenConfig, goMod string) ([]review.AdmissionProblem, error) {
	return review.NewReviewEngine().AdmitArtifact(fweng.Context{Context: context.Background()}, project, kind, appgenConfig, goMod)
}

// The files of the module Tier 1 composes the candidate with: its generator config and its go.mod,
// which names the module path.
const (
	appgenConfigFile = "appgen.json"
	goModFile        = "go.mod"
)

// moduleFiles reads the appgen.json and go.mod of the module the candidate document describes,
// from the worktree: the generators compose the module with that config and import its packages
// by that module path, so Tier 1 must too (a module whose messageBus constructor returns no error
// is a composition error under the defaults and a correct one under the module's own config).
// A module with no such file is admitted with it empty: the library's defaults, and a module path
// inferred from the document. A document that does not decode has no module to name: the Engine
// reports that.
func (s *Session) moduleFiles(raw []byte) (appgenConfig, goMod string, err error) {
	var doc struct {
		ModuleDir string `json:"moduleDir"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return "", "", nil
	}
	read := func(name string) (string, error) {
		b, err := os.ReadFile(filepath.Join(s.StateRoot, filepath.FromSlash(doc.ModuleDir), name))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return "", nil
		case err != nil:
			return "", fmt.Errorf("read the module's %s: %w", name, err)
		}
		return string(b), nil
	}
	if appgenConfig, err = read(appgenConfigFile); err != nil {
		return "", "", err
	}
	if goMod, err = read(goModFile); err != nil {
		return "", "", err
	}
	return appgenConfig, goMod, nil
}

// admitTier1 holds the candidate project document to Tier 1 for each kind (the kinds no generator
// reads admit nothing). It returns an error listing every local problem, or nil, noting the
// incomplete ones on the session for the tool result.
func (s *Session) admitTier1(what string, raw []byte, kinds ...string) error {
	cfg, goMod, err := s.moduleFiles(raw)
	if err != nil {
		return fmt.Errorf("tier 1 admission of the %s: %w", what, err)
	}
	var local, incomplete []review.AdmissionProblem
	seen := map[review.AdmissionProblem]bool{}
	for _, kind := range kinds {
		problems, err := admitArtifact(string(raw), kind, cfg, goMod)
		if err != nil {
			return fmt.Errorf("tier 1 admission of the %s: %w", what, err)
		}
		for _, p := range problems {
			if seen[p] {
				continue
			}
			seen[p] = true
			switch p.Severity {
			case review.AdmissionLocal:
				local = append(local, p)
			case review.AdmissionIncomplete:
				incomplete = append(incomplete, p)
			}
		}
	}
	if len(local) > 0 {
		return fmt.Errorf("the %s would be refused by Tier 1 admission: the generators that read it cannot build it. Fix every problem below and record again (%d):\n%s",
			what, len(local), formatAdmission(local))
	}
	for _, p := range incomplete {
		s.admissionNotes = append(s.admissionNotes, formatAdmissionProblem(p))
	}
	return nil
}

func formatAdmission(ps []review.AdmissionProblem) string {
	lines := make([]string, len(ps))
	for i, p := range ps {
		lines[i] = "  - " + formatAdmissionProblem(p)
	}
	return strings.Join(lines, "\n")
}

func formatAdmissionProblem(p review.AdmissionProblem) string {
	if p.Path == "" {
		return fmt.Sprintf("%s: %s", p.Consumer, p.Message)
	}
	return fmt.Sprintf("%s %s: %s", p.Consumer, p.Path, p.Message)
}

// withAdmissionNotes appends the warnings the last record left to a verb's success text, and clears them.
func (s *Session) withAdmissionNotes(text string) string {
	if len(s.admissionNotes) == 0 {
		return text
	}
	var b strings.Builder
	b.WriteString(text)
	fmt.Fprintf(&b, "\n%s: the generators could not build all of it yet, because part of the design is not recorded. This is a warning, not a refusal:\n", admissionIncompleteNote)
	for _, n := range s.admissionNotes {
		b.WriteString("  - " + n + "\n")
	}
	s.admissionNotes = nil
	return strings.TrimRight(b.String(), "\n")
}

// --- Tier 2 ---

// admissionTail is how much of a failing chain step's stderr the check's log keeps.
const admissionTail = 8 << 10

// runAdmission is Tier 2: the checkout's project.json builds through the whole generator chain
// (conformance.RunChainOn: scaffold, every generator, go build, go vet) in a scratch module, with
// the failing step's stderr tail as its log.
func (r checkRunner) runAdmission(ctx context.Context, c review.SpecCheck) CheckResult {
	start := r.now()
	ctx, cancel := context.WithTimeout(ctx, timeoutOf(c, defaultCheckTimeoutSec))
	defer cancel()
	raw, err := os.ReadFile(filepath.Join(r.root, statePathPrefix, projectFile))
	if err != nil {
		return r.finish(c, checkFail, r.since(start), []byte("admission: "+err.Error()+"\n"))
	}
	scratch, err := os.MkdirTemp("", "aiarch-admission-")
	if err != nil {
		return r.finish(c, checkFail, r.since(start), []byte("admission: "+err.Error()+"\n"))
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	rep, err := runChainOn(ctx, "project", raw, scratch)
	var log strings.Builder
	for _, st := range rep.Steps {
		fmt.Fprintf(&log, "%s: exit %d (%s)\n", st.Name, st.ExitCode, st.Duration.Round(100*time.Millisecond))
		if st.ExitCode != 0 && st.Stderr != "" {
			tail := st.Stderr
			if len(tail) > admissionTail {
				tail = "...\n" + tail[len(tail)-admissionTail:]
			}
			log.WriteString(tail + "\n")
		}
	}
	status := checkPass
	switch {
	case ctx.Err() != nil:
		status = checkTimeout
		fmt.Fprintf(&log, "\n[timed out after %ds]\n", int(timeoutOf(c, defaultCheckTimeoutSec)/time.Second))
	case err != nil:
		status = checkFail
		fmt.Fprintf(&log, "\nadmission: %v\n", err)
	}
	return r.finish(c, status, r.since(start), []byte(log.String()))
}
