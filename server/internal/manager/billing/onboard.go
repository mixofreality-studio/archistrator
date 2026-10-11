package billing

import (
	"encoding/json"
	"fmt"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/billingstate"
	"github.com/mixofreality-studio/archistrator/server/internal/utility/messagebus"
)

// ===========================================================================
// OnboardWorkflow — op 2.1 entry (UC5 operator-initiated payment-integration onboard).
// ===========================================================================

// OnboardInput is the start payload for OnboardWorkflow.
type onboardInput struct {
	DeployedAppID deployedAppID
}

// OnboardWorkflow drives UC5 onboard (billingManager.md §6.3; use case
// onboard-a-new-customer):
//  1. ReadBillingActivity → resolves deployedAppId → customerId + terms.
//  2. registered [no]: the customer has no billing record yet. Onboarding stops here and
//     answers the customer's BillingRef: the next step is the customer's own —
//     registering the payment instrument (RegisterCustomer), which validates it, opens
//     the record and then completes the onboarding below.
//  3. registered [yes]: completeOnboarding — BindGatewayLiveActivity (head-state;
//     Conflict loop) and the per-customer closeBillingCycle:<customerId> Schedule, read
//     back through DescribeSchedule.
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
		return BillingRef{}, err
	}
	if !billing.Registered {
		// billingStateAccess reads a customer it never opened as Registered=false.
		logger.Info("payment onboarding awaits customer registration",
			"customerId", billing.ID.String(), "deployedAppId", in.DeployedAppID.String())
		return BillingRef{CustomerID: billing.ID}, nil
	}
	if cerr := wf.completeOnboarding(ctx, billing); cerr != nil {
		return BillingRef{}, cerr
	}

	logger.Info("payment integration onboarded", "customerId", billing.ID.String(), "deployedAppId", in.DeployedAppID.String())
	return BillingRef{CustomerID: billing.ID}, nil
}

// completeOnboarding is the tail both onboarding entries share (use case
// onboard-a-new-customer, merge-reg → bind-gateway → register-schedule): bind the
// gateway customer reference unless the record is already bound, then register the
// per-customer cycle-close Schedule and read it back. billing is the registered record
// as last read or written.
func (wf *workflows) completeOnboarding(ctx workflow.Context, billing billingstate.Billing) error {
	customerID := billing.ID

	// Bind the gateway customer reference (head-state; Conflict loop). Charge-only: there
	// is no connected-account creation step (the platform never pays out); the gateway
	// customer reference is the customer's own stable id, which the charge rail
	// (chargeCustomer) keys on, and bindGatewayLive flips GatewayBound.
	if !billing.GatewayBound {
		if _, berr := wf.bindGatewayLive(ctx, customerID, billing.Version, gatewayBindingFor(customerID)); berr != nil {
			return berr
		}
	}

	// Register the per-customer cycle-close Schedule (idempotent by id), then read it back.
	if rerr := wf.registerCloseSchedule(ctx, customerID); rerr != nil {
		return rerr
	}
	schedule, derr := wf.Acts.MessageBusDescribeSchedule(ctx, closeScheduleID(customerID))
	if derr != nil {
		return derr
	}
	if !schedule.Registered {
		return temporal.NewNonRetryableApplicationError(
			"the settlement-cycle schedule did not land: "+string(closeScheduleID(customerID))+" reads back unregistered",
			fwmgr.ErrType(fwmgr.FailedPrecondition), nil)
	}
	if schedule.ExecutionKind != messagebus.ExecutionKind(executionKindClose) || schedule.TargetIDTemplate != closeTargetID(customerID) {
		return temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("the settlement-cycle schedule %s reads back as %q starting %q, not as registered (%q starting %q)",
				closeScheduleID(customerID), schedule.ExecutionKind, schedule.TargetIDTemplate, executionKindClose, closeTargetID(customerID)),
			fwmgr.ErrType(fwmgr.FailedPrecondition), nil)
	}
	return nil
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

// gatewayBindingFor builds the head-state GatewayBinding for onboarding. Under the
// charge-only model there is no connected-account creation call (the retired
// merchantGatewayAccess.createConnectedAccount); the gateway customer reference is the
// customer's own stable id, which the charge rail keys on.
func gatewayBindingFor(customerID customerID) billingstate.GatewayBinding {
	return billingstate.GatewayBinding{GatewayCustomerRef: customerID.String()}
}

func (wf *workflows) bindGatewayLive(ctx workflow.Context, customerID customerID, seed billingstate.Version, binding billingstate.GatewayBinding) (billingstate.Version, error) {
	return wf.applyRecovering(ctx, customerID, seed, func(expected billingstate.Version) (billingstate.Version, error) {
		return wf.Acts.BillingStateBindGatewayLive(ctx, customerID, expected, binding)
	})
}

// registerCloseSchedule invokes messageBus.registerSchedule for the
// per-customer closeBillingCycle:<customerId> Schedule (idempotent by id; op 2.1). The
// KindBinding table resolves the task queue, so it is not threaded.
//
// A firing must start a close that knows whose cycle it closes, and which one. A
// Schedule carries the SAME start argument on every firing, so the spec carries what is
// fixed — the customer, as the close's start payload, and the per-customer target id —
// and the cycle is what the firing derives: the calendar month that has just elapsed
// (CloseCycleWorkflow, elapsedCycle). The cadence is therefore the start of every
// calendar month, the boundary every cycle id ("2006-01") names.
func (wf *workflows) registerCloseSchedule(ctx workflow.Context, customerID customerID) error {
	payload, err := json.Marshal(closeInput{CustomerID: customerID})
	if err != nil {
		return temporal.NewNonRetryableApplicationError(
			"encode the cycle-close schedule's start payload: "+err.Error(),
			fwmgr.ErrType(fwmgr.ContractMisuse), nil)
	}
	return wf.Acts.MessageBusRegisterSchedule(ctx,
		closeScheduleID(customerID),
		messagebus.ScheduleSpec{
			ExecutionKind:    messagebus.ExecutionKind(executionKindClose),
			Cadence:          messagebus.Cadence{CronExpr: closeCycleCron},
			TargetIDTemplate: closeTargetID(customerID),
			StartPayload:     messagebus.ExecutionPayload{Bytes: payload},
		})
}

// closeTargetID is the id a scheduled firing starts the customer's close under
// ({customerId}:close; the runtime suffixes each firing's time).
func closeTargetID(customerID customerID) string {
	return fmt.Sprintf("%s:close", customerID)
}

// closeScheduleID is the per-customer closeBillingCycle:<customerId> Schedule id.
func closeScheduleID(customerID customerID) messagebus.ScheduleID {
	return messagebus.ScheduleID(fmt.Sprintf("%s:%s", scheduleIDCloseCyclePrefix, customerID))
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
