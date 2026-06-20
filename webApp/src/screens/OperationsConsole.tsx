/**
 * The full-screen Operations console (`/operations/$operatedAppId`) — the UC4
 * operateDeliveredSystem console, the post-construction SIBLING of the Phase-1/2
 * design experiences and the Phase-3 Construction console. It reuses the SAME
 * ExperienceChrome shell, swapping the ordered spine for FOUR TABS — Status /
 * Health · Deployments · Scaling & Cost · Interventions — because operations is a
 * LIVING CONSOLE over an observed runtime, not an ordered authored sequence.
 *
 * It binds to the REAL backend:
 *   - the operated-app runtime view (GET /operations/{id}/view, polled on the
 *     reconcile cadence) drives Status, Deployments rollup, and the autoscaler
 *     decision history + run-rate;
 *   - the read-only cost-projection (GET /operations/{id}/cost-projection) drives
 *     the scale what-if curve;
 *   - the publish actions (Deploy / Scale / UpdateAutoscalerPolicy / Withdraw)
 *     call the real POST endpoints.
 *
 * Operations is CLOUD / k8s only — the local/embedded profile has no operate
 * surface. Every tab degrades to an honest awaiting state rather than an error
 * when the read is quiet (no operated app deployed yet).
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import CircularProgress from '@mui/material/CircularProgress';
import Tooltip from '@mui/material/Tooltip';
import MonitorHeartOutlinedIcon from '@mui/icons-material/MonitorHeartOutlined';
import RocketLaunchOutlinedIcon from '@mui/icons-material/RocketLaunchOutlined';
import TrendingUpOutlinedIcon from '@mui/icons-material/TrendingUpOutlined';
import BoltOutlinedIcon from '@mui/icons-material/BoltOutlined';
import { getRouteApi, useNavigate, Link as RouterLink } from '@tanstack/react-router';

import { ApiError } from '../api/client';
import { useOperationsView } from '../hooks/useOperationsView';
import { sloSummary } from '../api/operationsAdapters';

import { ExperienceChrome } from '../components/design/ExperienceChrome';
import { CommentProvider } from '../components/comments/CommentContext';
import { StatusTab } from '../components/operations/StatusTab';
import { DeploymentsTab } from '../components/operations/DeploymentsTab';
import { ScalingCostTab } from '../components/operations/ScalingCostTab';
import { InterventionsTab } from '../components/operations/InterventionsTab';

import { useTokens } from '../theme/ThemeContext';
import type { Tokens } from '../theme/themes';
import { UI_IDENTIFIERS } from '../constants/UIIdentifiers';

const routeApi = getRouteApi('/operations/$operatedAppId');

type TabId = 'status' | 'deployments' | 'scaling' | 'interventions';

interface TabMeta {
  id: TabId;
  title: string;
  subtitle: string;
  icon: ReactNode;
  testid: string;
}

const TABS: readonly [TabMeta, ...TabMeta[]] = [
  { id: 'status', title: 'Status / Health', subtitle: 'readRuntimeStatus — SloMet / Phase + the health timeline', icon: <MonitorHeartOutlinedIcon sx={{ fontSize: 16 }} />, testid: UI_IDENTIFIERS.Operations.TAB_STATUS },
  { id: 'deployments', title: 'Deployments', subtitle: 'desired-state publish → GitOps-observed Phase', icon: <RocketLaunchOutlinedIcon sx={{ fontSize: 16 }} />, testid: UI_IDENTIFIERS.Operations.TAB_DEPLOYMENTS },
  { id: 'scaling', title: 'Scaling & Cost', subtitle: 'run-rate + what-if curve · autoscaler decision history', icon: <TrendingUpOutlinedIcon sx={{ fontSize: 16 }} />, testid: UI_IDENTIFIERS.Operations.TAB_SCALING },
  { id: 'interventions', title: 'Interventions', subtitle: 'interventionEngine escalations · human steer', icon: <BoltOutlinedIcon sx={{ fontSize: 16 }} />, testid: UI_IDENTIFIERS.Operations.TAB_INTERVENTIONS },
];

export function OperationsConsoleScreen(): ReactNode {
  const { operatedAppId } = routeApi.useParams();
  return (
    <CommentProvider>
      <OperationsConsoleBody operatedAppId={operatedAppId} />
    </CommentProvider>
  );
}

function OperationsConsoleBody({ operatedAppId }: { operatedAppId: string }): ReactNode {
  const t = useTokens();
  const navigate = useNavigate();
  const [tab, setTab] = useState<TabId>('status');

  const viewQuery = useOperationsView(operatedAppId);
  const viewMissing = viewQuery.error instanceof ApiError && viewQuery.error.status === 404;
  const view = viewMissing ? undefined : viewQuery.data;
  const sum = sloSummary(view);

  const active = TABS.find((x) => x.id === tab) ?? TABS[0];

  return (
    <ExperienceChrome
      phaseNum={4}
      phaseTitle="Operations"
      onClose={() => void navigate({ to: '/' })}
    >
      <Box
        data-testid={UI_IDENTIFIERS.Operations.ROOT}
        sx={{ flexGrow: 1, minWidth: 0, display: 'flex', flexDirection: 'column', minHeight: 0 }}
      >
        {/* tab bar */}
        <Box sx={{ flexShrink: 0, display: 'flex', alignItems: 'stretch', gap: 0.5, px: 2.5, bgcolor: t.paperAlt, borderBottom: `1.5px solid ${t.line}`, overflowX: 'auto' }}>
          {TABS.map((x) => {
            const isActive = x.id === tab;
            return (
              <Box
                aria-selected={isActive}
                data-testid={x.testid}
                key={x.id}
                role="tab"
                sx={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 0.75,
                  px: 1.5,
                  py: 1.25,
                  cursor: 'pointer',
                  flexShrink: 0,
                  color: isActive ? t.accent : t.muted,
                  borderBottom: `3px solid ${isActive ? t.accent : 'transparent'}`,
                  '&:hover': { color: isActive ? t.accent : t.ink },
                }}
                onClick={() => { setTab(x.id); }}
              >
                {x.icon}
                <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 12.5, letterSpacing: '0.04em', whiteSpace: 'nowrap' }}>
                  {x.title}
                </Typography>
              </Box>
            );
          })}
          <Box sx={{ flexGrow: 1 }} />
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, pr: 1 }}>
            {view !== undefined && sum.total > 0 && (
              <Box
                role="status"
                sx={{
                  display: 'inline-flex',
                  alignItems: 'center',
                  gap: 0.5,
                  px: 0.9,
                  py: 0.4,
                  borderRadius: 99,
                  bgcolor: sum.breaching > 0 ? t.awaitingBg : t.committedBg,
                  color: sum.breaching > 0 ? t.awaitingFg : t.committedFg,
                  border: `1px solid ${sum.breaching > 0 ? t.awaitingFg : t.committedDot}`,
                  fontFamily: t.mono,
                  fontSize: 11,
                  fontWeight: 700,
                }}
              >
                {sum.healthy}/{sum.total} SLOs
              </Box>
            )}
            <Tooltip title="Hosting + token spend are billed on the top-level Billing surface.">
              <Chip
                clickable
                component={RouterLink}
                data-testid={UI_IDENTIFIERS.Operations.BILLING_LINK}
                label="Billing →"
                size="small"
                sx={{ color: t.ink, borderColor: t.line, fontFamily: t.mono, fontWeight: 700, textDecoration: 'none' }}
                to="/billing"
                variant="outlined"
              />
            </Tooltip>
            <Tooltip title="Operations is cloud / k8s only — the local profile has no operate surface.">
              <Chip label="CLOUD" size="small" sx={{ bgcolor: t.chatPmBg, color: t.chatPmFg }} />
            </Tooltip>
          </Box>
        </Box>

        <Box sx={{ flexGrow: 1, minHeight: 0, overflowY: 'auto', px: { xs: 2, md: 4 }, py: 3 }}>
          <ConsoleHeader subtitle={active.subtitle} t={t} title={active.title} />

          {viewQuery.isLoading && !viewMissing ? (
            <Box sx={{ display: 'flex', justifyContent: 'center', py: 8 }}>
              <CircularProgress />
            </Box>
          ) : tab === 'status' ? (
            <StatusTab view={view} />
          ) : tab === 'deployments' ? (
            <DeploymentsTab operatedAppId={operatedAppId} view={view} />
          ) : tab === 'scaling' ? (
            <ScalingCostTab operatedAppId={operatedAppId} view={view} />
          ) : (
            <InterventionsTab view={view} />
          )}
        </Box>
      </Box>
    </ExperienceChrome>
  );
}

function ConsoleHeader({ t, title, subtitle }: { t: Tokens; title: string; subtitle: string }): ReactNode {
  return (
    <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1.5, mb: 2 }}>
      <Box>
        <Typography sx={{ color: t.ink }} variant="h4">{title}</Typography>
        <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted, mt: 0.5 }}>{subtitle}</Typography>
      </Box>
    </Box>
  );
}
