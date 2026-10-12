package internal_test

import (
	"go/constant"
	"go/types"
	"regexp"
	"strconv"
	"testing"
	"time"

	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
	"golang.org/x/tools/go/packages"
)

// R54 (final review I3): the delivery Manager observes a dispatched job for the job's own
// total bound plus a 15-minute margin. The constants are unexported Manager internals (the
// package may hold only generated surface), so this reads them from the type-checked
// package, and reads the bound itself from the templates the scaffold seats.
func TestObserveCeilingCoversTheJobBound(t *testing.T) {
	consts := deliveryConsts(t, "gatedJobTimeoutBound", "gatedJobObserveCeiling",
		"observeFastInterval", "observeFastPolls", "observeMedInterval", "observeMedPolls",
		"observeSlowInterval", "observeSlowPolls", "maxObserveTotalPolls")
	bound := time.Duration(consts["gatedJobTimeoutBound"])
	ceiling := time.Duration(consts["gatedJobObserveCeiling"])
	if ceiling != bound+15*time.Minute {
		t.Errorf("observe ceiling %s, want the job bound %s + 15m", ceiling, bound)
	}
	ladder := time.Duration(consts["observeFastPolls"])*time.Duration(consts["observeFastInterval"]) +
		time.Duration(consts["observeMedPolls"])*time.Duration(consts["observeMedInterval"]) +
		time.Duration(consts["observeSlowPolls"])*time.Duration(consts["observeSlowInterval"])
	if ladder != ceiling {
		t.Errorf("the ladder sums to %s, want the ceiling %s", ladder, ceiling)
	}
	if got := consts["observeFastPolls"] + consts["observeMedPolls"] + consts["observeSlowPolls"]; got != consts["maxObserveTotalPolls"] {
		t.Errorf("maxObserveTotalPolls %d, the ladder has %d", consts["maxObserveTotalPolls"], got)
	}

	files, err := methodassets.ScaffoldFiles(methodassets.ScaffoldData{
		ModulePath: "github.com/acme/shop", AppSlug: "aiarch-app", ProjectID: "shop", Owner: "acme", Name: "shop",
		StateMcpModulePath:    "github.com/mixofreality-studio/archistrator/server/cmd/aiarch-state-mcp",
		StateMcpModuleVersion: "0123456789abcdef0123456789abcdef01234567",
	})
	if err != nil {
		t.Fatal(err)
	}
	// A job-level timeout is indented four spaces (a step's is eight).
	jobTimeout := regexp.MustCompile(`(?m)^    timeout-minutes: (\d+)\s*$`)
	for _, wf := range []string{".github/workflows/aiarch-construct.yml", ".github/workflows/aiarch-design.yml"} {
		body, ok := files[wf]
		if !ok {
			t.Fatalf("the scaffold seats no %s", wf)
		}
		ms := jobTimeout.FindAllStringSubmatch(string(body), -1)
		if len(ms) == 0 {
			t.Fatalf("%s: no job-level timeout-minutes", wf)
		}
		for _, m := range ms {
			mins, _ := strconv.Atoi(m[1])
			if got := time.Duration(mins) * time.Minute; got != bound {
				t.Errorf("%s: a job bound is %s, gatedJobTimeoutBound is %s — move the constant with the template", wf, got, bound)
			}
		}
	}
}

// deliveryConsts evaluates the named package-level integer constants of the delivery
// Manager.
func deliveryConsts(t *testing.T, names ...string) map[string]int64 {
	t.Helper()
	pkgs, err := packages.Load(&packages.Config{Mode: packages.NeedName | packages.NeedSyntax | packages.NeedTypes},
		"github.com/mixofreality-studio/archistrator/server/internal/manager/delivery")
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 || pkgs[0].Types == nil || packages.PrintErrors(pkgs) > 0 {
		t.Fatal("the delivery package did not load")
	}
	out := make(map[string]int64, len(names))
	for _, n := range names {
		c, ok := pkgs[0].Types.Scope().Lookup(n).(*types.Const)
		if !ok {
			t.Fatalf("delivery has no constant %s", n)
		}
		v, exact := constant.Int64Val(c.Val())
		if !exact {
			t.Fatalf("constant %s is not an int64", n)
		}
		out[n] = v
	}
	return out
}
