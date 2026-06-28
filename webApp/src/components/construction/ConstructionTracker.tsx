/**
 * The Tracker tab — App-A project tracking under a build lens. The committed
 * Phase-2 project network (CPM over ActivityList × Network) annotated with the live
 * build status of the ONE in-flight activity from the construction session, plus a
 * CPM summary strip, the status legend, the tracker graph, and the active-activity
 * detail (or the awaiting state when the pump is dormant). Ported from ux-mock
 * ConstructionTracker, bound to real typed head-state + session data. The activity
 * graph is the SHARED project NetworkView under a build lens (statusFor).
 *
 * Phase-3 parity additions:
 *   - EvTrackingChart: EV planned vs earned curves (no AC — no fabricated spend).
 *   - HeadStateRollup: stacked status bar + legend from constructionRows + statusMap.
 *   - NearCriticalFloat: per-activity float table from the NetworkView.
 *   - SPI metric in the headline strip.
 */
import { useMemo, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import { toNetworkView } from '../../api/projectAdapters';
import type {
  GitRow,
  ProjectArtifactModelEnvelope,
  ConstructionRows,
  ConstructionProgress,
  ConstructionRow,
} from '../../api/types';
import type { ConstructionSessionState, OverrideKind } from '../../api/types';
import {
  buildStatusForStage,
  sessionIsLive,
  activeActivityId,
  computeActivityStatuses,
  type BuildStatus,
} from '../../api/constructionAdapters';
import { narrowProject } from '../../api/projectAdapters';
import { useTokens } from '../../theme/ThemeContext';
import type { Tokens } from '../../theme/themes';
import { ComputedBadge } from '../project/computed';
import { StatusLegend } from './status';
import { NetworkView } from '../project/NetworkView';
import { ActivityTrackingDetail } from './ActivityTrackingDetail';
import { AwaitingPanel } from './AwaitingPanel';
import { EvTrackingChart } from './EvTrackingChart';
import { HeadStateRollup, type StatusCount } from './HeadStateRollup';
import { NearCriticalFloat, type FloatChain } from './NearCriticalFloat';

function Metric({
  t,
  value,
  unit,
  label,
  accent,
}: {
  t: Tokens;
  value: string;
  unit: string;
  label: string;
  accent?: boolean;
}): ReactNode {
  return (
    <Box>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.6 }}>
        <Typography
          sx={{ fontFamily: t.mono, fontSize: 9.5, letterSpacing: '0.12em', color: t.muted }}
        >
          {label}
        </Typography>
        <ComputedBadge t={t} />
      </Box>
      <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 0.6 }}>
        <Typography
          sx={{
            fontFamily: t.display,
            fontWeight: 800,
            fontSize: 26,
            lineHeight: 1.1,
            color: accent === true ? t.accent : t.ink,
          }}
        >
          {value}
        </Typography>
        <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}>{unit}</Typography>
      </Box>
    </Box>
  );
}

/** Inline legend swatch for the EV chart. */
function Legend({
  t,
  color,
  label,
  dashed,
}: {
  t: Tokens;
  color: string;
  label: string;
  dashed?: boolean;
}): ReactNode {
  return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
      <Box
        sx={{
          width: 18,
          height: 0,
          borderTop: `${dashed === true ? '2px dashed' : '2.5px solid'} ${color}`,
        }}
      />
      <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, color: t.ink }}>{label}</Typography>
    </Box>
  );
}

export function ConstructionTracker({
  networkEnvelope,
  activityEnvelope,
  session,
  sessionMissing,
  gitFor,
  onOverride,
  overridePending,
  overrideError,
  onSelectActivity,
  constructionRows,
  constructionProgress,
}: {
  networkEnvelope: ProjectArtifactModelEnvelope | undefined;
  activityEnvelope: ProjectArtifactModelEnvelope | undefined;
  session: ConstructionSessionState | undefined;
  sessionMissing: boolean;
  /** Per-activity git head-state lookup (C-CW-GIT); undefined for not-yet-branched. */
  gitFor: (activityId: string) => GitRow | undefined;
  onOverride: (activityId: string, kind: OverrideKind, notes: string) => void;
  overridePending: boolean;
  overrideError: string | undefined;
  onSelectActivity?: ((id: string) => void) | undefined;
  /** Per-activity construction head-state (from project read); absent before any construction. */
  constructionRows?: ConstructionRows | undefined;
  /** Project-level EV progress (from project read); absent before any construction. */
  constructionProgress?: ConstructionProgress | undefined;
}): ReactNode {
  const t = useTokens();
  const view = useMemo(
    () => toNetworkView(networkEnvelope, activityEnvelope),
    [networkEnvelope, activityEnvelope]
  );

  const live = sessionIsLive(session);
  const activeId = activeActivityId(session);
  const activeStatus: BuildStatus =
    session !== undefined ? buildStatusForStage(session.stage) : 'not-started';

  // Narrow the raw NetworkModel from the envelope so computeActivityStatuses can
  // walk the dependency rows without touching the CPM math in toNetworkView.
  const networkModel = useMemo(() => narrowProject(networkEnvelope, 'network'), [networkEnvelope]);

  // constructionRowFor: stable lookup that reads from constructionRows (primary source).
  // Returns undefined when no construction data exists, preserving pre-constructionRows
  // behavior exactly (network-derived eligible/blocked only).
  const constructionRowFor = useMemo(
    () =>
      constructionRows !== undefined
        ? (id: string): ConstructionRow | undefined => constructionRows[id]
        : undefined,
    [constructionRows]
  );

  // Build the full status map from the committed network + constructionRows (primary)
  // + git head-state (secondary/compat) + the one live activity (session override).
  // constructionRows is authoritative for integrated/in-review/in-construction;
  // eligibility cascades off the real integrated set (both constructionRows + git).
  const statusMap = useMemo(
    () =>
      networkModel !== undefined
        ? computeActivityStatuses(
            networkModel,
            gitFor,
            live && activeId !== undefined ? activeId : undefined,
            activeStatus,
            constructionRowFor
          )
        : new Map<string, BuildStatus>(),
    [networkModel, gitFor, live, activeId, activeStatus, constructionRowFor]
  );

  const statusFor = useMemo(
    () =>
      (id: string): { status: BuildStatus; active: boolean } => {
        const isActive = live && activeId !== undefined && id === activeId;
        const status = statusMap.get(id) ?? 'not-started';
        return { status, active: isActive };
      },
    [statusMap, live, activeId]
  );

  // A compact signature of the build-status map + active node so the shared
  // NetworkView rebuilds its nodes when build state changes (it keys its graph on
  // content signature for selection-survival, so status needs its own signal).
  const statusSig = useMemo(
    () =>
      [...statusMap.entries()]
        .map(([id, s]) => `${id}:${s}`)
        .sort()
        .join('|') + `#${activeId ?? ''}`,
    [statusMap, activeId]
  );

  // Counts derived from the status map for the metric strip.
  const eligibleCount = useMemo(
    () => [...statusMap.values()].filter((s) => s === 'eligible').length,
    [statusMap]
  );
  const blockedCount = useMemo(
    () => [...statusMap.values()].filter((s) => s === 'blocked').length,
    [statusMap]
  );
  const doneCount = useMemo(
    () => [...statusMap.values()].filter((s) => s === 'integrated').length,
    [statusMap]
  );

  // Head-state rollup counts: statusMap is now the single source of truth — it already
  // incorporates constructionRows as the primary source and network-derived
  // eligible/blocked as the fallback. Read directly from statusMap so all three
  // consumers (metric strip, graph nodes, rollup bar) share the same status source.
  const rollupCounts = useMemo((): StatusCount[] => {
    const counts = new Map<BuildStatus, number>();
    const increment = (s: BuildStatus): void => {
      counts.set(s, (counts.get(s) ?? 0) + 1);
    };

    for (const status of statusMap.values()) {
      increment(status);
    }

    const order: BuildStatus[] = [
      'integrated',
      'in-review',
      'in-construction',
      'in-detailed-design',
      'eligible',
      'blocked',
      'not-started',
    ];
    return order.map((s) => ({ status: s, count: counts.get(s) ?? 0 }));
  }, [statusMap]);

  // Near-critical chains: activities on the NetworkView with float <= 5d,
  // not on the critical path, sorted by float ascending.
  const nearCriticalChains = useMemo((): FloatChain[] => {
    return view.nodes
      .filter((n) => !n.onCriticalPath && n.float <= 5)
      .sort((a, b) => a.float - b.float)
      .map((n) => ({ chain: n.id, floatDays: n.float }));
  }, [view]);

  if (view.nodes.length === 0) {
    return (
      <AwaitingPanel
        detail="The committed project network (the Phase-2 activity list × network) is not available yet. Commit the Project-Design plan of record (advance to Construction) and the tracker will render the activity graph under the build lens."
        title="No committed network to track yet"
      />
    );
  }

  const ev = constructionProgress?.ev;
  const spi = ev?.spi;

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
      {/* ---- Headline metrics ------------------------------------------------ */}
      <Paper
        data-testid="construction-tracker"
        sx={{
          p: 2,
          display: 'flex',
          flexWrap: 'wrap',
          alignItems: 'center',
          gap: { xs: 2, md: 3.5 },
        }}
      >
        <Metric label="ACTIVITIES" t={t} unit="in network" value={String(view.nodes.length)} />
        <Metric
          accent
          label="CRITICAL PATH"
          t={t}
          unit="activities"
          value={String(view.criticalPathActivityCount)}
        />
        <Metric label="TOTAL DURATION" t={t} unit="days" value={String(view.totalDurationDays)} />
        <Metric
          label="NEAR-CRITICAL"
          t={t}
          unit="≤5d float"
          value={String(view.nearCriticalCount)}
        />
        <Metric label="READY" t={t} unit="eligible" value={String(eligibleCount)} />
        <Metric label="BLOCKED" t={t} unit="waiting" value={String(blockedCount)} />
        <Metric label="DONE" t={t} unit="integrated" value={String(doneCount)} />
        {spi !== undefined && (
          <Metric
            accent={spi < 1}
            label="SPI (EV/PV)"
            t={t}
            unit={spi < 1 ? 'behind' : 'on plan'}
            value={spi.toFixed(2)}
          />
        )}
        <Box sx={{ flexGrow: 1 }} />
        <StatusLegend t={t} />
      </Paper>

      {/* ---- EV tracking chart ---------------------------------------------- */}
      {ev !== undefined && ev.weeks.length > 0 && (
        <Paper sx={{ p: 2.5 }}>
          <Box
            sx={{
              display: 'flex',
              alignItems: 'center',
              gap: 1.5,
              mb: 1,
              flexWrap: 'wrap',
            }}
          >
            <Typography
              sx={{
                fontFamily: t.mono,
                fontWeight: 700,
                fontSize: 12,
                letterSpacing: '0.08em',
                color: t.ink,
              }}
            >
              PROGRESS TRACKING · App A §3–4
            </Typography>
            <Box sx={{ flexGrow: 1 }} />
            <Legend dashed color={t.accent2} label="planned EV (PV)" t={t} />
            <Legend color={t.committedDot} label="progress / EV" t={t} />
          </Box>
          <EvTrackingChart ev={ev} />
        </Paper>
      )}

      {/* ---- Head-state rollup ---------------------------------------------- */}
      {rollupCounts.some((c) => c.count > 0) && (
        <Paper sx={{ p: 2 }}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1, flexWrap: 'wrap' }}>
            <Typography
              sx={{
                fontFamily: t.mono,
                fontWeight: 700,
                fontSize: 11.5,
                letterSpacing: '0.08em',
                color: t.ink,
              }}
            >
              HEAD-STATE ROLL-UP
            </Typography>
            <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}>
              · {view.nodes.length} activities
            </Typography>
          </Box>
          <HeadStateRollup counts={rollupCounts} />
        </Paper>
      )}

      {/* ---- Activity network (SHARED NetworkView under the build lens) ------ */}
      <NetworkView
        activityEnvelope={activityEnvelope}
        networkEnvelope={networkEnvelope}
        showComputedLegend={false}
        showSummary={false}
        statusFor={statusFor}
        statusSig={statusSig}
        {...(onSelectActivity !== undefined ? { onSelect: onSelectActivity } : {})}
      />

      {/* ---- Active activity detail / awaiting ------------------------------ */}
      {live && session !== undefined ? (
        <ActivityTrackingDetail
          git={activeId !== undefined ? gitFor(activeId) : undefined}
          overrideError={overrideError}
          overridePending={overridePending}
          session={session}
          onOverride={onOverride}
        />
      ) : (
        <AwaitingPanel
          detail={
            sessionMissing
              ? 'No construction session has started yet. The supervised pump that dispatches the next eligible activity is gated on a live build cluster (R-CPR) that is not provisioned in this environment, so no activity is in flight. The plan above is the committed network it will build.'
              : 'The construction pump is quiet — no activity is currently dispatched. The pump dispatches the next eligible activity on a 30s tick once the build cluster (R-CPR) is provisioned; until then the tracker shows the committed plan with no live in-flight activity.'
          }
          title="Awaiting the construction pump"
        />
      )}

      {/* ---- Near-critical float table -------------------------------------- */}
      {nearCriticalChains.length > 0 && (
        <Paper sx={{ p: 0, overflow: 'hidden' }}>
          <Box
            sx={{
              px: 2.5,
              py: 1.25,
              bgcolor: t.paperAlt,
              borderBottom: `1.5px solid ${t.line}`,
            }}
          >
            <Typography
              sx={{
                fontFamily: t.mono,
                fontWeight: 700,
                fontSize: 12,
                letterSpacing: '0.06em',
                color: t.ink,
              }}
            >
              NEAR-CRITICAL CHAIN FLOAT · App C §5.6
            </Typography>
          </Box>
          <NearCriticalFloat chains={nearCriticalChains} />
        </Paper>
      )}
    </Box>
  );
}
