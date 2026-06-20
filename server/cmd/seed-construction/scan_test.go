package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanCorpus_FixturePresence(t *testing.T) {
	root := t.TempDir()
	logDir := filepath.Join(root, "log")
	conDir := filepath.Join(root, "contracts")
	os.MkdirAll(logDir, 0o755)
	os.MkdirAll(conDir, 0o755)
	// C-CW: log + passing review + contract → integrated, produced
	os.WriteFile(filepath.Join(logDir, "C-CW.md"), []byte("# build"), 0o644)
	os.WriteFile(filepath.Join(logDir, "C-CW-review.md"), []byte("VERDICT: PASS"), 0o644)
	os.WriteFile(filepath.Join(conDir, "webClient.md"), []byte("# contract"), 0o644)
	// C-BE: log only → in-review
	os.WriteFile(filepath.Join(logDir, "C-BE.md"), []byte("# build"), 0o644)

	got, err := scanCorpus(root)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	cw := got["C-CW"]
	if !cw.HasLog || !cw.HasPassingReview {
		t.Errorf("C-CW want log+review got %+v", cw)
	}
	be := got["C-BE"]
	if !be.HasLog || be.HasPassingReview {
		t.Errorf("C-BE want log-only got %+v", be)
	}
}

func TestNormalizeID_DatedReview(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"C-CW-review-2026-05-30.md", "C-CW"},
		{"C-MSD-recut-review-2026-05-30.md", "C-MSD"},
		{"C-CW.md", "C-CW"},
		{"C-CW-review.md", "C-CW"},
	}
	for _, tc := range cases {
		if got := normalizeID(tc.input); got != tc.want {
			t.Errorf("normalizeID(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestComponentForActivity(t *testing.T) {
	if componentForActivity("C-CW") == "" {
		t.Errorf("C-CW should map to a component")
	}
	if got := componentForActivity("C-MST-Δ"); got != componentForActivity("C-MST") {
		t.Errorf("delta suffix should normalize: %q", got)
	}
}
