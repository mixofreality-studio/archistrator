package internal_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// workspaceDoc is a project document declaring the given workspaceRepos (a JSON array) with
// two contracts: "a" (goPackage internal/a) and "b" (goPackage internal/b), each carrying the
// given ownedPaths (a JSON array).
func workspaceDoc(repos, aOwned, bOwned string) []byte {
	return []byte(fmt.Sprintf(`{"workspaceRepos":%s,"serviceContracts":{
		"a":{"component":"A","layer":"Engine","goPackage":"internal/a","ownedPaths":%s},
		"b":{"component":"B","layer":"Engine","goPackage":"internal/b","ownedPaths":%s}}}`, repos, aOwned, bOwned))
}

const platformRepo = `[{"id":"archistrator-platform","relPath":"../archistrator-platform"}]`

// TestDecodeWorkspaceOwnedPaths pins a repo-qualified ownedPaths entry ("@<repo>/<dir>"): it
// names a declared workspace repo by id, holds a clean directory inside that repo, and
// overlaps only entries of the same repo (an in-project package of the same spelling is a
// different directory).
func TestDecodeWorkspaceOwnedPaths(t *testing.T) {
	cases := []struct {
		name         string
		repos, a, b  string
		wantContains string // empty: the document must decode
	}{
		{"declared repo", platformRepo, `["@archistrator-platform/framework-go/methodcheck"]`, `[]`, ""},
		{"same spelling in project and repo", platformRepo, `["@archistrator-platform/internal/b"]`, `[]`, ""},
		{"undeclared repo", `[]`, `["@archistrator-platform/framework-go"]`, `[]`, `names workspace repo "archistrator-platform", which the project does not declare`},
		{"no directory", platformRepo, `["@archistrator-platform"]`, `[]`, `must be "@<repo>/<dir>"`},
		{"repo root", platformRepo, `["@archistrator-platform/"]`, `[]`, `must name a directory`},
		{"parent segment", platformRepo, `["@archistrator-platform/a/../b"]`, `[]`, `must not contain ".."`},
		{"wildcard", platformRepo, `["@archistrator-platform/a/*"]`, `[]`, `wildcard`},
		{"two contracts, one repo directory", platformRepo, `["@archistrator-platform/x"]`, `["@archistrator-platform/x/y"]`, `a directory is owned by one contract`},
		{"clean siblings", platformRepo, `["@archistrator-platform/x"]`, `["@archistrator-platform/xy"]`, ""},
		{"duplicate repo id", `[{"id":"p","relPath":"../p"},{"id":"p","relPath":"../q"}]`, `[]`, `[]`, `declared twice`},
		{"repo id with a slash", `[{"id":"p/q","relPath":"../p"}]`, `[]`, `[]`, `must be a plain name`},
		{"absolute relPath", `[{"id":"p","relPath":"/p"}]`, `[]`, `[]`, `must be relative`},
		{"empty relPath", `[{"id":"p","relPath":""}]`, `[]`, `[]`, `must be relative`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, ok, err := projectstate.DecodeProjectJSON(workspaceDoc(c.repos, c.a, c.b), "")
			if c.wantContains == "" {
				if err != nil || !ok {
					t.Fatalf("want the document to decode, got ok=%v err=%v", ok, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want a refusal containing %q, the document decoded", c.wantContains)
			}
			if !strings.Contains(err.Error(), c.wantContains) {
				t.Fatalf("refusal %q does not contain %q", err, c.wantContains)
			}
		})
	}
}
