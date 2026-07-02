import { type ReactNode, useMemo, useState } from 'react';
import Box from '@mui/material/Box';
import Chip from '@mui/material/Chip';
import Typography from '@mui/material/Typography';
import FormControl from '@mui/material/FormControl';
import Select from '@mui/material/Select';
import MenuItem from '@mui/material/MenuItem';
import { UI_IDENTIFIERS } from '../../../constants/UIIdentifiers';
import type { TestScenarioView } from '../../../api/types';
import type { C4Component, DynamicViewModel, SequencedRelationship } from '../../../api/adapters';
import type { Tokens } from '../../../theme/themes';
import { DynamicViewFlow, type StepStatus } from '../../flow/DynamicViewFlow';

/** 'plan' (N-STP) → every call is a red target; 'run' (N-IT) → coloured by last-run status. */
export type ScenarioMode = 'plan' | 'run';

/**
 * Maps a black-box test scenario onto the shared layered step-through model: a Test
 * harness (client) drives every call down to the target manager components (the
 * managers never call each other). At plan time every call is a red target; at run
 * time each is tinted green (passing) or red (still failing) by its last-run status.
 */
function scenarioToDynamic(s: TestScenarioView, mode: ScenarioMode): {
  dv: DynamicViewModel;
  statusBySeq: Map<number, StepStatus>;
} {
  const steps = s.steps ?? [];
  const participants: C4Component[] = [
    { id: 'test-harness', name: 'Test harness', kind: 'client', layer: 'client', encapsulates: '' },
  ];
  const seen = new Set<string>(['test-harness']);
  for (const st of steps) {
    if (!seen.has(st.component)) {
      seen.add(st.component);
      participants.push({ id: st.component, name: st.component, kind: 'manager', layer: 'manager', encapsulates: '' });
    }
  }
  const edges: SequencedRelationship[] = steps.map((st) => ({
    from: 'test-harness',
    to: st.component,
    mode: 'sync',
    label: `${st.operation}()`,
    seq: st.seq,
  }));
  const statusBySeq = new Map<number, StepStatus>(
    steps.map((st) => [st.seq, mode === 'run' && st.status === 'green' ? 'green' : 'red'])
  );
  return { dv: { title: s.title, participants, edges }, statusBySeq };
}

/**
 * Scenario selector + single-scenario view — mirrors the service-contract
 * dynamic-view selector (chips pick one; only that one renders, no long scroll).
 * Shows the selected scenario's "what/why" summary and its call sequence as the
 * shared layered step-through (Test harness → target managers, one call at a time).
 */
export function ScenarioBrowser({
  scenarios,
  mode,
  t,
  statusChip,
}: {
  scenarios: TestScenarioView[];
  mode: ScenarioMode;
  t: Tokens;
  statusChip?: (s: TestScenarioView) => ReactNode;
}): ReactNode {
  const [selectedId, setSelectedId] = useState<string>('');
  const activeId = scenarios.some((s) => s.id === selectedId) ? selectedId : (scenarios[0]?.id ?? '');
  const active = scenarios.find((s) => s.id === activeId);
  const seq = useMemo(() => (active !== undefined ? scenarioToDynamic(active, mode) : null), [active, mode]);

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5, minWidth: 0 }}>
      {/* scenario dropdown selector — mirrors the architecture view picker */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
        <Typography sx={{ fontFamily: t.mono, fontSize: 10, letterSpacing: '0.08em', color: t.muted }}>
          SCENARIO
        </Typography>
        <FormControl size="small" sx={{ minWidth: 360 }}>
          <Select
            data-testid={UI_IDENTIFIERS.Construction.SCENARIO_PICKER}
            sx={{ fontFamily: t.mono, fontSize: 13 }}
            value={activeId}
            onChange={(e) => { setSelectedId(e.target.value); }}
          >
            {scenarios.map((s) => (
              <MenuItem key={s.id} sx={{ fontFamily: t.mono, fontSize: 13 }} value={s.id}>
                {`${s.useCase} — ${s.title}`}
              </MenuItem>
            ))}
          </Select>
        </FormControl>
      </Box>

      {active !== undefined && seq !== null ? (
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.75 }}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
            <Chip
              label={active.useCase}
              size="small"
              sx={{ height: 18, fontSize: 9, bgcolor: t.chatArchitectBg, color: t.chatArchitectFg }}
            />
            <Typography sx={{ fontFamily: t.body, fontWeight: 700, fontSize: 14, color: t.ink }}>
              {active.title}
            </Typography>
            <Box sx={{ flexGrow: 1 }} />
            {statusChip?.(active)}
          </Box>
          {active.description !== undefined && active.description.length > 0 ? (
            <Box sx={{ borderLeft: `3px solid ${t.line}`, pl: 1.25, py: 0.25 }}>
              <Typography sx={{ fontFamily: t.mono, fontSize: 9, letterSpacing: '0.08em', color: t.muted }}>
                WHAT THIS PROVES
              </Typography>
              <Typography sx={{ fontFamily: t.body, fontSize: 12.5, color: t.ink, lineHeight: 1.5 }}>
                {active.description}
              </Typography>
            </Box>
          ) : null}
          <DynamicViewFlow dv={seq.dv} height={440} resetKey={active.id} statusBySeq={seq.statusBySeq} />
        </Box>
      ) : null}
    </Box>
  );
}
