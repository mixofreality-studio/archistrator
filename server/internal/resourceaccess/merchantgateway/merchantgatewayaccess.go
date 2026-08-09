// Package merchantgateway is the merchantGatewayAccess component: the Stripe
// vendor-account gateway resource access.
//
// This file is hand-written (NOT generated) and stays alongside contract.gen.go.
// The generated stubMerchantGatewayAccess (contract.gen.go) already fails every op —
// it is not a silent-success fake — but it does so with fwra.Unknown, "not
// implemented": a Kind gatewayActivityOptions (server/internal/manager/billing)
// does NOT list in its TerminalRA set, so the workflow burns its full retry budget
// (MaxAttempts: 3) before failing anyway, and the message gives the operator no path
// forward. That is dishonest about WHY the op fails: this isn't an unimplemented
// contract, it's a real, wired component (manager/billing, engine/billing,
// resourceaccess/billingstate are all real) whose one remaining dependency — a
// live Stripe credential — hasn't been provisioned yet.
//
// notConfiguredMerchantGatewayAccess replaces the generated stub (wired via
// hooks.FinalizeMerchantGatewayAccess, the one seam composegen leaves for this)
// with an explicit, terminal fwra.Auth error: no wasted retries, and a message
// pointing at docs/billing-setup.md for how to provision Stripe and swap this
// out for a live gateway client.
package merchantgateway

import (
	"github.com/google/uuid"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// notConfiguredDetail is shared by every op so the message is identical regardless
// of which call surfaced it.
const notConfiguredDetail = "merchantgateway: Stripe not configured — see docs/billing-setup.md"

// notConfiguredMerchantGatewayAccess is the honest not-configured
// MerchantGatewayAccess: every op fails terminally with a message that tells the
// operator exactly what to do about it.
type notConfiguredMerchantGatewayAccess struct{}

// NewNotConfiguredMerchantGatewayAccess returns the not-configured
// MerchantGatewayAccess. Composed in place of the generated stub via
// hooks.FinalizeMerchantGatewayAccess.
func NewNotConfiguredMerchantGatewayAccess() MerchantGatewayAccess {
	return &notConfiguredMerchantGatewayAccess{}
}

var _ MerchantGatewayAccess = (*notConfiguredMerchantGatewayAccess)(nil)

// ChargeCustomer fails terminally: fwra.Auth is in gatewayActivityOptions'
// TerminalRA set (manager/billing/billingmanager.go), so the workflow does not
// retry a call that cannot succeed until Stripe is configured.
func (*notConfiguredMerchantGatewayAccess) ChargeCustomer(_ fwra.Context, _ uuid.UUID, _ Money, _ string) error {
	return fwra.New(fwra.Auth, notConfiguredDetail)
}

// ValidateStoredInstrument fails terminally for the same reason as ChargeCustomer.
func (*notConfiguredMerchantGatewayAccess) ValidateStoredInstrument(_ fwra.Context, _ uuid.UUID, _ string) error {
	return fwra.New(fwra.Auth, notConfiguredDetail)
}
