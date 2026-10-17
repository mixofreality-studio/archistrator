/**
 * BillingCycleLookup — the web entry of use case view-a-billing-cycle: the user
 * names a customer and one billing period, and the panel shows that period's
 * recorded settlement (read by BillingCycleLookupContainer through
 * billingManager.GetBillingCycle). The two outcomes the use case returns are the
 * two states shown: the period is still open (nothing has been charged for it
 * yet), or how it settled — charged, nothing owed, or a declined charge escalated
 * to delinquency (invoice unpaid). Pure: the read arrives as `result`.
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import TextField from '@mui/material/TextField';
import Button from '@mui/material/Button';
import Chip from '@mui/material/Chip';
import {
  billingCycleOutcome,
  type BillingCycle,
  type BillingCycleOutcome,
} from '../contracts/billing';
import { useTokens } from '../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../utilities/constants/UIIdentifiers';

/** What the container read for the period asked about. */
export type BillingCycleLookupResult =
  | { kind: 'pending' }
  | { kind: 'notFound' }
  | { kind: 'failed'; message: string }
  | { kind: 'read'; cycle: BillingCycle };

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

const OUTCOME_COPY: Record<BillingCycleOutcome, { label: string; detail: string }> = {
  open: {
    label: 'OPEN',
    detail: 'The period is still open: nothing has been charged for it yet.',
  },
  charged: { label: 'CHARGED', detail: 'The period closed and its net was charged.' },
  nothingOwed: { label: 'NOTHING OWED', detail: 'The period closed with nothing owed.' },
  unpaid: {
    label: 'UNPAID',
    detail:
      'The charge was declined and escalated: the invoice is unpaid and the account is delinquent.',
  },
  unrecognised: {
    label: 'UNRECOGNISED',
    detail: 'The period closed, but this build cannot name how its net routed.',
  },
};

export function BillingCycleLookup({
  result,
  onLookUp,
}: {
  result: BillingCycleLookupResult | undefined;
  onLookUp: (customerId: string, cycleId: string) => void;
}): ReactNode {
  const t = useTokens();
  const [customerId, setCustomerId] = useState('');
  const [cycleId, setCycleId] = useState('');

  const customerValid = UUID.test(customerId.trim());
  const customerInvalid = customerId !== '' && !customerValid;
  const canLookUp = customerValid && cycleId.trim() !== '';

  return (
    <Paper data-testid={UI_IDENTIFIERS.Billing.CYCLE_LOOKUP} sx={{ p: 2.5, mb: 3 }}>
      <Typography
        component="h2"
        sx={{ color: t.ink, fontFamily: t.display, fontWeight: 700, fontSize: 16 }}
      >
        Look up a billing month
      </Typography>
      <Typography sx={{ color: t.muted, fontSize: 13, mb: 2 }}>
        See how one month was settled: still open, charged, nothing owed, or unpaid.
      </Typography>
      <Box
        component="form"
        sx={{ display: 'flex', gap: 1.5, flexWrap: 'wrap', alignItems: 'flex-start' }}
        onSubmit={(e) => {
          e.preventDefault();
          if (canLookUp) {
            onLookUp(customerId.trim(), cycleId.trim());
          }
        }}
      >
        <TextField
          error={customerInvalid}
          helperText={
            customerInvalid
              ? 'A customer ID looks like 8-4-4-4-12 hex characters'
              : 'Shown after you connect to billing'
          }
          label="Billing customer ID"
          size="small"
          slotProps={{ htmlInput: { 'data-testid': UI_IDENTIFIERS.Billing.CUSTOMER_ID_INPUT } }}
          sx={{ minWidth: 320 }}
          value={customerId}
          onChange={(e) => {
            setCustomerId(e.target.value);
          }}
        />
        <TextField
          helperText="Year and month, e.g. 2026-09"
          label="Billing month"
          placeholder="YYYY-MM"
          size="small"
          slotProps={{ htmlInput: { 'data-testid': UI_IDENTIFIERS.Billing.CYCLE_ID_INPUT } }}
          value={cycleId}
          onChange={(e) => {
            setCycleId(e.target.value);
          }}
        />
        <Button
          data-testid={UI_IDENTIFIERS.Billing.CYCLE_LOOKUP_SUBMIT}
          disabled={!canLookUp}
          type="submit"
          variant="contained"
        >
          Look up
        </Button>
      </Box>
      {result === undefined ? null : (
        <Box data-testid={UI_IDENTIFIERS.Billing.CYCLE_RESULT} sx={{ mt: 1 }}>
          <LookupResult result={result} />
        </Box>
      )}
    </Paper>
  );
}

function LookupResult({ result }: { result: BillingCycleLookupResult }): ReactNode {
  const t = useTokens();
  switch (result.kind) {
    case 'pending':
      return <Typography sx={{ color: t.muted, fontSize: 13 }}>Reading the period…</Typography>;
    case 'notFound':
      return (
        <Typography sx={{ color: t.ink, fontSize: 13 }}>
          No billing record for that month and customer.
        </Typography>
      );
    case 'failed':
      return (
        <Typography sx={{ color: t.ink, fontSize: 13 }}>
          The period could not be read: {result.message}
        </Typography>
      );
    case 'read': {
      const copy = OUTCOME_COPY[billingCycleOutcome(result.cycle)];
      return (
        <Box role="status" sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
          <Chip
            data-testid={UI_IDENTIFIERS.Billing.CYCLE_OUTCOME}
            label={copy.label}
            size="small"
            sx={{ fontWeight: 700 }}
          />
          <Typography sx={{ color: t.ink, fontSize: 13 }}>
            {result.cycle.cycleId}: {copy.detail}
          </Typography>
        </Box>
      );
    }
  }
}
