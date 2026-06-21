// Package constitution holds the R6 enforcement of the test-authoring
// constitution ([[the-method-testing]] §7). These tests prove, STRUCTURALLY,
// that the harness module cannot cheat by importing the system under test. They
// need NO infrastructure and run on every `go test` — including -short and
// container-less sandboxes — so the anti-cheat guarantee is always verified.
package constitution

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// serverModulePrefix is the system-under-test module. The harness must import
// none of it. (Its internal/ packages are additionally compiler-sealed by Go's
// internal rule; this test also forbids the module's non-internal packages and
// makes the guarantee toolchain-independent.)
const serverModulePrefix = "github.com/mixofreality-studio/archistrator/server"

// mockingLibs are general mocking / monkey-patching libraries banned in the
// harness: a black-box wire harness substitutes NOTHING — it drives the real
// system. depguard enforces the same in .golangci.yml; this is the
// toolchain-independent guarantee.
var mockingLibs = []string{
	"github.com/stretchr/testify/mock",
	"go.uber.org/mock",
	"github.com/golang/mock",
	"bou.ke/monkey",
}

// TestHarnessIsOutsideServerTree enforces R3 placement: the module must NOT live
// under a server/ tree, or Go's internal/ seal would no longer protect the
// system's internals from harness import.
func TestHarnessIsOutsideServerTree(t *testing.T) {
	root := moduleRoot(t)
	sep := string(os.PathSeparator)
	if strings.Contains(root+sep, sep+"server"+sep) {
		t.Fatalf("harness module root %q is inside a server/ tree — breaks the internal/ seal (R3)", root)
	}
}

// TestHarnessImportsNoServerCode walks every Go source file in the module and
// asserts it imports neither the system-under-test module nor any banned mocking
// library — the black-box / anti-cheat guarantee (R1, R3): the harness speaks
// only the wire contract.
func TestHarnessImportsNoServerCode(t *testing.T) {
	root := moduleRoot(t)
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if p == serverModulePrefix || strings.HasPrefix(p, serverModulePrefix+"/") {
				t.Errorf("%s imports the system-under-test %q — black-box rule violated (R1/R3)", relTo(root, path), p)
			}
			for _, m := range mockingLibs {
				if p == m || strings.HasPrefix(p, m+"/") {
					t.Errorf("%s imports mocking lib %q — a wire harness substitutes nothing (R3)", relTo(root, path), p)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk module source: %v", err)
	}
}

// moduleRoot walks up from this source file to the directory holding go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve caller for module root")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", file)
		}
		dir = parent
	}
}

func relTo(root, path string) string {
	if r, err := filepath.Rel(root, path); err == nil {
		return r
	}
	return path
}
