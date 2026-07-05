package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// reconcile_test.go — coverage for the one-shot `reconcile` subcommand (F80b).

func writeDoc(t *testing.T, dir, name string, slots map[int]map[string]any) string {
	t.Helper()
	doc := map[string]any{"schemaVersion": 1, "slots": slots}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func TestRunReconcile_OverlaysOwnSlot(t *testing.T) {
	dir := t.TempDir()
	missionSlot := func(v string) map[string]any {
		return map[string]any{"status": int(projectstate.ReviewCommitted), "kind": int(projectstate.KindMission),
			"model": map[string]any{"vision": v, "objectives": []any{}, "mission": v}}
	}
	// main advanced Mission; the session branch owns Mission at its in-flight value.
	base := writeDoc(t, dir, "main.json", map[int]map[string]any{int(projectstate.KindMission): missionSlot("MAIN")})
	ours := writeDoc(t, dir, "ours.json", map[int]map[string]any{int(projectstate.KindMission): missionSlot("BRANCH")})
	out := filepath.Join(dir, "out.json")

	getenv := func(k string) string {
		switch k {
		case envArtifactKind:
			return projectstate.KindMission.String()
		case envProjectID:
			return "p"
		}
		return ""
	}
	if err := runReconcile(getenv, []string{"--base", base, "--ours", ours, "--out", out}); err != nil {
		t.Fatalf("runReconcile: %v", err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read out: %v", err)
	}
	proj, ok, err := projectstate.DecodeProjectJSON(raw, projectstate.ProjectID("p"))
	if err != nil || !ok {
		t.Fatalf("decode out: ok=%v err=%v", ok, err)
	}
	m, _ := proj.Mission.Model.(*projectstate.MissionStatement)
	if m == nil || m.Vision != "BRANCH" {
		t.Fatalf("reconciled Mission must be the session-branch value, got %+v", proj.Mission.Model)
	}
}

func TestRunReconcile_RequiresFlagsAndKind(t *testing.T) {
	getenvWithKind := func(k string) string {
		if k == envArtifactKind {
			return projectstate.KindMission.String()
		}
		return ""
	}
	if err := runReconcile(getenvWithKind, []string{"--base", "b"}); err == nil {
		t.Fatal("missing --ours/--out must error")
	}
	getenvNoKind := func(string) string { return "" }
	if err := runReconcile(getenvNoKind, []string{"--base", "b", "--ours", "o", "--out", "x"}); err == nil {
		t.Fatal("missing AIARCH_ARTIFACT_KIND must error")
	}
}
