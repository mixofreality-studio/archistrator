package internal_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// contractsDoc is a project document with two service contracts: "a" fronts the Go package
// internal/a and carries aOwned, "b" fronts internal/b and carries bOwned (each a JSON array).
func contractsDoc(aOwned, bOwned string) []byte {
	return []byte(fmt.Sprintf(`{"serviceContracts":{
		"a":{"component":"A","layer":"ResourceAccess","goPackage":"internal/a","ownedPaths":%s},
		"b":{"component":"B","layer":"ResourceAccess","goPackage":"internal/b","ownedPaths":%s}}}`, aOwned, bOwned))
}

// TestDecodeOwnedPaths pins the decode validation of a service contract's ownedPaths: a clean
// module-relative directory is accepted, and every shape or overlap problem is refused as
// malformed committed state naming the offending contract and entry.
func TestDecodeOwnedPaths(t *testing.T) {
	cases := []struct {
		name         string
		a, b         string
		wantContains string // empty: the document must decode
	}{
		{"clean directories", `["cmd/x"]`, `["cmd/y/z"]`, ""},
		{"none", `[]`, `[]`, ""},
		{"empty entry", `[""]`, `[]`, `must name a directory`},
		{"dot", `["."]`, `[]`, `must name a directory`},
		{"absolute", `["/cmd/x"]`, `[]`, `module-relative, not absolute`},
		{"trailing slash", `["cmd/x/"]`, `[]`, `must not end with a slash`},
		{"wildcard", `["cmd/*"]`, `[]`, `wildcard`},
		{"parent segment", `["cmd/../x"]`, `[]`, `must not contain ".."`},
		{"unclean", `["cmd//x"]`, `[]`, `clean path`},
		{"inside another contract's package", `["internal/b/sub"]`, `[]`, `overlaps the goPackage "internal/b" of contract "b"`},
		{"containing another contract's package", `["internal"]`, `[]`, `overlaps the goPackage`},
		{"inside its own package", `["internal/a/sub"]`, `[]`, `overlaps the goPackage "internal/a" of contract "a"`},
		{"two contracts, one directory", `["cmd/x"]`, `["cmd/x"]`, `a directory is owned by one contract`},
		{"nested across contracts", `["cmd/x"]`, `["cmd/x/y"]`, `a directory is owned by one contract`},
		{"sibling with a shared name prefix", `["cmd/x"]`, `["cmd/xy"]`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, ok, err := projectstate.DecodeProjectJSON(contractsDoc(c.a, c.b), "")
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
