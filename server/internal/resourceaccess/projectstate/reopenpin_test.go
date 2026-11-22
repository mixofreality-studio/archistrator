package projectstate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
)

const pinTestKey = "service"

func pinTestTask(t *testing.T) MethodTask {
	t.Helper()
	lc, ok := methodassets.LifecycleFor(pinTestKey)
	if !ok || len(lc.Tasks) == 0 {
		t.Fatalf("no lifecycle %q", pinTestKey)
	}
	return MethodTask(lc.Tasks[0].ID)
}

func pinOld() LifecyclePin {
	return LifecyclePin{TypeKey: pinTestKey, AssetsVersion: "dag-sha256:old"}
}
func pinNew() LifecyclePin {
	return LifecyclePin{TypeKey: pinTestKey, AssetsVersion: LifecycleIdentity(pinTestKey)}
}

func TestDecideReopenPin_Birth(t *testing.T) {
	if _, ok := decideReopenPin(ActivityExecution{}, pinNew()).(pinBirth); !ok {
		t.Fatal("unpinned row must birth-pin")
	}
}

func TestDecideReopenPin_Keep(t *testing.T) {
	p := pinNew()
	live := time.Now()
	if _, ok := decideReopenPin(ActivityExecution{Pin: &p, StartedAt: &live}, p).(pinKeep); !ok {
		t.Fatal("equal pin must keep, live or not")
	}
}

func TestDecideReopenPin_RepinAfterRequeue(t *testing.T) {
	old := pinOld()
	cs := ActivityExecution{ActivityID: "A", Pin: &old, Attempts: []TaskAttempt{{Task: pinTestTask(t)}}}
	d, ok := decideReopenPin(cs, pinNew()).(pinRepin)
	if !ok || d.From != old || d.To != pinNew() {
		t.Fatalf("want repin old->new, got %#v", d)
	}
}

func TestDecideReopenPin_RefusesLiveRow(t *testing.T) {
	old := pinOld()
	live := time.Now()
	d, ok := decideReopenPin(ActivityExecution{Pin: &old, StartedAt: &live}, pinNew()).(pinRefuse)
	if !ok || len(d.Missing) != 0 {
		t.Fatalf("live row must refuse as misuse, got %#v", d)
	}
}

func TestDecideReopenPin_RefusesTypeChange(t *testing.T) {
	old := LifecyclePin{TypeKey: "other", AssetsVersion: "x"}
	if _, ok := decideReopenPin(ActivityExecution{Pin: &old}, pinNew()).(pinRefuse); !ok {
		t.Fatal("type change must refuse")
	}
}

func TestApplyReopenPin_RepinAppendsHistory(t *testing.T) {
	old := pinOld()
	cs := ActivityExecution{ActivityID: "A", Pin: &old}
	now := time.Now()
	if err := applyReopenPin(&cs, "A", pinNew(), now); err != nil {
		t.Fatal(err)
	}
	if *cs.Pin != pinNew() || len(cs.PinHistory) != 1 || cs.PinHistory[0].Pin != old || !cs.PinHistory[0].SupersededAt.Equal(now) {
		t.Fatalf("pin %v history %v", cs.Pin, cs.PinHistory)
	}
}

func TestApplyReopenPin_LiveRowIsMisuse(t *testing.T) {
	old := pinOld()
	live := time.Now()
	cs := ActivityExecution{ActivityID: "A", Pin: &old, StartedAt: &live}
	err := applyReopenPin(&cs, "A", pinNew(), live)
	var ra *fwra.Error
	if err == nil || !errors.As(err, &ra) || ra.Kind != fwra.ContractMisuse || *cs.Pin != old || len(cs.PinHistory) != 0 {
		t.Fatalf("want ContractMisuse and no change, got %v", err)
	}
}

func TestApplyReopenPin_MissingTaskIsConflictNamingIDs(t *testing.T) {
	old := pinOld()
	cs := ActivityExecution{ActivityID: "A", Pin: &old, Attempts: []TaskAttempt{{Task: "no-such-task"}}}
	err := applyReopenPin(&cs, "A", pinNew(), time.Now())
	var ra *fwra.Error
	if err == nil || !errors.As(err, &ra) || ra.Kind != fwra.Conflict || !strings.Contains(err.Error(), "no-such-task") || *cs.Pin != old {
		t.Fatalf("want Conflict naming the task, got %v", err)
	}
}

func TestLifecycleIdentity_StableAndDiscriminating(t *testing.T) {
	id := LifecycleIdentity(pinTestKey)
	if id != LifecycleIdentity(pinTestKey) || !strings.HasPrefix(id, "dag-sha256:") {
		t.Fatal("identity must be deterministic and DAG-keyed")
	}
	if id == LifecycleIdentity("no-such-type") {
		t.Fatal("distinct lifecycles must differ")
	}
}

// The tests below drive the REAL operations (OpenActivity, RecordAttemptOutcome,
// RecordActivityOutcome, RecordOperatorNote) over a throwaway local git repo, so a
// regression of OpenActivity's own pin handling is caught, not just of the pure helpers.

const pinProject ProjectID = "pin-project"

func pinRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(dir, ".aiarch", "state"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".aiarch", "state", "project.json"), []byte(`{"id":"pin-project"}`), 0o644); err != nil { //nolint:gosec // a repo file
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "."}, {"commit", "-q", "-m", "arranged state"}, {"checkout", "-q", "-b", "checkout"}} {
		cmd := exec.Command("git", append([]string{"-c", "user.name=seed", "-c", "user.email=seed@aiarch.local"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return "file://" + dir
}

type pinRig struct {
	t  *testing.T
	ae ActivityExecutionAccess
	rc fwra.Context
	n  int
}

func (r *pinRig) key() fwra.IdempotencyKey {
	r.n++
	return fwra.IdempotencyKey(fmt.Sprintf("k-%d", r.n))
}

func (r *pinRig) open(pin LifecyclePin) error {
	_, err := r.ae.OpenActivity(r.rc, pinProject, NoActivityVersionExpectation, "A", ActivityTypeService, TestVariantNone, pin, RepoCredential{}, r.key())
	return err
}

func (r *pinRig) row() ActivityExecution {
	r.t.Helper()
	row, err := r.ae.ReadActivityExecution(r.rc, pinProject, "A")
	if err != nil {
		r.t.Fatal(err)
	}
	return row
}

func (r *pinRig) must(_ Version, err error) {
	r.t.Helper()
	if err != nil {
		r.t.Fatal(err)
	}
}

// exitAndRequeue records one attempt on task, fails the activity, and requeues it.
func (r *pinRig) exitAndRequeue(task MethodTask) {
	r.t.Helper()
	r.must(r.ae.RecordAttemptOutcome(r.rc, pinProject, NoActivityVersionExpectation, "A",
		TaskAttemptInput{AttemptID: "att-1", TaskID: task, Attempt: 1, Outcome: OutcomePassed}, RepoCredential{}, r.key()))
	r.must(r.ae.RecordActivityOutcome(r.rc, pinProject, NoActivityVersionExpectation, "A", ActivityOutcomeUnknown, PipelineFailed, "failed", RepoCredential{}, r.key()))
	r.must(r.ae.RecordOperatorNote(r.rc, pinProject, NoActivityVersionExpectation, "A",
		OperatorNoteInput{NoteID: "n-1", Kind: NoteRequeue, Gate: "construction", Text: "change request"}, "", RepoCredential{}, r.key()))
}

func newPinRig(t *testing.T) *pinRig {
	t.Helper()
	return &pinRig{t: t, ae: NewGitLocalActivityExecutionAccess(pinRepo(t)), rc: fwra.Context{Context: context.Background()}}
}

func TestOpenActivity_RepinsARequeuedRowAndRecordsHistory(t *testing.T) {
	r := newPinRig(t)
	if err := r.open(pinOld()); err != nil {
		t.Fatal(err)
	}
	r.exitAndRequeue(pinTestTask(t))
	if err := r.open(pinNew()); err != nil {
		t.Fatalf("a requeued row must re-pin on re-open: %v", err)
	}
	row := r.row()
	if row.Pin == nil || *row.Pin != pinNew() || len(row.PinHistory) != 1 || row.PinHistory[0].Pin != pinOld() {
		t.Fatalf("pin %v history %v", row.Pin, row.PinHistory)
	}
}

func TestOpenActivity_RefusesToRepinALiveRow(t *testing.T) {
	r := newPinRig(t)
	if err := r.open(pinOld()); err != nil {
		t.Fatal(err)
	}
	err := r.open(pinNew())
	var ra *fwra.Error
	if err == nil || !errors.As(err, &ra) || ra.Kind != fwra.ContractMisuse {
		t.Fatalf("a live row must refuse a re-pin as ContractMisuse, got %v", err)
	}
	if row := r.row(); *row.Pin != pinOld() || len(row.PinHistory) != 0 {
		t.Fatalf("a refused re-pin must change nothing: %v %v", row.Pin, row.PinHistory)
	}
}

func TestOpenActivity_RepinWithATaskMissingFromTheNewDAGIsAConflict(t *testing.T) {
	r := newPinRig(t)
	if err := r.open(pinOld()); err != nil {
		t.Fatal(err)
	}
	r.exitAndRequeue("no-such-task")
	err := r.open(pinNew())
	var ra *fwra.Error
	if err == nil || !errors.As(err, &ra) || ra.Kind != fwra.Conflict || !strings.Contains(err.Error(), "no-such-task") {
		t.Fatalf("want a Conflict naming the missing task, got %v", err)
	}
	if row := r.row(); *row.Pin != pinOld() || len(row.PinHistory) != 0 {
		t.Fatalf("a refused re-pin must change nothing: %v %v", row.Pin, row.PinHistory)
	}
}
