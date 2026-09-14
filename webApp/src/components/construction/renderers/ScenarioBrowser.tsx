import { type ReactNode, useContext, useState } from 'react';
import Box from '@mui/material/Box';
import Chip from '@mui/material/Chip';
import Typography from '@mui/material/Typography';
import FormControl from '@mui/material/FormControl';
import Select from '@mui/material/Select';
import MenuItem from '@mui/material/MenuItem';
import { UI_IDENTIFIERS } from '../../../utilities/constants/UIIdentifiers';
import type { TestCaseView, TestScenarioView } from '../../../contracts/types';
import type { C4Component, DynamicViewModel, SequencedCall } from '../../../contracts/adapters';
import type { Tokens } from '../../../utilities/theme/themes';
import { DynamicViewFlow, type StepDetail, type StepStatus } from '../../flow/DynamicViewFlow';
import { useComments, testScenarioStepAnchor } from '../../comments/CommentContext';
import { caseKindInk, stepStatusFor, type CaseInk, type ScenarioMode } from './scenarioInk.ts';
import { useElementWidth } from '../useElementWidth';
import { ScenarioLinkContext } from './scenarioLink';
import { activeScenarioId, casesAsDropdown } from './scenarioSelection.ts';

export type { ScenarioMode } from './scenarioInk.ts';

/** The token behind each case-kind ink (the rule itself is scenarioInk.caseKindInk). */
function inkColor(ink: CaseInk, t: Tokens): string {
  switch (ink) {
    case 'good':
      return t.committedDot;
    case 'danger':
      return t.dangerFg;
    case 'caution':
      return t.awaitingFg;
    case 'neutral':
      return t.muted;
  }
}

function kindColor(kind: string, mode: ScenarioMode, t: Tokens): string {
  return inkColor(caseKindInk(kind, mode), t);
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
  // A test case is not a realized use-case chain: it has no activity diagram and
  // no CallStep, so each call is its own synthetic single-call "step" keyed by the
  // case + seq, captioned with the case title.
  const edges: SequencedCall[] = steps.map((st) => ({
    from: 'test-harness',
    to: st.component,
    mode: 'sync',
    label: `${st.operation}()`,
    seq: st.seq,
    // Synthetic id (case::seq) — intentionally matches no activity node, since
    // this case has no activity diagram. Must never be passed through
    // findingsForStep (there is no CC-* realization to join against).
    stepNodeId: `${c.id}::${String(st.seq)}`,
    stepLabel: c.title,
    callInStep: 1,
    callsInStep: 1,
  }));
  const statusBySeq = new Map<number, StepStatus>(
    steps.map((st) => [st.seq, stepStatusFor(mode, st.status)])
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
  // No people and nothing unresolved: every endpoint is a synthesized participant.
  return {
    dv: { title: c.title, participants, persons: [], edges, unresolved: [] },
    statusBySeq,
    detailBySeq,
  };
}

/**
 * Scenario + case browser for the System Test Plan: pick a scenario (a core use
 * case), then a case (happy / negative / boundary). The selected case renders as the
 * shared layered step-through, with each call's concrete inputs → expected surfaced
 * in the step caption. Mirrors the architecture dynamic-view selector.
 *
 * `linked` makes the scenario follow the URL's `sc` deep link (scenarioLink.ts):
 * the plan's own browser on N-STP takes it, so a component's "reached through"
 * row opens at the scenario it names. A coverage browser inside another
 * component's pane keeps its own local pick.
 *
 * In the narrow pane the scenario picker takes the width it is given rather than
 * a fixed 360px, and more than three cases go into a dropdown (they wrapped to
 * four lines as chips) — designer check, polish 5.
 */
export function ScenarioBrowser({
  scenarios,
  mode,
  t,
  statusChip,
  linked = false,
}: {
  scenarios: TestScenarioView[];
  mode: ScenarioMode;
  t: Tokens;
  statusChip?: (s: TestScenarioView) => ReactNode;
  linked?: boolean;
}): ReactNode {
  const { setAnchor } = useComments();
  const link = useContext(ScenarioLinkContext);
  const deepLink = linked ? link : undefined;
  const [measure, width] = useElementWidth();
  const [selectedId, setSelectedId] = useState<string>('');
  const [selectedCaseId, setSelectedCaseId] = useState<string>('');
  const activeId = activeScenarioId(scenarios, [deepLink?.scenarioId, selectedId]);
  const active = scenarios.find((s) => s.id === activeId);
  const cases = active?.cases ?? [];
  const activeCase = cases.find((c) => c.id === selectedCaseId) ?? cases[0];
  const seq = activeCase !== undefined ? caseToDynamic(activeCase, mode) : null;

  // Arm an anchored comment on a single test-scenario step (seq → the case's step),
  // so the operator can attach feedback to a specific call in the plan/run.
  const onCommentStep =
    activeCase !== undefined
      ? (edge: SequencedCall): void => {
          setAnchor({
            kind: 'node',
            label: `${edge.label} (step ${String(edge.seq)})`,
            source: `${activeId} · ${activeCase.id}`,
            jsonPath: testScenarioStepAnchor(activeId, activeCase.id, edge.seq),
          });
        }
      : undefined;

  return (
    <Box
      data-active-scenario={activeId}
      ref={measure}
      sx={{ display: 'flex', flexDirection: 'column', gap: 1.5, minWidth: 0 }}
    >
      {/* scenario dropdown selector — mirrors the architecture view picker */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, minWidth: 0 }}>
        <Typography
          sx={{ fontFamily: t.mono, fontSize: 10, letterSpacing: '0.08em', color: t.muted }}
        >
          SCENARIO
        </Typography>
        <FormControl size="small" sx={{ flex: '1 1 auto', minWidth: 0, maxWidth: 520 }}>
          <Select
            data-testid={UI_IDENTIFIERS.Construction.SCENARIO_PICKER}
            inputProps={{ 'aria-label': 'Scenario' }}
            sx={{ fontFamily: t.mono, fontSize: 13 }}
            value={activeId}
            onChange={(e) => {
              setSelectedId(e.target.value);
              setSelectedCaseId('');
              deepLink?.onScenarioChange(e.target.value);
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

          {/* case selector — pick happy / negative / boundary. A dropdown past three
              cases in the narrow pane; chips otherwise. */}
          {cases.length > 0 && casesAsDropdown(cases.length, width) ? (
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, minWidth: 0, mt: 0.25 }}>
              <Typography
                sx={{
                  flexShrink: 0,
                  whiteSpace: 'nowrap',
                  fontFamily: t.mono,
                  fontSize: 10,
                  letterSpacing: '0.08em',
                  color: t.muted,
                }}
              >
                CASE · {cases.length}
              </Typography>
              <FormControl size="small" sx={{ flex: '1 1 auto', minWidth: 0 }}>
                <Select
                  data-testid={UI_IDENTIFIERS.Construction.CASE_PICKER}
                  inputProps={{ 'aria-label': 'Case' }}
                  // Plain text in the box, so the selected case's chip is not drawn twice.
                  renderValue={(id: string) => {
                    const c = cases.find((x) => x.id === id);
                    return c !== undefined ? `${c.kind} · ${c.title}` : '';
                  }}
                  sx={{ fontFamily: t.mono, fontSize: 12 }}
                  value={activeCase?.id ?? ''}
                  onChange={(e) => {
                    setSelectedCaseId(e.target.value);
                  }}
                >
                  {cases.map((c) => {
                    const col = kindColor(c.kind, mode, t);
                    return (
                      // Each option IS the case chip — the same ink, border and test
                      // id as the chip row — so the ink reads the same either way.
                      <MenuItem key={c.id} sx={{ py: 0.5 }} value={c.id}>
                        <Chip
                          data-case-ink={caseKindInk(c.kind, mode)}
                          data-testid={UI_IDENTIFIERS.Construction.caseChip(c.id)}
                          label={`${c.kind} · ${c.title}`}
                          size="small"
                          sx={{
                            maxWidth: 420,
                            fontFamily: t.mono,
                            fontSize: 10,
                            fontWeight: 700,
                            cursor: 'pointer',
                            bgcolor: t.paperAlt,
                            color: t.ink,
                            border: `1.5px solid ${col}`,
                          }}
                        />
                      </MenuItem>
                    );
                  })}
                </Select>
              </FormControl>
            </Box>
          ) : cases.length > 0 ? (
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
                const col = kindColor(c.kind, mode, t);
                return (
                  <Chip
                    data-case-ink={caseKindInk(c.kind, mode)}
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
              data-case-ink={caseKindInk(activeCase.kind, mode)}
              data-testid={UI_IDENTIFIERS.Construction.ACTIVE_CASE}
              sx={{
                borderLeft: `3px solid ${kindColor(activeCase.kind, mode, t)}`,
                pl: 1.25,
                py: 0.25,
              }}
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
                    data-testid={UI_IDENTIFIERS.Construction.CASE_EXPECT}
                    sx={{ color: kindColor(activeCase.kind, mode, t), fontWeight: 700 }}
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
