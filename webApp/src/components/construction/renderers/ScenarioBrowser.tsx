import { type ReactNode, useState } from 'react';
import Box from '@mui/material/Box';
import Chip from '@mui/material/Chip';
import Typography from '@mui/material/Typography';
import FormControl from '@mui/material/FormControl';
import Select from '@mui/material/Select';
import MenuItem from '@mui/material/MenuItem';
import { UI_IDENTIFIERS } from '../../../utilities/constants/UIIdentifiers';
import type { TestCaseView, TestScenarioView } from '../../../contracts/types';
import type {
  C4Component,
  DynamicViewModel,
  SequencedRelationship,
} from '../../../contracts/adapters';
import type { Tokens } from '../../../utilities/theme/themes';
import { DynamicViewFlow, type StepDetail, type StepStatus } from '../../flow/DynamicViewFlow';
import { useComments, testScenarioStepAnchor } from '../../comments/CommentContext';

/** 'plan' (N-STP) → every call is a red target; 'run' (N-IT) → coloured by last-run status. */
export type ScenarioMode = 'plan' | 'run';

/** Case-kind accent: happy=green, negative=red, boundary=amber. */
function kindColor(kind: string, t: Tokens): string {
  if (kind === 'happy') return t.committedDot;
  if (kind === 'negative') return t.dangerFg;
  return t.awaitingFg; // boundary
}

/**
 * Maps one test CASE onto the shared layered step-through model: a Test harness
 * (client) drives every call down to the target manager components (managers never
 * call each other). Carries each call's concrete inputs/expected as step detail, and
 * a per-call status colour (plan = red target; run = green/red by last-run status).
 */
function caseToDynamic(
  c: TestCaseView,
  mode: ScenarioMode
): {
  dv: DynamicViewModel;
  statusBySeq: Map<number, StepStatus>;
  detailBySeq: Map<number, StepDetail>;
} {
  const steps = c.steps ?? [];
  const participants: C4Component[] = [
    {
      id: 'test-harness',
      name: 'Test harness',
      kind: 'client',
      layer: 'client',
      encapsulates: '',
      encapsulatesVolatilities: [],
      contractKey: '',
    },
  ];
  const seen = new Set<string>(['test-harness']);
  for (const st of steps) {
    if (!seen.has(st.component)) {
      seen.add(st.component);
      participants.push({
        id: st.component,
        name: st.component,
        kind: 'manager',
        layer: 'manager',
        encapsulates: '',
        encapsulatesVolatilities: [],
        contractKey: '',
      });
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
  const detailBySeq = new Map<number, StepDetail>(
    steps.map((st) => [
      st.seq,
      {
        inputs: (st.inputs ?? []).map((a) => ({ name: a.name, value: a.value })),
        ...(st.expect.result !== undefined ? { result: st.expect.result } : {}),
        errorExpected: st.expect.errorExpected,
        ...(st.expect.errorCode !== undefined ? { errorCode: st.expect.errorCode } : {}),
        ...(st.assertion !== undefined ? { assertion: st.assertion } : {}),
      },
    ])
  );
  return { dv: { title: c.title, participants, edges }, statusBySeq, detailBySeq };
}

/**
 * Scenario + case browser for the System Test Plan: pick a scenario (a core use
 * case), then a case (happy / negative / boundary). The selected case renders as the
 * shared layered step-through, with each call's concrete inputs → expected surfaced
 * in the step caption. Mirrors the architecture dynamic-view selector.
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
  const { setAnchor } = useComments();
  const [selectedId, setSelectedId] = useState<string>('');
  const [selectedCaseId, setSelectedCaseId] = useState<string>('');
  const activeId = scenarios.some((s) => s.id === selectedId)
    ? selectedId
    : (scenarios[0]?.id ?? '');
  const active = scenarios.find((s) => s.id === activeId);
  const cases = active?.cases ?? [];
  const activeCase = cases.find((c) => c.id === selectedCaseId) ?? cases[0];
  const seq = activeCase !== undefined ? caseToDynamic(activeCase, mode) : null;

  // Arm an anchored comment on a single test-scenario step (seq → the case's step),
  // so the operator can attach feedback to a specific call in the plan/run.
  const onCommentStep =
    activeCase !== undefined
      ? (edge: SequencedRelationship): void => {
          setAnchor({
            kind: 'node',
            label: `${edge.label} (step ${String(edge.seq)})`,
            source: `${activeId} · ${activeCase.id}`,
            jsonPath: testScenarioStepAnchor(activeId, activeCase.id, edge.seq),
          });
        }
      : undefined;

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5, minWidth: 0 }}>
      {/* scenario dropdown selector — mirrors the architecture view picker */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
        <Typography
          sx={{ fontFamily: t.mono, fontSize: 10, letterSpacing: '0.08em', color: t.muted }}
        >
          SCENARIO
        </Typography>
        <FormControl size="small" sx={{ minWidth: 360 }}>
          <Select
            data-testid={UI_IDENTIFIERS.Construction.SCENARIO_PICKER}
            sx={{ fontFamily: t.mono, fontSize: 13 }}
            value={activeId}
            onChange={(e) => {
              setSelectedId(e.target.value);
              setSelectedCaseId('');
            }}
          >
            {scenarios.map((s) => (
              <MenuItem key={s.id} sx={{ fontFamily: t.mono, fontSize: 13 }} value={s.id}>
                {`${s.id} · ${s.useCase} — ${s.title}`}
              </MenuItem>
            ))}
          </Select>
        </FormControl>
      </Box>

      {active !== undefined ? (
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
              <Typography
                sx={{ fontFamily: t.mono, fontSize: 9, letterSpacing: '0.08em', color: t.muted }}
              >
                WHAT THIS PROVES
              </Typography>
              <Typography
                sx={{ fontFamily: t.body, fontSize: 12.5, color: t.ink, lineHeight: 1.5 }}
              >
                {active.description}
              </Typography>
            </Box>
          ) : null}

          {/* case selector — pick happy / negative / boundary */}
          {cases.length > 0 ? (
            <Box
              sx={{ display: 'flex', alignItems: 'center', gap: 0.75, flexWrap: 'wrap', mt: 0.25 }}
            >
              <Typography
                sx={{
                  fontFamily: t.mono,
                  fontSize: 10,
                  letterSpacing: '0.08em',
                  color: t.muted,
                  mr: 0.25,
                }}
              >
                CASE
              </Typography>
              {cases.map((c) => {
                const on = c.id === activeCase?.id;
                const col = kindColor(c.kind, t);
                return (
                  <Chip
                    data-testid={UI_IDENTIFIERS.Construction.caseChip(c.id)}
                    key={c.id}
                    label={`${c.kind} · ${c.title}`}
                    size="small"
                    sx={{
                      maxWidth: 340,
                      fontFamily: t.mono,
                      fontSize: 10,
                      fontWeight: 700,
                      cursor: 'pointer',
                      bgcolor: on ? col : t.paperAlt,
                      color: on ? t.paper : t.ink,
                      border: `1.5px solid ${col}`,
                    }}
                    onClick={() => {
                      setSelectedCaseId(c.id);
                    }}
                  />
                );
              })}
            </Box>
          ) : null}

          {/* case-level "what this proves / expected outcome" */}
          {activeCase !== undefined ? (
            <Box
              sx={{ borderLeft: `3px solid ${kindColor(activeCase.kind, t)}`, pl: 1.25, py: 0.25 }}
            >
              {activeCase.proves !== undefined && activeCase.proves.length > 0 ? (
                <Typography
                  sx={{ fontFamily: t.body, fontSize: 12, color: t.ink, lineHeight: 1.5 }}
                >
                  {activeCase.proves}
                </Typography>
              ) : null}
              {activeCase.expectedOutcome !== undefined && activeCase.expectedOutcome.length > 0 ? (
                <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted, mt: 0.35 }}>
                  <Box
                    component="span"
                    sx={{ color: kindColor(activeCase.kind, t), fontWeight: 700 }}
                  >
                    EXPECT{' '}
                  </Box>
                  {activeCase.expectedOutcome}
                </Typography>
              ) : null}
            </Box>
          ) : null}

          {seq !== null && activeCase !== undefined ? (
            <DynamicViewFlow
              detailBySeq={seq.detailBySeq}
              dv={seq.dv}
              height={440}
              resetKey={`${activeId}::${activeCase.id}`}
              statusBySeq={seq.statusBySeq}
              {...(onCommentStep ? { onCommentStep } : {})}
            />
          ) : null}
        </Box>
      ) : null}
    </Box>
  );
}
