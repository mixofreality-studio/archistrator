package delivery

import (
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// TestLifecyclePinFor_StampsTheDAGIdentityNotTheBuildVersion pins the stamp the live
// dispatcher writes (deliveryactivity.go OpenActivity -> lifecyclePinFor). The pin must be a
// content identity of the resolved lifecycle DAG, so a GOWORK=off build (vX.Y.Z), a workspace
// build ((devel)) and a release all agree on it; the method-assets module version is
// build-dependent and would force a re-pin on every move between builds.
func TestLifecyclePinFor_StampsTheDAGIdentityNotTheBuildVersion(t *testing.T) {
	act := constructionActivity{Type: projectstate.ActivityTypeService}
	pin := lifecyclePinFor(act)
	key := projectstate.LifecycleKeyFor(act.Type, act.Variant)
	if pin.TypeKey != key {
		t.Fatalf("typeKey %q, want %q", pin.TypeKey, key)
	}
	if want := projectstate.LifecycleIdentity(key); pin.AssetsVersion != want {
		t.Fatalf("assetsVersion %q, want the lifecycle DAG identity %q", pin.AssetsVersion, want)
	}
	if !strings.HasPrefix(pin.AssetsVersion, "dag-sha256:") {
		t.Fatalf("assetsVersion %q is not a DAG content identity", pin.AssetsVersion)
	}
}
