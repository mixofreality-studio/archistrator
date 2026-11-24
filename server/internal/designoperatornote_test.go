package internal_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// BLK-APP-DESIGN-OPERATOR-NOTE-DROPPED: an operator's note on a design draft or critique
// (a dispatch's feedback, a human send-back) reached the draft agent in neither venue, and
// the Manager reported success. The note now rides four seams, and each is pinned here as a
// SOURCE gate for the same reason TestLifecyclePinStampsDAGIdentity is one: the delivery
// Manager and agenticJobAccess packages admit only generated scenario tests (TestFileLayout),
// and every scenario bound to them is skipped under ruling 8 (a full run needs arranged
// state), so a declarative binding would pin nothing today. Each assertion below fails when
// its seam is removed.
//
//  1. delivery.submitDesignJob stamps DispatchInputs["operator_note"] from the pending block,
//     converges the seated scaffold BEFORE that stamp (a repo seated before the design
//     workflow declared the input would answer the dispatch with HTTP 422), and settles the
//     carried notes after the submit.
//  2. delivery.produceDesignArtifact carries a send-back's feedback into the redraft.
//  3. delivery.DispatchActivityTask refuses feedback aimed at a computed task.
//  4. agenticjob.designDispatchPlan stamps AIARCH_OPERATOR_NOTE into the rig when non-empty,
//     and the local submitDesignJob hands it the note and refuses one on an answer job.
func TestDesignOperatorNoteSeams(t *testing.T) {
	delivery := filepath.Join("manager", "delivery")
	agentic := filepath.Join("resourceaccess", "agenticjob")

	submit := findFunc(t, delivery, "submitDesignJob")
	stamp := indexAssignPos(submit, "DispatchInputs", "dispatchInputOperatorNote")
	if stamp == token.NoPos {
		t.Error(`delivery.submitDesignJob must set DispatchInputs[dispatchInputOperatorNote]`)
	}
	sync := firstCallPos(submit, "syncScaffoldBeforeDispatch")
	switch {
	case sync == token.NoPos:
		t.Error("delivery.submitDesignJob must call syncScaffoldBeforeDispatch before it dispatches a note (a stale seated aiarch-design.yml rejects operator_note)")
	case stamp != token.NoPos && sync > stamp:
		t.Error("delivery.submitDesignJob must sync the managed scaffold BEFORE it stamps the note into the dispatch inputs")
	}
	for _, c := range []string{"pendingNotesBlock", "settleCarriedNotes"} {
		if firstCallPos(submit, c) == token.NoPos {
			t.Errorf("delivery.submitDesignJob must call %s", c)
		}
	}

	if firstCallPos(findFunc(t, delivery, "produceDesignArtifact"), "carryWalkFeedback") == token.NoPos {
		t.Error("delivery.produceDesignArtifact must call carryWalkFeedback so a send-back's steer rides the redraft")
	}

	if !hasFeedbackRefusal(findFunc(t, delivery, "DispatchActivityTask")) {
		t.Error("delivery.DispatchActivityTask must refuse non-nil feedback on a computed task with fwmanager.ContractMisuse")
	}

	plan := findFunc(t, agentic, "designDispatchPlan")
	if indexAssignPos(plan, "rig", "rigOperatorNoteKey") == token.NoPos {
		t.Error("agenticjob.designDispatchPlan must stamp rig[rigOperatorNoteKey] (AIARCH_OPERATOR_NOTE)")
	}
	local := findFunc(t, agentic, "submitDesignJob")
	if !callPassesIdent(local, "designDispatchPlan", "operatorNote") {
		t.Error("agenticjob submitDesignJob must pass the operator note to designDispatchPlan")
	}
	if !hasAnswerNoteRefusal(local) {
		t.Error("agenticjob submitDesignJob must refuse an operator note on an answer job with fwra.ContractMisuse")
	}
}

// findFunc parses dir's non-test sources and returns the function or method named name.
func findFunc(t *testing.T, dir, name string) *ast.FuncDecl {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	var found *ast.FuncDecl
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, n), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", n, err)
		}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == name {
				if found != nil {
					t.Fatalf("%s: %s is declared twice; the gate cannot tell which to read", dir, name)
				}
				found = fd
			}
		}
	}
	if found == nil {
		t.Fatalf("%s must declare %s", dir, name)
	}
	return found
}

// calleeName is the trailing identifier of a call's function expression.
func calleeName(c *ast.CallExpr) string {
	switch f := c.Fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

// firstCallPos is the position of the earliest call of name in fn, or NoPos.
func firstCallPos(fn *ast.FuncDecl, name string) token.Pos {
	pos := token.NoPos
	ast.Inspect(fn, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok && calleeName(c) == name && (pos == token.NoPos || c.Pos() < pos) {
			pos = c.Pos()
		}
		return true
	})
	return pos
}

// callPassesIdent reports whether some call of name in fn has an argument that is the bare ident arg.
func callPassesIdent(fn *ast.FuncDecl, name, arg string) bool {
	ok := false
	ast.Inspect(fn, func(n ast.Node) bool {
		if c, isCall := n.(*ast.CallExpr); isCall && calleeName(c) == name {
			for _, a := range c.Args {
				if id, isID := a.(*ast.Ident); isID && id.Name == arg {
					ok = true
				}
			}
		}
		return true
	})
	return ok
}

// indexAssignPos is the position of the earliest assignment `<m>[key] = ...` in fn, where m is
// the ident or selector field named mapName and key the ident keyName.
func indexAssignPos(fn *ast.FuncDecl, mapName, keyName string) token.Pos {
	pos := token.NoPos
	ast.Inspect(fn, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range as.Lhs {
			ix, ok := lhs.(*ast.IndexExpr)
			if !ok {
				continue
			}
			key, ok := ix.Index.(*ast.Ident)
			if !ok || key.Name != keyName {
				continue
			}
			var base string
			switch b := ix.X.(type) {
			case *ast.Ident:
				base = b.Name
			case *ast.SelectorExpr:
				base = b.Sel.Name
			}
			if base == mapName && (pos == token.NoPos || as.Pos() < pos) {
				pos = as.Pos()
			}
		}
		return true
	})
	return pos
}

// mentionsSelector reports whether n contains the selector pkg.name.
func mentionsSelector(n ast.Node, pkg, name string) bool {
	hit := false
	ast.Inspect(n, func(x ast.Node) bool {
		if s, ok := x.(*ast.SelectorExpr); ok && s.Sel.Name == name {
			if id, ok := s.X.(*ast.Ident); ok && id.Name == pkg {
				hit = true
			}
		}
		return true
	})
	return hit
}

// identsOf is the set of identifier names appearing in n.
func identsOf(n ast.Node) map[string]bool {
	seen := map[string]bool{}
	ast.Inspect(n, func(x ast.Node) bool {
		if id, ok := x.(*ast.Ident); ok {
			seen[id.Name] = true
		}
		return true
	})
	return seen
}

// hasFeedbackRefusal: an `if` whose condition names `feedback` and whose body names
// fwmanager.ContractMisuse.
func hasFeedbackRefusal(fn *ast.FuncDecl) bool {
	hit := false
	ast.Inspect(fn, func(n ast.Node) bool {
		if ifs, ok := n.(*ast.IfStmt); ok && identsOf(ifs.Cond)["feedback"] && mentionsSelector(ifs.Body, "fwmanager", "ContractMisuse") {
			hit = true
		}
		return true
	})
	return hit
}

// hasAnswerNoteRefusal: an `if` whose condition names operatorNote and designJobModeAnswer
// and whose body names fwra.ContractMisuse.
func hasAnswerNoteRefusal(fn *ast.FuncDecl) bool {
	hit := false
	ast.Inspect(fn, func(n ast.Node) bool {
		if ifs, ok := n.(*ast.IfStmt); ok {
			c := identsOf(ifs.Cond)
			if c["operatorNote"] && c["designJobModeAnswer"] && mentionsSelector(ifs.Body, "fwra", "ContractMisuse") {
				hit = true
			}
		}
		return true
	})
	return hit
}
