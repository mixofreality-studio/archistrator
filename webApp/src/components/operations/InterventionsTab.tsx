/**
 * Interventions — the policy-gated human-approval queue (same pattern as
 * Construction): items the interventionEngine ESCALATED (health → Retry/Escalate,
 * billing charge failure → Retry/Charge/Withdraw). The Engine DECIDES; the
 * operator EXECUTES.
 *
 * The operations read projection does NOT yet carry the escalated-intervention
 * queue (the interventionEngine surface is a documented follow-up). So this tab
 * renders an honest awaiting state rather than inventing data — degrading the same
 * way the Construction console does when the pump is dormant. [interventionEngine]
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import BoltIcon from '@mui/icons-material/Bolt';
import { useTokens } from '../../theme/ThemeContext';
import type { OperationsView } from '../../api/operationsTypes';
import { UI_IDENTIFIERS } from '../../constants/UIIdentifiers';
import { AwaitingPanel } from './AwaitingPanel';

export function InterventionsTab({ view }: { view: OperationsView | undefined }): ReactNode {
  const t = useTokens();

  return (
    <Box
      data-testid={UI_IDENTIFIERS.Operations.INTERVENTIONS_TAB}
      sx={{ display: 'flex', flexDirection: 'column', gap: 2, maxWidth: 1080 }}
    >
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
        <BoltIcon sx={{ fontSize: 18, color: t.accent }} />
        <Typography sx={{ fontFamily: t.display, fontWeight: 700, fontSize: 18, color: t.ink }}>Waiting for your steer</Typography>
      </Box>

      <AwaitingPanel
        detail={
          view === undefined
            ? 'This operated app has no observed runtime, so there is nothing for the interventionEngine to escalate yet.'
            : 'The interventionEngine escalation queue (health degraded → Retry/Escalate, billing charge failure → Retry/Charge/Withdraw) is not yet carried by the operations read projection. Health transitions surface on the Status / Health tab; escalations will populate here once the interventionEngine read lands.'
        }
        title={view === undefined ? 'No operated app to supervise' : 'No escalated interventions'}
      />
    </Box>
  );
}
