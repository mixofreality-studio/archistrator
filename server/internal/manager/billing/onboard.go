package billing

import (
	"errors"
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/billingstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/durableexecution"
)

// ===========================================================================
// OnboardWorkflow — op 2.1 entry (UC5 operator-initiated payment-integration onboard).
// ===========================================================================

// OnboardInput is the start payload for OnboardWorkflow.
type onboardInput struct {
	DeployedAppID deployedAppID
}

// OnboardWorkflow drives UC5 onboard (billingManager.md §6.3):
//  1. ReadBillingActivity → resolves deployedAppId → customerId + terms/payout.
//  2. CreateConnectedAccountActivity (merchantGatewayAccess).
//  3. BindGatewayLiveActivity (head-state; Conflict loop).
//  4. RegisterScheduleActivity (the per-customer closeBillingCycle:<customerId> Schedule).
//
// (Runtime payment-config wiring is NOT a billing step: publishing desired state into
// the operated runtime is OperationsManager's publishDesiredState concern — the
// declared architecture gives billing no operatedRuntimeAccess edge; per operational
// concept #2 billing reaches operations only via the queued applyDelinquencyPolicy
// signal.)
func (wf *workflows) OnboardWorkflow(ctx workflow.Context, in onboardInput) (BillingRef, error) {
	logger := workflow.GetLogger(ctx)

	// Resolve the billing aggregate. The head-state row carries the customerId the
	// deployed app settles under (§3.0 / UC5 line 683).
	billing, err := wf.readBillingByDeployedApp(ctx, in.DeployedAppID)
	if err != nil {
		// A missing billing aggregate violates the UC5 pre-condition ("the deployed
		// app exists and is operated"); surface it as a terminal FailedPrecondition.
		if isReadNotFound(err) {
			return BillingRef{}, temporal.NewNonRetryableApplicationError(
				"no billing aggregate for the deployed app (not registered/operated)",
				fwmgr.ManagerErrType(fwmgr.FailedPrecondition), nil)
		}
		return BillingRef{}, err
	}
	customerID := billing.ID

	// Create the merchant connected account (external gateway).
	binding, gerr := wf.createConnectedAccount(ctx, customerID)
	if gerr != nil {
		return BillingRef{}, gerr
	}

	// Record the binding (head-state; Conflict loop).
	if _, berr := wf.bindGatewayLive(ctx, customerID, billing.Version, binding); berr != nil {
		return BillingRef{}, berr
	}

	// Register the per-customer cycle-close Schedule (idempotent by id).
	if rerr := wf.registerCloseSchedule(ctx, customerID); rerr != nil {
		return BillingRef{}, rerr
	}

	logger.Info("payment integration onboarded", "customerId", customerID.String(), "deployedAppId", in.DeployedAppID.String())
	return BillingRef{CustomerID: customerID}, nil
}

// Shared workflow-context helper (used by 2 workflows); lives in its first caller's file per the file-layout standard.
// readBilling invokes billingStateAccess.readBilling. The workflow speaks the
// generated billingstate.Billing contract type directly — no Manager-local mirror.
func (wf *workflows) readBilling(ctx workflow.Context, customerID customerID) (billingstate.Billing, error) {
	return wf.Acts.BillingStateReadBilling(ctx, customerID)
}

// readBillingByDeployedApp resolves a deployedAppId to its billing aggregate
// (UC5 onboarding). The head-state RA keys on customerId; the onboarding read carries
// the deployedAppId so the RA resolves the owning customer. Modelled as the same
// readBilling over the deployedApp's resolved customer; here the deployedApp id IS the
// resolution input the RA maps to the customer aggregate.
func (wf *workflows) readBillingByDeployedApp(ctx workflow.Context, deployedAppID deployedAppID) (billingstate.Billing, error) {
	// The billing aggregate is per-customer; the onboarding RA read resolves the
	// owning customer from the deployed app. We pass the deployedAppId as the read key;
	// the RA returns the customer's Billing (ID = customerId).
	return wf.readBilling(ctx, deployedAppID)
}

// createConnectedAccount invokes merchantGatewayAccess.createConnectedAccount (caller-
// keyed onboard:{id}) and folds the merchantgateway-owned binding onto the
// billingstate-owned GatewayBinding the head-state write (bindGatewayLive) persists —
// two distinct generated types, same shape (ConnectedAccountID string).
func (wf *workflows) createConnectedAccount(ctx workflow.Context, customerID customerID) (billingstate.GatewayBinding, error) {
	key := fmt.Sprintf("onboard:%s", customerID)
	b, err := wf.Acts.MerchantGatewayCreateConnectedAccount(ctx, fwra.IdempotencyKey(key), customerID, key)
	if err != nil {
		return billingstate.GatewayBinding{}, err
	}
	return billingstate.GatewayBinding{ConnectedAccountID: b.ConnectedAccountID}, nil
}

func (wf *workflows) bindGatewayLive(ctx workflow.Context, customerID customerID, seed billingstate.Version, binding billingstate.GatewayBinding) (billingstate.Version, error) {
	return wf.applyRecovering(ctx, customerID, seed, func(expected billingstate.Version) (billingstate.Version, error) {
		return wf.Acts.BillingStateBindGatewayLive(ctx, customerID, expected, binding)
	})
}

// registerCloseSchedule invokes durableExecutionAccess.registerSchedule for the
// per-customer closeBillingCycle:<customerId> Schedule (idempotent by id; op 2.1). The
// KindBinding table resolves the task queue, so it is not threaded.
func (wf *workflows) registerCloseSchedule(ctx workflow.Context, customerID customerID) error {
	return wf.Acts.DurableExecutionRegisterSchedule(ctx,
		durableexecution.ScheduleID(fmt.Sprintf("%s:%s", scheduleIDCloseCyclePrefix, customerID)),
		durableexecution.ScheduleSpec{
			ExecutionKind: durableexecution.ExecutionKind(executionKindClose),
			Cadence:       durableexecution.Cadence{Every: time.Duration(closeCycleDefaultIntervalSecs) * time.Second},
		})
}

// Shared workflow-context helper (used by 3 workflows); lives in its first caller's file per the file-layout standard.
// applyRecovering executes one head-state mutation Activity with a workflow-level
// Conflict re-read→re-apply loop (§6.5; identical discipline to operations/construction).
// On a stale-version fwra.Conflict it re-reads the true head Version and re-applies with
// the SAME idempotency key (dedup-first ordering preserves idempotent replay of the
// money-affecting write).
func (wf *workflows) applyRecovering(
	ctx workflow.Context,
	customerID customerID,
	seed billingstate.Version,
	apply func(expected billingstate.Version) (billingstate.Version, error),
) (billingstate.Version, error) {
	expected := seed
	for attempt := 0; ; attempt++ {
		v, err := apply(expected)
		if err == nil {
			return v, nil
		}
		if !isConflict(err) {
			return 0, err
		}
		if attempt+1 >= maxMutateConflictAttempts {
			return 0, temporal.NewNonRetryableApplicationError(
				"billing head-state conflict did not converge within bounded attempts",
				"MutateConflictExhausted", err)
		}
		s, rerr := wf.readBilling(ctx, customerID)
		if rerr != nil {
			return 0, rerr
		}
		expected = s.Version
		workflow.GetLogger(ctx).Info("billing head-state conflict; re-read version and retrying",
			"attempt", attempt+1, "nextExpectedVersion", expected)
	}
}

// isReadNotFound reports whether err is a head-state read's NotFound.
func isReadNotFound(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == raNotFoundErrType
	}
	return false
}

// raNotFoundErrType is the canonical Temporal Type() ReadBilling surfaces for a
// missing billing aggregate.
var raNotFoundErrType = fwmgr.RAErrType(fwra.NotFound)
