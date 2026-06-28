/**
 * The Artifacts tab — the constructor/reviewer's "WHAT'S BEEN BUILT" view.
 *
 * When constructionRows is present: renders a two-column per-activity browser fed
 * by real server data — left column = activity list (kind badge, status chip,
 * progress bar, artifact count); right column = detail (header + lifecycle strip +
 * ArtifactCard list). A kind filter bar (All / Service / Frontend / Testing with
 * counts from constructionRows) narrows the list.
 *
 * Fallback: when constructionRows is undefined/empty — or when the session is not
 * live and sessionMissing is set — renders the AwaitingPanel stub unchanged (same
 * copy as the original awaiting-stub implementation).
 *
 * Ported from ux-mock ArtifactsTab.tsx, data source swapped from the mock's
 * hardcoded ACTIVITIES to real ConstructionRows + the committed activity-list names.
 */
import { useState, useMemo, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import type { ConstructionSessionState } from '../../api/types';
import { sessionIsLive } from '../../api/constructionAdapters';
import type { ArtifactModelEnvelope, ConstructionRows, ProjectArtifactModelEnvelope, ProjectStateWithGit } from '../../api/types';
import { narrowProject } from '../../api/projectAdapters';
import { useTokens } from '../../theme/ThemeContext';
import type { Tokens } from '../../theme/themes';
import { UI_IDENTIFIERS } from '../../constants/UIIdentifiers';
import { AwaitingPanel } from './AwaitingPanel';
import { ArtifactActivityList, type ArtifactActivityVM } from './ArtifactActivityList';
import { ArtifactActivityDetail } from './ArtifactActivityDetail';
import type { ActivityKind } from './KindBadge';
import { kindColor, KIND_META } from './KindBadge';

type KindFilter = ActivityKind | 'all';

// ---------------------------------------------------------------------------
// FilterChip — the kind filter buttons above the two-column layout.
// ---------------------------------------------------------------------------

function FilterChip({
  active,
  color,
  label,
  t,
  onClick,
}: {
  active: boolean;
  color?: string;
  label: string;
  t: Tokens;
  onClick: () => void;
}): ReactNode {
  return (
    <Box
      role="button"
      sx={{
        px: 1.1,
        py: 0.45,
        cursor: 'pointer',
        borderRadius: t.radius / 8 + 0.5,
        bgcolor: active ? t.accent : t.paperAlt,
        color: active ? t.accentText : (color ?? t.ink),
        border: `1.5px solid ${active ? t.accent : t.line}`,
        fontFamily: t.mono,
        fontWeight: 700,
        fontSize: 11,
      }}
      onClick={onClick}
    >
      {label}
    </Box>
  );
}

// ---------------------------------------------------------------------------
// ArtifactsTab
// ---------------------------------------------------------------------------

export function ArtifactsTab({
  activityEnvelope,
  constructionRows,
  project,
  session,
  sessionMissing,
}: {
  activityEnvelope?: ProjectArtifactModelEnvelope | undefined;
  constructionRows?: ConstructionRows | undefined;
  project?: ProjectStateWithGit | undefined;
  session: ConstructionSessionState | undefined;
  sessionMissing: boolean;
}): ReactNode {
  const t = useTokens();
  const [kindFilter, setKindFilter] = useState<KindFilter>('all');

  // Extract the system envelope from the Phase-1 slots for the Dynamic tab in
  // ServiceContractView (which needs the slot-5 dynamicViews to render real call
  // chains). project.slots is the ArtifactSlotView[] from the Phase-1 head-state;
  // the 'system' kind slot carries the ArtifactModelEnvelope under `.model`.
  const systemEnvelope = useMemo(
    (): ArtifactModelEnvelope | undefined =>
      (project?.slots ?? []).find((s) => s.kind === 'system')?.model ?? undefined,
    [project]
  );

  // ---------------------------------------------------------------------------
  // Step 1: Build the activity view-model by joining constructionRows with the
  // committed activity-list items (name lookup by id). Sort: integrated last so
  // in-flight activities appear first (matches mock ordering intent).
  // ---------------------------------------------------------------------------
  const activityListModel = useMemo(
    () => narrowProject(activityEnvelope, 'activityList'),
    [activityEnvelope]
  );

  const nameForId = useMemo((): ((id: string) => string) => {
    const items = activityListModel?.activities ?? [];
    // Prefer the human-readable title when present; fall back to the id (name).
    const byName = new Map<string, string>(items.map((a) => [a.name, a.title ?? a.name]));
    return (id: string): string => byName.get(id) ?? id;
  }, [activityListModel]);

  const activities = useMemo((): ArtifactActivityVM[] => {
    if (constructionRows === undefined) return [];
    const vms: ArtifactActivityVM[] = Object.values(constructionRows).map((row) => ({
      activityId: row.activityId,
      name: nameForId(row.activityId),
      row,
    }));
    // Sort: integrated last, then by activityId alphabetically.
    const order: Record<string, number> = {
      'in-construction': 0,
      'in-review': 1,
      integrated: 2,
    };
    vms.sort((a, b) => {
      const ao = order[a.row.status] ?? 99;
      const bo = order[b.row.status] ?? 99;
      if (ao !== bo) return ao - bo;
      return a.activityId.localeCompare(b.activityId);
    });
    return vms;
  }, [constructionRows, nameForId]);

  // Filtered list for the left column.
  const filteredActivities = useMemo(
    () => activities.filter((vm) => kindFilter === 'all' || vm.row.kind === kindFilter),
    [activities, kindFilter]
  );

  // Kind counts for the filter chips.
  const kindCounts = useMemo((): Record<ActivityKind, number> => {
    const counts: Record<ActivityKind, number> = { service: 0, frontend: 0, testing: 0 };
    for (const vm of activities) {
      counts[vm.row.kind] += 1;
    }
    return counts;
  }, [activities]);

  // Selection state — default to first in filtered list.
  const [selectedId, setSelectedId] = useState<string>('');
  const resolvedSelectedId = useMemo(() => {
    if (filteredActivities.some((vm) => vm.activityId === selectedId)) return selectedId;
    return filteredActivities[0]?.activityId ?? '';
  }, [filteredActivities, selectedId]);

  const selectedVm = filteredActivities.find((vm) => vm.activityId === resolvedSelectedId);

  // ---------------------------------------------------------------------------
  // Fallback: no constructionRows — render the AwaitingPanel stub unchanged.
  // ---------------------------------------------------------------------------
  const hasData = activities.length > 0;
  const live = sessionIsLive(session);

  if (!hasData) {
    return (
      <Box data-testid={UI_IDENTIFIERS.Construction.ARTIFACTS}>
        <AwaitingPanel
          detail={
            sessionMissing
              ? 'No change has been produced yet — the supervised pump (gated on the R-CPR build cluster) has not staged any construction output. Once a worker produces a change against a frozen contract, its reviewEngine reviewer set and produced artifact surface here.'
              : live
                ? 'The active session has no construction rows yet. Artifacts appear here once the activity pump records its first produced output.'
                : 'No produced artifacts yet — the construction head-state is empty. Once activities are dispatched and produce output, the per-activity artifact browser appears here.'
          }
          title="No produced artifacts yet"
        />
      </Box>
    );
  }

  return (
    <Box data-testid={UI_IDENTIFIERS.Construction.ARTIFACTS} sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
      {/* filter bar */}
      <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 1, alignItems: 'center' }}>
        <Typography
          sx={{ fontFamily: t.mono, fontSize: 10, letterSpacing: '0.08em', color: t.muted, mr: 0.5 }}
        >
          FILTER BY KIND
        </Typography>
        <FilterChip
          active={kindFilter === 'all'}
          label={`All · ${activities.length.toString()}`}
          t={t}
          onClick={() => { setKindFilter('all'); }}
        />
        {(['service', 'frontend', 'testing'] as ActivityKind[]).map((k) => (
          <FilterChip
            active={kindFilter === k}
            color={kindColor(t, k).fg}
            key={k}
            label={`${KIND_META[k].label} · ${kindCounts[k].toString()}`}
            t={t}
            onClick={() => { setKindFilter(k); }}
          />
        ))}
      </Box>

      {/* two-column layout */}
      <Box
        sx={{
          display: 'grid',
          gridTemplateColumns: { xs: '1fr', md: '320px 1fr' },
          gap: 2,
          alignItems: 'start',
        }}
      >
        {/* left: activity list */}
        <ArtifactActivityList
          activities={filteredActivities}
          selectedId={resolvedSelectedId}
          t={t}
          onSelect={(id) => { setSelectedId(id); }}
        />

        {/* right: selected activity detail */}
        {selectedVm !== undefined ? (
          <ArtifactActivityDetail project={project} systemEnvelope={systemEnvelope} t={t} vm={selectedVm} />
        ) : (
          <Typography sx={{ color: t.muted, fontSize: 12.5, p: 2 }}>
            Select an activity to view its artifacts.
          </Typography>
        )}
      </Box>
    </Box>
  );
}
