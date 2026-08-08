package operatedruntime_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedruntime"
)

func rc() fwra.Context { return fwra.Context{Context: context.Background()} }

// TestUnknownProfileFailsFast: an unset profile is a construction-time ContractMisuse.
func TestUnknownProfileFailsFast(t *testing.T) {
	_, err := operatedruntime.NewProfiledOperatedRuntimeAccess(operatedruntime.RuntimeProfileUnknown, operatedruntime.RuntimeConfig{})
	var e *fwra.Error
	if !errors.As(err, &e) || e.Kind != fwra.ContractMisuse {
		t.Fatalf("unknown profile: want ContractMisuse, got %v", err)
	}
}

// TestLocalProfileDeterministic: the LOCAL/dry-run profile accepts writes as no-ops,
// reports a deterministic Healthy/SLO-met snapshot, and invents no usage facts.
func TestLocalProfileDeterministic(t *testing.T) {
	rt, err := operatedruntime.NewProfiledOperatedRuntimeAccess(operatedruntime.RuntimeProfileLocal, operatedruntime.RuntimeConfig{})
	if err != nil {
		t.Fatalf("local build: %v", err)
	}
	app := uuid.New()

	if err := rt.PublishDesiredState(rc(), app, operatedruntime.RuntimeDesiredState{}, "k1"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := rt.Withdraw(rc(), app, "k2"); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if err := rt.WirePaymentConfig(rc(), app, operatedruntime.GatewayBinding{ConnectedAccountID: "acct"}, "k3"); err != nil {
		t.Fatalf("wire: %v", err)
	}

	health, err := rt.GetApplicationHealth(rc(), app)
	if err != nil || health != operatedruntime.RuntimeStatusHealthy {
		t.Fatalf("health = %v, err = %v; want Healthy", health, err)
	}
	slo, err := rt.GetSloStatus(rc(), app)
	if err != nil || !slo.SloMet {
		t.Fatalf("slo = %+v, err = %v; want SloMet", slo, err)
	}
	attr, err := rt.ReadComputeAttribution(rc(), app, operatedruntime.AttributionWindow{})
	if err != nil {
		t.Fatalf("attribution: %v", err)
	}
	if attr.RuntimeEventID != "" {
		t.Fatalf("dry-run fabricated a usage fact: %+v", attr)
	}
}

// TestRealProfileExplicitNotImplemented: the REAL profile constructs (server still boots)
// but every verb returns an explicit, diagnosable error naming the follow-up — NOT a
// silent generated stub, and it preserves the non-retryable wire behaviour.
func TestRealProfileExplicitNotImplemented(t *testing.T) {
	rt, err := operatedruntime.NewProfiledOperatedRuntimeAccess(operatedruntime.RuntimeProfileReal, operatedruntime.RuntimeConfig{})
	if err != nil {
		t.Fatalf("real build should not fail at construction (preserves boot): %v", err)
	}
	app := uuid.New()

	perr := rt.PublishDesiredState(rc(), app, operatedruntime.RuntimeDesiredState{}, "k")
	var e *fwra.Error
	if !errors.As(perr, &e) {
		t.Fatalf("real publish: want *fwra.Error, got %T %v", perr, perr)
	}
	if e.Retryable {
		t.Fatalf("real-profile error must be non-retryable (fail-fast wire behaviour), got retryable")
	}
	if _, herr := rt.GetApplicationHealth(rc(), app); herr == nil {
		t.Fatalf("real getApplicationHealth: want explicit error, got nil")
	}
}
