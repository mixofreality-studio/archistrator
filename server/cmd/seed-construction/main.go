package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/davidmarne/archistrator/server/internal/resourceaccess/projectstate"
)

func main() {
	corpus := flag.String("corpus", "../../methodpoc/designs/aiarch/implementation", "path to implementation corpus (log/ + contracts/)")
	file := flag.String("file", "/Users/davidmarne/mixofrealitystudio/archistrator/.aiarch/state/project.json", "path to project.json to rewrite")
	id := flag.String("id", "archistrator", "project id")
	flag.Parse()

	raw, err := os.ReadFile(*file)
	must(err, "read project.json")
	proj, ok, err := projectstate.DecodeProjectJSON(raw, projectstate.ProjectID(*id))
	must(err, "decode")
	if !ok {
		fail("no project document")
	}

	presence, err := scanCorpus(*corpus)
	must(err, "scan corpus")

	// contract file presence
	contractFiles := map[string]string{} // component -> corpus-relative path
	cEntries, err := os.ReadDir(filepath.Join(*corpus, "contracts"))
	must(err, "read contracts dir")
	for _, e := range cEntries {
		if e.IsDir() {
			continue
		}
		stem := e.Name()[:len(e.Name())-len(filepath.Ext(e.Name()))]
		contractFiles[stem] = "implementation/contracts/" + e.Name()
	}

	acts := proj.ActivityList.Model.(*projectstate.ActivityList).Activities
	rows := map[string]projectstate.ActivityConstructionStatus{}
	var matched, unmatched []string
	integratedEffort, totalEffort := 0.0, 0.0

	for _, a := range acts {
		id := a.Name
		totalEffort += a.EffortDays
		comp := componentForActivity(id)
		rawP := presence[normalizeID(id+".md")]
		cp := projectstate.CorpusPresence{HasLog: rawP.HasLog, HasPassingReview: rawP.HasPassingReview}
		if comp != "" {
			if path, ok := contractFiles[comp]; ok {
				cp.HasContract = true
				cp.ContractFile = path
			}
		}
		if !cp.HasLog && !cp.HasContract {
			continue // no corpus evidence → honest-empty (absent row)
		}
		status, integrated := projectstate.DeriveBuildStatus(cp)
		// Phase is the PUMP lifecycle, distinct from the corpus BuildStatus lens: it is
		// Done ONLY for a truly-integrated activity (log + passing review). Everything else
		// — including a has-log-but-in-review activity — is NotStarted from the pump's POV
		// so a live construction resume PICKS IT UP and drives it to done. (BuildStatus
		// still carries the corpus evidence: in-review / in-construction render until the
		// pump completes the activity.) Conflating in-review into Phase=Running walled the
		// cascade off behind activities the pump would never resume.
		phase := projectstate.ActivityConstructionNotStarted
		if integrated {
			phase = projectstate.ActivityConstructionDone
			integratedEffort += a.EffortDays
		}
		rows[id] = projectstate.ActivityConstructionStatus{
			ActivityID:  id,
			Phase:       phase,
			Kind:        projectstate.DeriveKind(id, comp),
			BuildStatus: status,
			Produced:    projectstate.DeriveProduced(cp, comp),
		}
		matched = append(matched, id)
		if comp == "" && cp.HasLog {
			unmatched = append(unmatched, id) // logged but no component mapping
		}
	}

	proj.ActivityConstruction = rows
	pct := 0
	if totalEffort > 0 {
		pct = int(integratedEffort / totalEffort * 100)
	}
	// Week:0 / TotalWeeks:0 are sentinels: the server derives the EV horizon from the real
	// CPM schedule (computeEV totalWeeks<=0 branch), rather than a fabricated 19-week frame.
	proj.ConstructionProgress = &projectstate.ConstructionProgress{
		Week: 0, TotalWeeks: 0, HandOffModel: "Senior hand-off", SupervisionCap: 3,
	}

	enc, err := projectstate.EncodeProjectJSON(proj)
	must(err, "encode")
	must(os.WriteFile(*file, enc, 0o644), "write project.json")

	sort.Strings(matched)
	sort.Strings(unmatched)
	fmt.Printf("seeded %d construction rows (%d%% integrated by effort) at %s\n", len(rows), pct, time.Now().Format(time.RFC3339))
	if len(unmatched) > 0 {
		fmt.Printf("WARNING %d logged activities had no component mapping (no contract artifact): %v\n", len(unmatched), unmatched)
	}
}

func must(err error, what string) {
	if err != nil {
		fail(what + ": " + err.Error())
	}
}
func fail(msg string) {
	fmt.Fprintln(os.Stderr, "seed-construction:", msg)
	os.Exit(1)
}
