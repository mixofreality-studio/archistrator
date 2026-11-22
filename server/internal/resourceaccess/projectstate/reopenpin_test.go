package projectstate

import (
	"errors"
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
