package main

import (
	"os"
	"path/filepath"
	"testing"
)

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanCorpus_FixturePresence(t *testing.T) {
	root := t.TempDir()
	logDir := filepath.Join(root, "log")
	conDir := filepath.Join(root, "contracts")
	mkdir(t, logDir)
	mkdir(t, conDir)
	// C-CW: log + passing review + contract → integrated, produced
	write(t, filepath.Join(logDir, "C-CW.md"), "# build")
	write(t, filepath.Join(logDir, "C-CW-review.md"), "VERDICT: PASS")
	write(t, filepath.Join(conDir, "webClient.md"), "# contract")
	// C-BE: log only → in-review
	write(t, filepath.Join(logDir, "C-BE.md"), "# build")

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
