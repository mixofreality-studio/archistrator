package main

import (
	"os"
	"path/filepath"
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

// fakeGit records git invocations and returns canned output, so publishDraft's control
// flow is exercised without a real repo.
type fakeGit struct {
	calls     [][]string
	porcelain string // returned for `status --porcelain`
	failOn    string // if set, any call whose first arg matches returns an error
}

func (f *fakeGit) run(_ string, args ...string) (string, error) {
	f.calls = append(f.calls, args)
	if f.failOn != "" && len(args) > 0 && args[0] == f.failOn {
		return "", errFake
	}
	if len(args) >= 2 && args[0] == "status" && args[1] == "--porcelain" {
		return f.porcelain, nil
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
		for _, a := range c {
			if a == verb {
				return true
			}
		}
	}
	return false
}
