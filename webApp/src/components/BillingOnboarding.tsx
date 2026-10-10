/**
 * BillingOnboarding — the web entries of use case onboard-a-new-customer (spec §12
 * B7). Two steps, in the use case's order:
 *
 *   1. Open payment onboarding for a deployed app (OpenPaymentOnboarding →
 *      billingManager.OnboardPaymentIntegration): the app resolves to its billing
 *      customer, whose id the step shows and hands to step 2.
 *   2. Register the customer's payment instrument (RegisterPaymentInstrument →
 *      billingManager.RegisterCustomer) for the project it is billed for: the stored
 *      instrument is validated with a zero-amount authorization and the customer's
 *      billing record opens under that project's committed terms (DCT §12 B13).
 *
 * Each step reports what its op answered — the customer's billing reference — or the
 * refusal verbatim. Pure: the container owns both mutations and hands their results
 * in as `onboard` / `register`.
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import TextField from '@mui/material/TextField';
import Button from '@mui/material/Button';
import { useTokens } from '../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../utilities/constants/UIIdentifiers';

/** What one onboarding step's op said. */
export type OnboardingStepResult =
  | { kind: 'pending' }
  | { kind: 'done'; customerId: string }
  | { kind: 'failed'; message: string };

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function BillingOnboarding({
  onboard,
  register,
  onOnboard,
  onRegister,
}: {
  onboard: OnboardingStepResult | undefined;
  register: OnboardingStepResult | undefined;
  onOnboard: (deployedAppId: string) => void;
  onRegister: (customerId: string, projectId: string) => void;
}): ReactNode {
  const t = useTokens();
  const [appId, setAppId] = useState('');
  // Step 2's customer: typed, or — once step 1 answered and nothing was typed — the
  // customer step 1 resolved the app to.
  const [typedCustomer, setTypedCustomer] = useState<string | undefined>(undefined);
  const customerId = typedCustomer ?? (onboard?.kind === 'done' ? onboard.customerId : '');

  const [projectId, setProjectId] = useState('');

  const appValid = UUID.test(appId.trim());
  const customerValid = UUID.test(customerId.trim());
  const projectValid = projectId.trim() !== '';

  return (
    <Paper data-testid={UI_IDENTIFIERS.Billing.ONBOARDING} sx={{ p: 2.5, mb: 3 }}>
      <Typography sx={{ color: t.ink, fontFamily: t.display, fontWeight: 700, fontSize: 16 }}>
        Payment onboarding
      </Typography>
      <Typography sx={{ color: t.muted, fontSize: 13, mb: 2, lineHeight: 1.5 }}>
        Open payment onboarding for a deployed app, then register the customer&apos;s payment
        instrument. Registration validates the stored instrument with a zero-amount authorization —
        nothing is charged.
      </Typography>

      <Box
        component="form"
        sx={{ display: 'flex', gap: 1.5, flexWrap: 'wrap', alignItems: 'flex-start' }}
        onSubmit={(e) => {
          e.preventDefault();
          if (appValid && onboard?.kind !== 'pending') onOnboard(appId.trim());
        }}
      >
        <TextField
          error={appId !== '' && !appValid}
          helperText={appId !== '' && !appValid ? 'A deployed app id is a UUID' : ' '}
          label="Deployed app id"
          size="small"
          slotProps={{ htmlInput: { 'data-testid': UI_IDENTIFIERS.Billing.ONBOARD_APP_ID_INPUT } }}
          sx={{ minWidth: 320 }}
          value={appId}
          onChange={(e) => {
            setAppId(e.target.value);
          }}
        />
        <Button
          data-testid={UI_IDENTIFIERS.Billing.ONBOARD_SUBMIT}
          disabled={!appValid || onboard?.kind === 'pending'}
          type="submit"
          variant="contained"
        >
          Open payment onboarding
        </Button>
      </Box>
      <StepResult
        doneCopy="Onboarding opened for customer"
        refusedTestId={UI_IDENTIFIERS.Billing.ONBOARD_REFUSED}
        result={onboard}
        testId={UI_IDENTIFIERS.Billing.ONBOARD_RESULT}
      />

      <Box
        component="form"
        sx={{ display: 'flex', gap: 1.5, flexWrap: 'wrap', alignItems: 'flex-start', mt: 2 }}
        onSubmit={(e) => {
          e.preventDefault();
          if (customerValid && projectValid && register?.kind !== 'pending') {
            onRegister(customerId.trim(), projectId.trim());
          }
        }}
      >
        <TextField
          error={customerId !== '' && !customerValid}
          helperText={customerId !== '' && !customerValid ? 'A customer id is a UUID' : ' '}
          label="Customer id"
          size="small"
          slotProps={{
            htmlInput: { 'data-testid': UI_IDENTIFIERS.Billing.REGISTER_CUSTOMER_ID_INPUT },
          }}
          sx={{ minWidth: 320 }}
          value={customerId}
          onChange={(e) => {
            setTypedCustomer(e.target.value);
          }}
        />
        <TextField
          helperText="The project the customer is billed for"
          label="Project"
          size="small"
          slotProps={{
            htmlInput: { 'data-testid': UI_IDENTIFIERS.Billing.REGISTER_PROJECT_ID_INPUT },
          }}
          sx={{ minWidth: 240 }}
          value={projectId}
          onChange={(e) => {
            setProjectId(e.target.value);
          }}
        />
        <Button
          data-testid={UI_IDENTIFIERS.Billing.REGISTER_SUBMIT}
          disabled={!customerValid || !projectValid || register?.kind === 'pending'}
          type="submit"
          variant="contained"
        >
          Register payment instrument
        </Button>
      </Box>
      <StepResult
        doneCopy="Customer registered"
        refusedTestId={UI_IDENTIFIERS.Billing.REGISTER_REFUSED}
        result={register}
        testId={UI_IDENTIFIERS.Billing.REGISTER_RESULT}
      />
    </Paper>
  );
}

// StepResult shows what one onboarding step returned. A refusal carries its own test id,
// so a refused registration is observable apart from an accepted one (use case
// onboard-a-new-customer, project-committed [no]).
function StepResult({
  result,
  testId,
  refusedTestId,
  doneCopy,
}: {
  result: OnboardingStepResult | undefined;
  testId: string;
  refusedTestId: string;
  doneCopy: string;
}): ReactNode {
  const t = useTokens();
  if (result === undefined) return null;
  return (
    <Typography
      data-testid={result.kind === 'failed' ? refusedTestId : testId}
      role={result.kind === 'failed' ? 'alert' : 'status'}
      sx={{ fontSize: 13, color: result.kind === 'failed' ? t.dangerFg : t.ink }}
    >
      {result.kind === 'pending'
        ? 'Working…'
        : result.kind === 'done'
          ? `${doneCopy} ${result.customerId}.`
          : `Refused: ${result.message}`}
    </Typography>
  );
}
