package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// seedProject writes a project.json (encoded through the real codec) into a fresh temp
// state root and returns a Session bound to it, in the given mode + ambient kind, with a
// recording fake git runner. It is the common fixture for the verb unit tests.
func seedProject(t *testing.T, p projectstate.Project, mode string, kind projectstate.ArtifactKind) (*Session, *fakeGit) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, statePathPrefix), 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	b, err := projectstate.EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("encode seed project: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, statePathPrefix, projectFile), b, 0o644); err != nil {
		t.Fatalf("write seed project: %v", err)
	}
	fg := &fakeGit{}
	s := &Session{
		ProjectID:    p.ID,
		Kind:         kind,
		Mode:         mode,
		StateRoot:    root,
		TargetBranch: "aiarch-design/testproj/3",
		git:          fg.run,
	}
	return s, fg
}

// minimalProject is a valid, decodable head-state with no committed slots — the
// from-scratch drafting baseline.
func minimalProject() projectstate.Project {
	return projectstate.Project{
		ID:      projectstate.ProjectID("testproj"),
		Version: 1,
		Owner:   projectstate.OwnerScope("owner"),
		Name:    "Test Project",
	}
}

// readBackSlot re-reads the on-disk project.json and returns the slot for kind.
func readBackSlot(t *testing.T, s *Session, kind projectstate.ArtifactKind) projectstate.ArtifactSlot {
	t.Helper()
	proj, _, err := s.readProject()
	if err != nil {
		t.Fatalf("read back project: %v", err)
	}
	slot, ok := slotFor(&proj, kind)
	if !ok {
		t.Fatalf("no slot for kind %s", kind)
	}
	return *slot
}

// newTestSession returns a draft-mode Session over the Volatilities slot, seeded with a
// minimal decodable project — the common fixture for review-thread tests where the
// artifact kind itself is incidental.
func newTestSession(t *testing.T) *Session {
	t.Helper()
	s, _ := seedProject(t, minimalProject(), jobModeDraft, projectstate.KindVolatilities)
	return s
}

// seedThread appends c to the ambient slot's review thread, on top of whatever
// newTestSession already wrote to disk.
func seedThread(t *testing.T, s *Session, c projectstate.ReviewComment) {
	t.Helper()
	proj, _, err := s.readProject()
	if err != nil {
		t.Fatalf("read project: %v", err)
	}
	slot, ok := slotFor(&proj, s.Kind)
	if !ok {
		t.Fatalf("no slot for kind %s", s.Kind)
	}
	// encodeSlotsMap drops a slot entirely at ReviewNone (the "nothing drafted yet"
	// zero value), which would silently discard the seeded thread on round-trip.
	// A review thread only exists on a slot that has been staged/committed at least
	// once, so give it a real status.
	if slot.Status == projectstate.ReviewNone {
		slot.Status = projectstate.ReviewCommitted
	}
	slot.ReviewThread = append(slot.ReviewThread, c)
	newBytes, err := projectstate.EncodeProjectJSON(proj)
	if err != nil {
		t.Fatalf("encode project: %v", err)
	}
	if err := s.writeProjectBytes(newBytes); err != nil {
		t.Fatalf("write project: %v", err)
	}
}

// readThread re-reads the checked-out project.json and returns the ambient slot's
// review thread.
func readThread(t *testing.T, s *Session) []projectstate.ReviewComment {
	t.Helper()
	return readBackSlot(t, s, s.Kind).ReviewThread
}

// fakeGit records git invocations and returns canned output, so publishDraft's control
// flow is exercised without a real repo.
type fakeGit struct {
	calls     [][]string
	porcelain string // returned for `status --porcelain`
	failOn    string // if set, any call whose first arg matches returns an error
	// noOrigin makes `git remote` list NOTHING — the LOCAL venue, where the state repo
	// has no origin to push to. The zero value is the remote venue (an origin exists),
	// so every test that predates the local-venue case keeps asserting a push.
	noOrigin bool
}

func (f *fakeGit) run(_ string, args ...string) (string, error) {
	f.calls = append(f.calls, args)
	if f.failOn != "" && len(args) > 0 && args[0] == f.failOn {
		return "", errFake
	}
	if len(args) >= 2 && args[0] == "status" && args[1] == "--porcelain" {
		return f.porcelain, nil
	}
	if len(args) == 1 && args[0] == "remote" {
		if f.noOrigin {
			return "", nil
		}
		return "origin", nil
	}
	// What real git does when asked to push to a remote that is not configured — the exact
	// failure the local venue produced 19 times on one run. Reproduced here so a test can
	// prove the caller never asks in the first place.
	if f.noOrigin && len(args) > 0 && args[0] == "push" {
		return "", &fakeError{"fatal: 'origin' does not appear to be a git repository"}
	}
	return "", nil
}

var errFake = &fakeError{"fake git failure"}

type fakeError struct{ s string }

func (e *fakeError) Error() string { return e.s }

// didCall reports whether git was invoked with the given verb anywhere in its args
// (a commit call carries -c config flags before the "commit" verb).
func (f *fakeGit) didCall(verb string) bool {
	for _, c := range f.calls {
		if slices.Contains(c, verb) {
			return true
		}
	}
	return false
}

// didCallWith reports whether a single git invocation carried BOTH tokens anywhere in
// its args (e.g. a "commit" that also passed "--allow-empty").
func (f *fakeGit) didCallWith(verb, flag string) bool {
	for _, c := range f.calls {
		sawVerb, sawFlag := false, false
		for _, a := range c {
			if a == verb {
				sawVerb = true
			}
			if a == flag {
				sawFlag = true
			}
		}
		if sawVerb && sawFlag {
			return true
		}
	}
	return false
}
