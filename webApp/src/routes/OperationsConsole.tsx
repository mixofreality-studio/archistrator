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
 *     call the real POST endpoints;
 *   - Register (RegisterOperatedApp, use case register-a-delivered-system-for-
 *     operation) seeds the operated system's head state once, above every tab.
 *
 * Operations is CLOUD / k8s only — the local/embedded profile has no operate
 * surface. (A "CLOUD" chip used to say so in the tab bar; it told a customer
 * nothing they could act on, so it went.) Every tab degrades to an honest awaiting state rather than an error
 * when the read is quiet (no operated app deployed yet).
 */
import { useEffect, useState, type ReactElement, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Button from '@mui/material/Button';
import CircularProgress from '@mui/material/CircularProgress';
import Tooltip from '@mui/material/Tooltip';
import Tabs from '@mui/material/Tabs';
import Tab from '@mui/material/Tab';
import MonitorHeartOutlinedIcon from '@mui/icons-material/MonitorHeartOutlined';
import RocketLaunchOutlinedIcon from '@mui/icons-material/RocketLaunchOutlined';
import TrendingUpOutlinedIcon from '@mui/icons-material/TrendingUpOutlined';
import BoltOutlinedIcon from '@mui/icons-material/BoltOutlined';
import { getRouteApi, useNavigate } from '@tanstack/react-router';

import { ApiError } from '../contracts/errors';
import { useOperationsView } from '../hooks/useOperationsView';
import { useProjects } from '../hooks/useDeliveryQueries';
import { useOperatedAppId } from '../hooks/useOperatedAppId';
import { useRegisterOperatedApp } from '../hooks/useOperationsMutations';
import { sloSummary } from '../contracts/operationsAdapters';

import { ExperienceChrome } from '../components/design/ExperienceChrome';
import { CommentProvider } from '../components/comments/CommentContext';
import { StatusTab } from '../components/operations/StatusTab';
import { DeploymentsTab } from '../components/operations/DeploymentsTab';
import { ScalingCostTab } from '../components/operations/ScalingCostTab';
import { InterventionsTab } from '../components/operations/InterventionsTab';
import { RegisterPanel, type RegisterResult } from '../components/operations/RegisterPanel';

import { useTokens } from '../utilities/theme/ThemeContext';
import type { Tokens } from '../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../utilities/constants/UIIdentifiers';

const routeApi = getRouteApi('/operations/$operatedAppId');

type TabId = 'status' | 'deployments' | 'scaling' | 'interventions';

interface TabMeta {
  id: TabId;
  title: string;
  subtitle: string;
  icon: ReactElement;
  testid: string;
}

const TABS: readonly [TabMeta, ...TabMeta[]] = [
  {
    id: 'status',
    title: 'Health',
    subtitle:
      'Is the app up, and is each part meeting its service-level objective (SLO) — the target it promises, such as response time?',
    icon: <MonitorHeartOutlinedIcon sx={{ fontSize: 16 }} />,
    testid: UI_IDENTIFIERS.Operations.TAB_STATUS,
  },
  {
    id: 'deployments',
    title: 'Deployments',
    subtitle:
      'What is deployed, and publishing a new version. archistrator records the version you want; the hosting platform rolls it out and this page shows what it observes.',
    icon: <RocketLaunchOutlinedIcon sx={{ fontSize: 16 }} />,
    testid: UI_IDENTIFIERS.Operations.TAB_DEPLOYMENTS,
  },
  {
    id: 'scaling',
    title: 'Scaling & Cost',
    subtitle:
      'What the app costs to run now, what it would cost at other sizes, and how it has scaled with traffic.',
    icon: <TrendingUpOutlinedIcon sx={{ fontSize: 16 }} />,
    testid: UI_IDENTIFIERS.Operations.TAB_SCALING,
  },
  {
    id: 'interventions',
    title: 'Interventions',
    subtitle: 'Problems the platform could not resolve on its own, waiting for your decision.',
    icon: <BoltOutlinedIcon sx={{ fontSize: 16 }} />,
    testid: UI_IDENTIFIERS.Operations.TAB_INTERVENTIONS,
  },
];

export function OperationsConsoleScreen(): ReactNode {
  const { operatedAppId } = routeApi.useParams();

  return (
    <CommentProvider>
      <OperationsConsoleBody operatedAppId={operatedAppId} />
    </CommentProvider>
  );
}

/**
 * Reports `projectId` when ITS derived operated-app id is the console's. The
 * route carries only the operated-app id, and the id is a one-way derivation of
 * the project id (useOperatedAppId), so the owning project is found by asking
 * each of the owner's projects — one cached, never-polled read apiece.
 */
function OwnerProbe({
  projectId,
  operatedAppId,
  onMatch,
}: {
  projectId: string;
  operatedAppId: string;
  onMatch: (projectId: string) => void;
}): null {
  const derived = useOperatedAppId(projectId);
  useEffect(() => {
    if (derived === operatedAppId) onMatch(projectId);
  }, [derived, operatedAppId, projectId, onMatch]);
  return null;
}

function OperationsConsoleBody({ operatedAppId }: { operatedAppId: string }): ReactNode {
  const t = useTokens();
  const navigate = useNavigate();
  const [tab, setTab] = useState<TabId>('status');
  const { data: projects } = useProjects();
  const [ownerProjectId, setOwnerProjectId] = useState<string | undefined>(undefined);
  const ownerName = projects?.find((p) => p.projectId === ownerProjectId)?.name;

  const viewQuery = useOperationsView(operatedAppId);
  const viewMissing = viewQuery.error instanceof ApiError && viewQuery.error.status === 404;
  const view = viewMissing ? undefined : viewQuery.data;
  const sum = sloSummary(view);

  const active = TABS.find((x) => x.id === tab) ?? TABS[0];

  const register = useRegisterOperatedApp(operatedAppId);
  const registerResult: RegisterResult | undefined = register.isPending
    ? { kind: 'pending' }
    : register.data !== undefined
      ? { kind: 'registered', version: register.data }
      : register.error !== null
        ? { kind: 'failed', message: register.error.message }
        : undefined;

  return (
    <ExperienceChrome
      // Self: the tab bar is pinned and the tab BODY scrolls (see below). No margin.
      bodyScroll="self"
      eyebrow="AFTER DELIVERY"
      phaseNum={4}
      phaseTitle="Operations"
      projectName={ownerName}
      onClose={() =>
        void (ownerProjectId !== undefined
          ? navigate({ to: '/project/$projectId/home', params: { projectId: ownerProjectId } })
          : navigate({ to: '/' }))
      }
    >
      {(projects ?? []).map((p) => (
        <OwnerProbe
          key={p.projectId}
          operatedAppId={operatedAppId}
          projectId={p.projectId}
          onMatch={setOwnerProjectId}
        />
      ))}
      <Box
        data-testid={UI_IDENTIFIERS.Operations.ROOT}
        sx={{ flexGrow: 1, minWidth: 0, display: 'flex', flexDirection: 'column', minHeight: 0 }}
      >
        {/* tab bar */}
        <Box
          sx={{
            flexShrink: 0,
            display: 'flex',
            alignItems: 'stretch',
            px: 2.5,
            bgcolor: t.paperAlt,
            borderBottom: `1.5px solid ${t.line}`,
          }}
        >
          <Tabs
            aria-label="Operations console sections"
            scrollButtons={false}
            sx={{
              minHeight: 0,
              '& .MuiTabs-flexContainer': { gap: 0.5 },
              '& .MuiTabs-indicator': { backgroundColor: t.accent, height: 3 },
            }}
            value={tab}
            variant="scrollable"
            onChange={(_e, value: TabId) => {
              setTab(value);
            }}
          >
            {TABS.map((x) => (
              <Tab
                data-testid={x.testid}
                icon={x.icon}
                iconPosition="start"
                key={x.id}
                label={x.title}
                sx={{
                  minHeight: 0,
                  flexShrink: 0,
                  gap: 0.75,
                  px: 1.5,
                  py: 1.25,
                  fontFamily: t.mono,
                  fontWeight: 700,
                  fontSize: 12.5,
                  letterSpacing: '0.04em',
                  textTransform: 'none',
                  color: t.muted,
                  '&:hover': { color: t.ink },
                  // The indicator carries the accent; the label stays ink so it
                  // clears AA on every theme (the lavender accent does not on dark).
                  '&.Mui-selected': { color: t.ink },
                }}
                value={x.id}
              />
            ))}
          </Tabs>
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
                {sum.healthy} of {sum.total} SLOs met
              </Box>
            )}
            {ownerProjectId !== undefined ? (
              <Tooltip title="Hosting and AI-work charges are billed on the project's Billing page.">
                <Button
                  data-testid={UI_IDENTIFIERS.Operations.BILLING_LINK}
                  size="small"
                  sx={{ color: t.ink, borderColor: t.line, fontWeight: 700, textTransform: 'none' }}
                  variant="outlined"
                  onClick={() =>
                    void navigate({
                      to: '/project/$projectId/billing',
                      params: { projectId: ownerProjectId },
                    })
                  }
                >
                  Billing →
                </Button>
              </Tooltip>
            ) : null}
          </Box>
        </Box>

        <Box sx={{ flexGrow: 1, minHeight: 0, overflowY: 'auto', px: { xs: 2, md: 4 }, py: 3 }}>
          <RegisterPanel
            result={registerResult}
            onRegister={(vars) => {
              register.mutate(vars);
            }}
          />
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

function ConsoleHeader({
  t,
  title,
  subtitle,
}: {
  t: Tokens;
  title: string;
  subtitle: string;
}): ReactNode {
  return (
    <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1.5, mb: 2 }}>
      <Box>
        <Typography component="h1" sx={{ color: t.ink }} variant="h4">
          {title}
        </Typography>
        <Typography
          sx={{ fontSize: 13.5, color: t.muted, mt: 0.5, maxWidth: 760, lineHeight: 1.5 }}
        >
          {subtitle}
        </Typography>
      </Box>
    </Box>
  );
}
