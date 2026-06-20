/**
 * Deployments — the desired-state publish surface. aiarch PUBLISHES desired state
 * (a commit to the manifests repo); the GitOps runtime DRIVES convergence — there
 * is no aiarch progress bar, only the observed rollup Phase + an in-flight
 * (converging) flag from the live view. The Deploy / Scale / Update-autoscaler /
 * Withdraw buttons call the real POST routes (operationsManager). Per-component
 * revision detail is not carried by the read projection yet, so the tab surfaces
 * the rollup + the publish affordances honestly. [operationsManager + operatedRuntimeAccess]
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import Button from '@mui/material/Button';
import RocketLaunchOutlinedIcon from '@mui/icons-material/RocketLaunchOutlined';
import TrendingUpOutlinedIcon from '@mui/icons-material/TrendingUpOutlined';
import PowerSettingsNewIcon from '@mui/icons-material/PowerSettingsNew';
import { useTokens } from '../../theme/ThemeContext';
import type { OperationsView } from '../../api/operations';
import { normalizePhase } from '../../api/operationsAdapters';
import { useOperationAction, useWithdrawOperatedApp } from '../../hooks/useOperationsMutations';
import { UI_IDENTIFIERS } from '../../constants/UIIdentifiers';
import { PhaseChip } from './phase';
import { AwaitingPanel } from './AwaitingPanel';

export function DeploymentsTab({
  operatedAppId,
  view,
}: {
  operatedAppId: string;
  view: OperationsView | undefined;
}): ReactNode {
  const t = useTokens();
  const action = useOperationAction(operatedAppId);
  const withdraw = useWithdrawOperatedApp(operatedAppId);
  const [lastResult, setLastResult] = useState<string | null>(null);

  const busy = action.isPending || withdraw.isPending;
  const error =
    action.error instanceof Error
      ? action.error.message
      : withdraw.error instanceof Error
        ? withdraw.error.message
        : undefined;

  const run = (kind: 'deploy' | 'scale' | 'autoscaler-policy'): void => {
    action.mutate(kind, {
      onSuccess: (r) =>
        { setLastResult(
          r.published
            ? `Published${r.revision !== undefined && r.revision.length > 0 ? ` · ${r.revision}` : ''}`
            : 'Accepted (no republish needed)'
        ); },
    });
  };
  const doWithdraw = (): void => {
    withdraw.mutate({ reason: 'operator withdraw from console' }, {
      onSuccess: (r) => { setLastResult(r.withdrawn ? 'Withdrawn' : 'Already withdrawn'); },
    });
  };

  if (view === undefined) {
    return (
      <AwaitingPanel
        detail="This operated app has no observed deployment status. Once a desired state is published, aiarch observes convergence via the GitOps runtime — it does not drive it."
        title="No deployment status yet"
      />
    );
  }

  const rollup = normalizePhase(view.phase);

  return (
    <Box
      data-testid={UI_IDENTIFIERS.Operations.DEPLOYMENTS_TAB}
      sx={{ display: 'flex', flexDirection: 'column', gap: 2, maxWidth: 1080 }}
    >
      <Box sx={{ p: 1.25, bgcolor: t.chatPmBg, border: `1.5px solid ${t.line}`, borderRadius: t.radius / 8 + 0.5 }}>
        <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.chatPmFg, lineHeight: 1.5 }}>
          🔭 aiarch <b>publishes desired state</b> (a git commit to the manifests repo); the GitOps runtime <b>drives convergence</b>. There is no aiarch
          progress bar — only the observed rollup Phase. Phase=Unknown is the normal just-published transient (converging).
        </Typography>
      </Box>

      {/* rollup + in-flight */}
      <Paper sx={{ p: 0, overflow: 'hidden' }}>
        <Box sx={{ px: 2, py: 1.25, bgcolor: t.paperAlt, borderBottom: `1.5px solid ${t.line}`, display: 'flex', alignItems: 'center', gap: 1.5, flexWrap: 'wrap' }}>
          <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11, letterSpacing: '0.06em', color: t.ink }}>DESIRED-STATE ROLLUP</Typography>
          <PhaseChip phase={rollup} t={t} />
          {view.inFlight ? <Chip label="republish in flight" size="small" sx={{ height: 18, fontSize: 8.5, bgcolor: t.chatPmBg, color: t.chatPmFg }} /> : null}
          <Box sx={{ flexGrow: 1 }} />
          {lastResult !== null && (
            <Chip label={lastResult} size="small" sx={{ height: 18, fontSize: 9, fontFamily: t.mono, color: t.committedFg }} variant="outlined" />
          )}
        </Box>
        <Box sx={{ px: 2, py: 1.75 }}>
          <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.muted, lineHeight: 1.5, mb: 1.5 }}>
            {view.health.detail.length > 0
              ? view.health.detail
              : 'Per-component revision detail is not carried by the read projection yet. Publish a desired state below; convergence is observed on the reconcile tick.'}
          </Typography>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
            <Button
              color="primary"
              data-testid={UI_IDENTIFIERS.Operations.DEPLOY_BUTTON}
              disabled={busy}
              size="small"
              startIcon={<RocketLaunchOutlinedIcon sx={{ fontSize: 15 }} />}
              sx={{ py: 0.25, fontSize: 11 }}
              variant="contained"
              onClick={() => { run('deploy'); }}
            >
              Deploy / publish revision
            </Button>
            <Button
              color="inherit"
              data-testid={UI_IDENTIFIERS.Operations.SCALE_BUTTON}
              disabled={busy}
              size="small"
              startIcon={<TrendingUpOutlinedIcon sx={{ fontSize: 15 }} />}
              sx={{ py: 0.25, fontSize: 11, color: t.ink, borderColor: t.line }}
              variant="outlined"
              onClick={() => { run('scale'); }}
            >
              Scale
            </Button>
            <Box sx={{ flexGrow: 1 }} />
            <Button
              color="inherit"
              data-testid={UI_IDENTIFIERS.Operations.WITHDRAW_BUTTON}
              disabled={busy}
              size="small"
              startIcon={<PowerSettingsNewIcon sx={{ fontSize: 15 }} />}
              sx={{ py: 0.25, fontSize: 11, color: t.awaitingFg, borderColor: t.line }}
              variant="outlined"
              onClick={doWithdraw}
            >
              Withdraw
            </Button>
          </Box>
          {error !== undefined && (
            <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.awaitingFg, mt: 1 }}>{error}</Typography>
          )}
        </Box>
      </Paper>
    </Box>
  );
}
