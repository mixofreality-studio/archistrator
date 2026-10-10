package billing

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	billingengine "github.com/mixofreality-studio/archistrator/server/internal/engine/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/billingstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// ===========================================================================
// RegisterCustomerWorkflow — op 2.2 entry (ncuc1 open the billing aggregate).
// ===========================================================================

// RegisterInput is the start payload for RegisterCustomerWorkflow.
type registerInput struct {
	CustomerID customerID
	ProjectID  string
}

// RegisterCustomerWorkflow drives ncuc1 (billingManager.md §6.3):
//  1. ValidateStoredInstrumentActivity (merchantGatewayAccess; zero-amount auth), read
//     back through ReadStoredInstrument: a customer whose instrument the gateway does
//     not hold as validated is never registered.
//  2. The terms (DCT §12 B13): the project's committed SDP option's compute-cost regime,
//     read through projectStateAccess and validated by the billingEngine
//     (ProjectCommitTimeComputeCost refuses an unknown regime), in the project's cost
//     currency — billing reads the commitment, it never asks another Manager.
//  3. RegisterCustomerActivity (head-state; opens the aggregate under those terms;
//     Conflict loop).
func (wf *workflows) RegisterCustomerWorkflow(ctx workflow.Context, in registerInput) (BillingRef, error) {
	logger := workflow.GetLogger(ctx)

	if verr := wf.validateStoredInstrument(ctx, in.CustomerID); verr != nil {
		return BillingRef{}, verr
	}
	instrument, ierr := wf.Acts.MerchantGatewayReadStoredInstrument(ctx, in.CustomerID)
	if ierr != nil {
		return BillingRef{}, ierr
	}
	if !instrument.Validated {
		return BillingRef{}, temporal.NewNonRetryableApplicationError(
			"the stored instrument did not validate: the gateway holds no validated instrument for the customer",
			fwmgr.ErrType(fwmgr.FailedPrecondition), nil)
	}

	terms, terr := wf.committedTerms(ctx, in.ProjectID)
	if terr != nil {
		return BillingRef{}, terr
	}

	// Open the aggregate. A fresh registration seeds expectedVersion 0; the Conflict
	// loop recovers a racing register.
	profile := billingstate.CustomerProfile{ProjectID: in.ProjectID, Terms: terms}
	if _, rerr := wf.registerCustomer(ctx, in.CustomerID, 0, profile); rerr != nil {
		return BillingRef{}, rerr
	}

	logger.Info("customer registered", "customerId", in.CustomerID.String(), "projectId", in.ProjectID)
	return BillingRef{CustomerID: in.CustomerID}, nil
}

// committedTerms are the terms a customer of projectID is billed under (DCT §12 B13):
// the compute-cost regime, markup and schedule every option of the project's committed
// SDP review is priced under (its planning assumptions' terms), validated by the
// billingEngine's commit-time projection, in the project's cost currency. A project with
// no committed SDP review has no terms to bill under: FailedPrecondition.
func (wf *workflows) committedTerms(ctx workflow.Context, projectID string) (billingstate.BillingTerms, error) {
	proj, err := wf.Acts.ProjectStateReadProject(ctx, projectstate.ProjectID(projectID))
	if err != nil {
		return billingstate.BillingTerms{}, err
	}
	refuse := func(why string) (billingstate.BillingTerms, error) {
		return billingstate.BillingTerms{}, temporal.NewNonRetryableApplicationError(
			"project "+projectID+" "+why, fwmgr.ErrType(fwmgr.FailedPrecondition), nil)
	}
	sdp, ok := proj.SdpReview.Model.(*projectstate.SdpReview)
	if proj.SdpReview.Status != projectstate.ReviewCommitted || !ok || sdp == nil {
		return refuse("has no committed SDP option to bill under")
	}
	pa, ok := proj.PlanningAssumptions.Model.(*projectstate.PlanningAssumptions)
	if !ok || pa == nil {
		return refuse("has no planning assumptions carrying its billing terms")
	}
	if pa.IndirectDailyRate.Currency == "" {
		return refuse("names no cost currency to bill in")
	}
	projection, perr := wf.Billing.ProjectCommitTimeComputeCost(fweng.Context{Context: context.Background()},
		billingengine.ProjectOption{
			OptionID: billingengine.OptionID(sdp.Recommendation),
			Terms: billingengine.BillingTerms{
				ComputeCost:          billingengine.ComputeCostKind(pa.Terms.ComputeCost),
				ComputeMarkupPercent: pa.Terms.ComputeMarkupPercent,
				Schedule:             billingengine.ScheduleKind(pa.Terms.Schedule),
			},
		})
	if perr != nil {
		return billingstate.BillingTerms{}, fwmgr.MapError(perr)
	}
	return billingstate.BillingTerms{
		ComputeCostKind:      int64(projection.ComputeCostKind),
		ComputeMarkupPercent: projection.ComputeMarkupPercent,
		ScheduleKind:         int64(pa.Terms.Schedule),
		Currency:             pa.IndirectDailyRate.Currency,
	}, nil
}

// validateStoredInstrument invokes merchantGatewayAccess.validateStoredInstrument (the
// zero-amount registration auth; caller-keyed validate:{id}).
func (wf *workflows) validateStoredInstrument(ctx workflow.Context, customerID customerID) error {
	key := fmt.Sprintf("validate:%s", customerID)
	return wf.Acts.MerchantGatewayValidateStoredInstrument(ctx, fwra.IdempotencyKey(key), customerID, key)
}

func (wf *workflows) registerCustomer(ctx workflow.Context, customerID customerID, seed billingstate.Version, profile billingstate.CustomerProfile) (billingstate.Version, error) {
	return wf.applyRecovering(ctx, customerID, seed, func(expected billingstate.Version) (billingstate.Version, error) {
		return wf.Acts.BillingStateRegisterCustomer(ctx, customerID, expected, profile)
	})
}
