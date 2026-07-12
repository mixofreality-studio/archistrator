package billing

import (
	"fmt"

	"go.temporal.io/sdk/workflow"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/billingstate"
)

// ===========================================================================
// RegisterCustomerWorkflow — op 2.2 entry (ncuc1 open the billing aggregate).
// ===========================================================================

// RegisterInput is the start payload for RegisterCustomerWorkflow.
type registerInput struct {
	CustomerID customerID
}

// RegisterCustomerWorkflow drives ncuc1 (billingManager.md §6.3):
//  1. ValidateStoredInstrumentActivity (merchantGatewayAccess; zero-amount auth).
//  2. RegisterCustomerActivity (head-state; opens the aggregate; Conflict loop).
func (wf *workflows) RegisterCustomerWorkflow(ctx workflow.Context, in registerInput) (BillingRef, error) {
	logger := workflow.GetLogger(ctx)

	if verr := wf.validateStoredInstrument(ctx, in.CustomerID); verr != nil {
		return BillingRef{}, verr
	}

	// Open the aggregate. A fresh registration seeds expectedVersion 0; the Conflict
	// loop recovers a racing register.
	if _, rerr := wf.registerCustomer(ctx, in.CustomerID, 0); rerr != nil {
		return BillingRef{}, rerr
	}

	logger.Info("customer registered", "customerId", in.CustomerID.String())
	return BillingRef(in), nil
}

// validateStoredInstrument invokes merchantGatewayAccess.validateStoredInstrument (the
// zero-amount registration auth; caller-keyed validate:{id}).
func (wf *workflows) validateStoredInstrument(ctx workflow.Context, customerID customerID) error {
	key := fmt.Sprintf("validate:%s", customerID)
	return wf.Acts.MerchantGatewayValidateStoredInstrument(ctx, fwra.IdempotencyKey(key), customerID, key)
}

func (wf *workflows) registerCustomer(ctx workflow.Context, customerID customerID, seed billingstate.Version) (billingstate.Version, error) {
	return wf.applyRecovering(ctx, customerID, seed, func(expected billingstate.Version) (billingstate.Version, error) {
		return wf.Acts.BillingStateRegisterCustomer(ctx, customerID, expected, billingstate.CustomerProfile{})
	})
}
