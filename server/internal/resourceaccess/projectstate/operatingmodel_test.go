package projectstate_test

// operatingmodel_test.go — coverage for the project-level OperatingModel field + the
// SetOperatingModel head-state write (founder ruling 2026-07-05). A project is born
// self-operated (the back-compat default); SetOperatingModel flips it to
// archistrator-operated; a project.json that pre-dates the field decodes to the
// default; and an unknown wire value is rejected.

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	ps "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

func TestGitStore_SetOperatingModel_RoundTrip(t *testing.T) {
	store, _, cred, ctx := newLocalGitStoreWithRepo(t)
	id := ps.ProjectID(uuid.NewString())
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// A fresh project is born self-operated (the default applied on decode).
	proj, err := store.ReadProject(ctx, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if proj.OperatingModel != ps.OperatingModelSelfOperated {
		t.Fatalf("fresh project operating model = %q, want selfOperated (born explicit)", proj.OperatingModel)
	}

	// Flip it to archistrator-operated.
	v2, err := store.SetOperatingModel(ctx, id, proj.Version, ps.OperatingModelArchistratorOperated, cred, "wf:setmodel")
	if err != nil {
		t.Fatalf("SetOperatingModel: %v", err)
	}
	after, err := store.ReadProject(ctx, id, cred)
	if err != nil {
		t.Fatalf("ReadProject after set: %v", err)
	}
	if after.OperatingModel != ps.OperatingModelArchistratorOperated {
		t.Fatalf("operating model after set = %q, want archistratorOperated", after.OperatingModel)
	}

	// IDEMPOTENT RETRY: same key dedups to the original result version (no double-write).
	vAgain, err := store.SetOperatingModel(ctx, id, 999, ps.OperatingModelArchistratorOperated, cred, "wf:setmodel")
	if err != nil {
		t.Fatalf("idempotent retry SetOperatingModel: %v", err)
	}
	if vAgain != v2 {
		t.Fatalf("idempotent retry must dedup to result version %d, got %d", v2, vAgain)
	}
}

func TestGitStore_SetOperatingModel_RejectsUnknownValue(t *testing.T) {
	store, _, cred, ctx := newLocalGitStoreWithRepo(t)
	id := ps.ProjectID(uuid.NewString())
	if _, err := store.CreateProject(ctx, id, "alice", "Demo", cred, "wf:create"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	_, err := store.SetOperatingModel(ctx, id, 1, ps.OperatingModel("bogusCloud"), cred, "wf:bad")
	if err == nil {
		t.Fatal("SetOperatingModel with an unknown model must fail")
	}
}

// TestDecodeProjectJSON_PreFieldReadsAsSelfOperated proves a committed project.json
// that pre-dates the operatingModel field decodes to the EMPTY value (preserved verbatim
// for byte-identical round-trip) which every reader interprets as the DEFAULT
// (selfOperated) via OrDefault — so an existing project keeps today's open guidance.
func TestDecodeProjectJSON_PreFieldReadsAsSelfOperated(t *testing.T) {
	// A minimal pre-field document — no "operatingModel" key at all.
	raw := []byte(`{"id":"p1","version":3,"phase":0,"owner":"alice","name":"Legacy","research":{"Sources":null},"slots":{}}`)
	proj, ok, err := ps.DecodeProjectJSON(raw, ps.ProjectID("p1"))
	if err != nil || !ok {
		t.Fatalf("DecodeProjectJSON: ok=%v err=%v", ok, err)
	}
	if !proj.OperatingModel.IsZero() {
		t.Fatalf("pre-field project decoded operating model = %q, want empty (verbatim)", proj.OperatingModel)
	}
	if proj.OperatingModel.OrDefault() != ps.OperatingModelSelfOperated {
		t.Fatalf("pre-field project OrDefault = %q, want selfOperated", proj.OperatingModel.OrDefault())
	}
}

// TestEncodeProjectJSON_PersistsOperatingModel proves the field round-trips through the
// canonical project.json encoder once set (a lazy migration persists the concrete value).
func TestEncodeProjectJSON_PersistsOperatingModel(t *testing.T) {
	p := ps.Project{ID: "p1", Owner: "alice", Name: "Demo", OperatingModel: ps.OperatingModelArchistratorOperated}
	b, err := ps.EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("EncodeProjectJSON: %v", err)
	}
	if !strings.Contains(string(b), `"operatingModel": "archistratorOperated"`) {
		t.Fatalf("encoded project.json missing operatingModel; got:\n%s", string(b))
	}
}
