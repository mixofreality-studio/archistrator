package main

// operatornotes_manifest_test.go pins the half of operator-note delivery that depends on
// the method-assets STEP MANIFEST (amendment §C.1 item 3, finding H9): the manifest
// narrows every bound session's tool surface, so get_operator_notes reaches a
// dispatched construct step only because method-assets grants it to every construct
// command. Without the grant, a bound construct step would lose the tool its own
// command tells it to call first.

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestOperatorNote_EveryConstructStepKeepsTheTool: every construct command's manifest
// grants get_operator_notes, and a session bound to it registers the tool.
func TestOperatorNote_EveryConstructStepKeepsTheTool(t *testing.T) {
	construct := 0
	for command, m := range methodassets.Manifests() {
		if m.Mode != jobModeConstruct {
			continue
		}
		construct++
		s := constructSession("", command)
		if !grantedTools(s).bound {
			t.Fatalf("%s: expected a bound step manifest", command)
		}
		if !registeredMCPToolNames(t, s)[getOperatorNotesTool] {
			t.Errorf("%s: a construct step must keep get_operator_notes (the method-assets manifest grant)", command)
		}
	}
	if construct == 0 {
		t.Fatal("no construct-mode step manifests found")
	}
}

// TestRig_OperatorNoteOverStdio_BoundToAConstructStep is the dispatched shape: the
// binary bound to service-construction's manifest (AIARCH_COMMAND, exactly as both
// dispatch rails stamp it) still registers get_operator_notes and serves the note.
func TestRig_OperatorNoteOverStdio_BoundToAConstructStep(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	bin := buildBinary(t)
	repo := initGitRepoWithProject(t, minimalProject(), "activity/C-BE")
	note := hostileOperatorNotes["quotes"] + "\n" + hostileOperatorNotes["expression"]

	cmd := exec.Command(bin)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		envJobMode+"="+jobModeConstruct,
		envComponentID+"=billingEngine",
		envActivityID+"=C-BE",
		envTargetBranch+"=activity/C-BE",
		envProjectID+"=rigproj",
		envStateRoot+"="+repo,
		envCommand+"=service-construction",
		envOperatorNote+"="+note,
	)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "rig", Version: "0"}, nil).Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	names := mustListToolNames(ctx, t, session)
	if !names[getOperatorNotesTool] {
		t.Fatalf("service-construction (manifest-bound) must register get_operator_notes: %v", names)
	}
	res := callTool(ctx, t, session, getOperatorNotesTool, map[string]any{})
	if res.IsError || !strings.Contains(contentText(res), "\n"+note+"\n") {
		t.Fatalf("get_operator_notes did not return the note verbatim: %s", contentText(res))
	}
}
