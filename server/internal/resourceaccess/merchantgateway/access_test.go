package merchantgateway

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

func rc() fwra.Context { return fwra.Context{Context: context.Background()} }

// TestNotConfiguredChargeCustomer asserts the not-configured gateway fails
// ChargeCustomer terminally (fwra.Auth — in gatewayActivityOptions' TerminalRA set)
// with a message that points the operator at docs/billing-setup.md, instead of the
// generated stub's untargeted fwra.Unknown "not implemented".
func TestNotConfiguredChargeCustomer(t *testing.T) {
	access := NewNotConfiguredMerchantGatewayAccess()

	err := access.ChargeCustomer(rc(), uuid.New(), Money{MinorUnits: 100, Currency: "USD"}, "k1")

	var e *fwra.Error
	if !errors.As(err, &e) {
		t.Fatalf("ChargeCustomer: want *fwra.Error, got %T (%v)", err, err)
	}
	if e.Kind != fwra.Auth {
		t.Fatalf("ChargeCustomer: want Kind=Auth, got %v", e.Kind)
	}
	if !strings.Contains(e.Detail, "docs/billing-setup.md") {
		t.Fatalf("ChargeCustomer: want Detail referencing docs/billing-setup.md, got %q", e.Detail)
	}
}

// TestNotConfiguredValidateStoredInstrument mirrors TestNotConfiguredChargeCustomer
// for the other op.
func TestNotConfiguredValidateStoredInstrument(t *testing.T) {
	access := NewNotConfiguredMerchantGatewayAccess()

	err := access.ValidateStoredInstrument(rc(), uuid.New(), "k2")

	var e *fwra.Error
	if !errors.As(err, &e) {
		t.Fatalf("ValidateStoredInstrument: want *fwra.Error, got %T (%v)", err, err)
	}
	if e.Kind != fwra.Auth {
		t.Fatalf("ValidateStoredInstrument: want Kind=Auth, got %v", e.Kind)
	}
	if !strings.Contains(e.Detail, "docs/billing-setup.md") {
		t.Fatalf("ValidateStoredInstrument: want Detail referencing docs/billing-setup.md, got %q", e.Detail)
	}
}
