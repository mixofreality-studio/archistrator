package main

import (
	"os"
	"path/filepath"
	"strings"
)

// scan.go holds the corpus filesystem IO for the seed-construction generator. It reads
// methodpoc/designs/aiarch/implementation/{log,contracts} and reports, per activity id,
// what corpus evidence exists. The pure derivation rules live in the projectstate package.

// passing review markers — a review log counts as passing unless it records a send-back.
func reviewPasses(body string) bool {
	up := strings.ToUpper(body)
	if strings.Contains(up, "SEND BACK") || strings.Contains(up, "SEND-BACK") || strings.Contains(up, "VERDICT: FAIL") {
		return false
	}
	return true
}

// normalizeID strips known activity-id suffixes so a log file maps to its base activity.
func normalizeID(name string) string {
	id := strings.ToUpper(strings.TrimSuffix(name, ".md"))
	// strip date stamps like -2026-05-30 FIRST so a trailing -REVIEW suffix is exposed
	if i := strings.Index(id, "-2026"); i >= 0 {
		id = id[:i]
	}
	// drop review marker
	id = strings.TrimSuffix(id, "-REVIEW")
	id = strings.TrimSuffix(id, "-R")
	// drop common work-suffixes that are the same underlying activity
	for _, sfx := range []string{"-GIT", "-Δ", "-AD", "-AG", "-RB", "-C2", "-PR", "-RECONCILE", "-RECUT", "-CRITIQUE-FIX", "-PREP"} {
		id = strings.TrimSuffix(id, sfx)
	}
	return id
}

func isReviewFile(name string) bool {
	up := strings.ToUpper(name)
	return strings.Contains(up, "-REVIEW") || strings.HasSuffix(up, "-R.MD")
}

// scanCorpus walks log/ and contracts/ and returns presence keyed by normalized id.
func scanCorpus(root string) (map[string]CorpusPresenceRaw, error) {
	out := map[string]CorpusPresenceRaw{}
	logDir := filepath.Join(root, "log")
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		id := normalizeID(e.Name())
		cur := out[id]
		if isReviewFile(e.Name()) {
			body, err := os.ReadFile(filepath.Join(logDir, e.Name())) // #nosec G304 -- logDir is the developer-provided corpus root scanned by a local seed tool, no trust boundary
			if err == nil && reviewPasses(string(body)) {
				cur.HasPassingReview = true
			}
		} else {
			cur.HasLog = true
		}
		out[id] = cur
	}
	return out, nil
}

// CorpusPresenceRaw is the cmd-local mirror of projectstate.CorpusPresence (kept local so
// the scanner has no import cycle); main.go converts it + the contract lookup into the
// projectstate.CorpusPresence the pure rules consume.
type CorpusPresenceRaw struct {
	HasLog           bool
	HasPassingReview bool
}

// componentForActivity maps an activity id to its contract component name. The corpus uses
// component file names (webClient.md, settlementManager.md); the activity ids encode the
// component via a stable abbreviation table. Unmapped ids return "".
func componentForActivity(activityID string) string {
	base := normalizeID(activityID + ".md")
	base = strings.TrimPrefix(base, "C-")
	base = strings.TrimPrefix(base, "D-")
	base = strings.TrimPrefix(base, "U-")
	base = strings.TrimPrefix(base, "I-")
	base = strings.TrimPrefix(base, "N-")
	if c, ok := componentAbbrev[base]; ok {
		return c
	}
	return ""
}

// componentAbbrev maps the activity-id abbreviation to the contract file stem. Derived
// from the corpus contracts/ listing; extend here when the generator reports an unmatched id.
var componentAbbrev = map[string]string{
	"CW":  "webClient",
	"MST": "settlementManager",
	"MCN": "constructionManager",
	"MPD": "projectDesignManager",
	"MSD": "systemDesignManager",
	"MOP": "operationsManager",
	"BM":  "billingManager",
	"BE":  "billingEngine",
	"AE":  "autoscalerEngine",
	"HE":  "handOffEngine",
	"IE":  "interventionEngine",
	"SDE": "systemDesignEngine",
	"AV":  "artifactValidationEngine",
	"EE":  "constructionEstimationEngine",
	"OE":  "operationEstimationEngine",
	"SE":  "settlementEngine",
	"PA":  "projectStateAccess",
	"AA":  "artifactAccess",
	"RE":  "artifactRenderingAccess",
	"DA":  "durableExecutionAccess",
	"CP":  "constructionPipelineAccess",
	"UA":  "usageAccess",
	"WA":  "workerAccess",
	"SC":  "sourceControlAccess",
	"DG":  "billingGatewayAccess",
	"LG":  "revenueLedgerAccess",
	"BG":  "billingStateAccess",
	"OR":  "operatedRuntimeAccess",
	"BS":  "billingStateAccess",
	"VCI": "mcpClient",
}
