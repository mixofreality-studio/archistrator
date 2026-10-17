/**
 * Register the delivered system for operation — the operations console's entry for
 * use case register-a-delivered-system-for-operation (spec §12 B7):
 * operationsManager.RegisterOperatedApp(operatedAppId, customerId, projectRef,
 * deployableBundleRef). The operated-app id is the console's own (its route); the
 * operator names the customer, the project and the deployable bundle.
 *
 * Registration happens once: the result names the head-state version it recorded,
 * and a second registration under a different key is a Conflict the panel reports
 * verbatim rather than retrying.
 *
 * Pure and props-only: the route owns the mutation.
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Paper from '@mui/material/Paper';
import TextField from '@mui/material/TextField';
import Typography from '@mui/material/Typography';
import AppRegistrationIcon from '@mui/icons-material/AppRegistration';

import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** What the last registration said. */
export type RegisterResult =
  | { kind: 'pending' }
  | { kind: 'registered'; version: number }
  | { kind: 'failed'; message: string };

export function RegisterPanel({
  result,
  onRegister,
}: {
  result: RegisterResult | undefined;
  onRegister: (vars: {
    customerId: string;
    projectRef: string;
    deployableBundleRef: string;
  }) => void;
}): ReactNode {
  const t = useTokens();
  const [open, setOpen] = useState(false);
  const [customerId, setCustomerId] = useState('');
  const [projectRef, setProjectRef] = useState('');
  const [bundleRef, setBundleRef] = useState('');

  const customerValid = UUID.test(customerId.trim());
  const customerInvalid = customerId !== '' && !customerValid;
  const pending = result?.kind === 'pending';
  const canRegister =
    customerValid && projectRef.trim() !== '' && bundleRef.trim() !== '' && !pending;

  return (
    <Box sx={{ mb: 2 }}>
      <Box sx={{ display: 'flex', justifyContent: 'flex-end' }}>
        <Button
          data-testid={UI_IDENTIFIERS.Operations.REGISTER_BUTTON}
          size="small"
          startIcon={<AppRegistrationIcon sx={{ fontSize: 16 }} />}
          sx={{ fontFamily: t.mono, color: t.ink, borderColor: t.line, textTransform: 'none' }}
          variant="outlined"
          onClick={() => {
            setOpen((o) => !o);
          }}
        >
          Register for operation
        </Button>
      </Box>
      {open ? (
        <Paper data-testid={UI_IDENTIFIERS.Operations.REGISTER_FORM} sx={{ p: 2.5, mt: 1.5 }}>
          <Typography sx={{ color: t.ink, fontFamily: t.display, fontWeight: 700, fontSize: 16 }}>
            Register the delivered system
          </Typography>
          <Typography sx={{ color: t.muted, fontSize: 13, mb: 2, lineHeight: 1.5 }}>
            A one-time step that hands a delivered system to archistrator to run. Name the billing
            customer it runs for, the project that built it, and the release bundle to run.
          </Typography>
          <Box
            component="form"
            sx={{ display: 'flex', gap: 1.5, flexWrap: 'wrap', alignItems: 'flex-start' }}
            onSubmit={(e) => {
              e.preventDefault();
              if (canRegister) {
                onRegister({
                  customerId: customerId.trim(),
                  projectRef: projectRef.trim(),
                  deployableBundleRef: bundleRef.trim(),
                });
              }
            }}
          >
            <TextField
              error={customerInvalid}
              helperText={
                customerInvalid
                  ? 'A customer ID looks like 8-4-4-4-12 hex characters'
                  : 'From the project’s Billing page'
              }
              label="Billing customer ID"
              size="small"
              slotProps={{
                htmlInput: { 'data-testid': UI_IDENTIFIERS.Operations.REGISTER_CUSTOMER_ID },
              }}
              sx={{ minWidth: 320 }}
              value={customerId}
              onChange={(e) => {
                setCustomerId(e.target.value);
              }}
            />
            <TextField
              helperText=" "
              label="Project"
              size="small"
              slotProps={{
                htmlInput: { 'data-testid': UI_IDENTIFIERS.Operations.REGISTER_PROJECT_REF },
              }}
              value={projectRef}
              onChange={(e) => {
                setProjectRef(e.target.value);
              }}
            />
            <TextField
              helperText="Which build of the system to run"
              label="Release bundle"
              size="small"
              slotProps={{
                htmlInput: { 'data-testid': UI_IDENTIFIERS.Operations.REGISTER_BUNDLE_REF },
              }}
              value={bundleRef}
              onChange={(e) => {
                setBundleRef(e.target.value);
              }}
            />
            <Button
              data-testid={UI_IDENTIFIERS.Operations.REGISTER_SUBMIT}
              disabled={!canRegister}
              type="submit"
              variant="contained"
            >
              Register
            </Button>
          </Box>
          {result === undefined ? null : (
            <Typography
              data-testid={UI_IDENTIFIERS.Operations.REGISTER_RESULT}
              role={result.kind === 'failed' ? 'alert' : 'status'}
              sx={{
                mt: 1,
                fontSize: 13,
                color: result.kind === 'failed' ? t.dangerFg : t.ink,
              }}
            >
              {result.kind === 'pending'
                ? 'Registering…'
                : result.kind === 'registered'
                  ? `Registered (record version ${String(result.version)})`
                  : `Registration refused: ${result.message}`}
            </Typography>
          )}
        </Paper>
      ) : null}
    </Box>
  );
}
