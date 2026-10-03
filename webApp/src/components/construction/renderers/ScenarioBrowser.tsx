import { type ReactNode, useContext, useState } from 'react';
import Box from '@mui/material/Box';
import Chip from '@mui/material/Chip';
import Typography from '@mui/material/Typography';
import FormControl from '@mui/material/FormControl';
import Select from '@mui/material/Select';
import MenuItem from '@mui/material/MenuItem';
import { UI_IDENTIFIERS } from '../../../utilities/constants/UIIdentifiers';
import type { ScenarioView, StepBindingView, TestExpectView } from '../../../contracts/types';
import type { C4Component, DynamicViewModel, SequencedCall } from '../../../contracts/adapters';
import type { Tokens } from '../../../utilities/theme/themes';
import { DynamicViewFlow, type StepDetail, type StepStatus } from '../../flow/DynamicViewFlow';
import {
  useComments,
  testPlanBindingAnchor,
  testPlanStepAnchor,
} from '../../comments/CommentContext';
import { CommentableList } from '../../comments/CommentableList';
import { ScenarioLinkContext } from './scenarioLink';
import { activeScenarioId } from './scenarioSelection.ts';

/** The harness's id in the step-through: it stands in for each stimulus's `from`. */
const HARNESS_ID = 'test-harness';

/**
 * One projected stimulus, as step-through detail: the BINDING's concrete inputs /
 * expectation when the plan has bound this seq, and the projection's own expected
 * outputs / states / hints either way — so an unbound step still says what it
 * must prove.
 */
function stimulusDetail(
  s: ScenarioView,
  seq: number,
  step: StepBindingView | undefined
): StepDetail {
  const st = s.stimuli.find((x) => x.seq === seq);
  const proves: string[] = [];
  for (const o of st?.outcome.expectedOutputs ?? []) {
    proves.push(`output ${o.label.length > 0 ? o.label : o.node} → ${o.to}`);
  }
  for (const x of st?.outcome.expectedStates ?? []) proves.push(`${x.object} ${x.inState}`);
  for (const h of st?.outcome.hints ?? []) proves.push(h);
  const expect: TestExpectView | undefined = step?.expect;
  return {
    inputs: (step?.inputs ?? []).map((a) => ({ name: a.name, value: a.value })),
    ...(expect !== undefined && expect.result.length > 0 ? { result: expect.result } : {}),
    errorExpected: expect?.errorExpected ?? false,
    ...(expect !== undefined && expect.errorCode.length > 0 ? { errorCode: expect.errorCode } : {}),
    ...(proves.length > 0 ? { assertion: proves.join(' · ') } : {}),
  };
}

/**
 * Maps one projected scenario onto the shared layered step-through model: the
 * generated test harness (a client, standing in for the stimulus's `from`) calls
 * the component under test once per stimulus. A bound stimulus is a neutral,
 * planned call; an unbound one is the red target the test engineer still owes; a
 * skipped scenario's calls are all neutral.
 */
function scenarioToDynamic(
  s: ScenarioView,
  component: string
): {
  dv: DynamicViewModel;
  statusBySeq: Map<number, StepStatus>;
  detailBySeq: Map<number, StepDetail>;
} {
  const participants: C4Component[] = [
    {
      id: HARNESS_ID,
      name: 'Test harness',
      kind: 'client',
      layer: 'client',
      encapsulates: '',
      encapsulatesVolatilities: [],
      contractKey: '',
    },
    {
      id: component,
      name: component,
      kind: 'manager',
      layer: 'manager',
      encapsulates: '',
      encapsulatesVolatilities: [],
      contractKey: '',
    },
  ];
  const stepsBySeq = new Map<number, StepBindingView>(
    (s.binding?.steps ?? []).map((st) => [st.seq, st])
  );
  const edges: SequencedCall[] = s.stimuli.map((st) => ({
    from: HARNESS_ID,
    to: component,
    mode: 'sync',
    label: `${st.input.op}()`,
    seq: st.seq,
    // The scenario IS a path through the use case's activity diagram, so each
    // call keys on the node its stimulus stands at.
    stepNodeId: st.node,
    stepLabel: st.input.from.length > 0 ? `${st.input.from}: ${st.input.call}` : st.input.call,
    callInStep: 1,
    callsInStep: 1,
  }));
  const statusBySeq = new Map<number, StepStatus>(
    s.stimuli.map((st) => [
      st.seq,
      s.binding?.skip !== undefined || stepsBySeq.has(st.seq) ? 'planned' : 'red',
    ])
  );
  const detailBySeq = new Map<number, StepDetail>(
    s.stimuli.map((st) => [st.seq, stimulusDetail(s, st.seq, stepsBySeq.get(st.seq))])
  );
  return {
    dv: { title: s.title, participants, persons: [], edges, unresolved: [] },
    statusBySeq,
    detailBySeq,
  };
}

/** The binding's standing, as a chip: bound / n/m bound / unbound / skipped. */
function bindingChip(s: ScenarioView, t: Tokens): ReactNode {
  const b = s.binding;
  let label: string;
  let fg: string;
  let bg: string;
  if (b === undefined) {
    label = 'unbound';
    fg = t.dangerFg;
    bg = t.paperAlt;
  } else if (b.skip !== undefined) {
    label = `skipped until ${b.skip.until}`;
    fg = t.awaitingFg;
    bg = t.awaitingBg;
  } else {
    const bound = new Set(b.steps.map((st) => st.seq));
    const all = s.stimuli.every((st) => bound.has(st.seq));
    label = all ? 'bound' : `${String(bound.size)}/${String(s.stimuli.length)} bound`;
    fg = all ? t.committedFg : t.awaitingFg;
    bg = all ? t.committedBg : t.awaitingBg;
  }
  return (
    <Chip
      data-binding-status={label}
      data-testid={UI_IDENTIFIERS.Construction.SCENARIO_BINDING_CHIP}
      label={label}
      size="small"
      sx={{ height: 18, fontSize: 8.5, bgcolor: bg, color: fg, border: `1px solid ${t.line}` }}
    />
  );
}

function probeLine(p: StepBindingView['probes'][number]): string {
  const args = p.inputs.map((a) => `${a.name}=${a.value}`).join(', ');
  const outcome = p.expect.errorExpected
    ? ` → error ${p.expect.errorCode}`
    : p.expect.result.length > 0
      ? ` → ${p.expect.result}`
      : '';
  return `probe ${p.for}: ${p.component}.${p.operation}(${args})${outcome}`;
}

/** One bound step's probes, unobservables and hook flag — what the step caption
 *  (inputs / expect) does not carry; or the plain reason there is no binding. */
function BindingDetails({ s, t }: { s: ScenarioView; t: Tokens }): ReactNode {
  const b = s.binding;
  if (b === undefined) {
    return (
      <Typography
        data-testid={UI_IDENTIFIERS.Construction.SCENARIO_UNBOUND}
        sx={{ fontFamily: t.body, fontSize: 12, color: t.muted }}
      >
        No binding yet: the test plan has not bound this scenario. Every projected stimulus above is
        a red target until it carries concrete inputs and an expectation.
      </Typography>
    );
  }
  if (b.skip !== undefined) {
    return (
      <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.ink }}>
        <Box component="span" sx={{ fontFamily: t.mono, fontWeight: 700, color: t.awaitingFg }}>
          SKIPPED{' '}
        </Box>
        {b.skip.reason} — until {b.skip.until}. A skipped scenario is not a pass; the testing gate
        counts it.
      </Typography>
    );
  }
  const withExtras = b.steps.filter(
    (st) => st.probes.length > 0 || st.unobservable.length > 0 || st.hook
  );
  if (withExtras.length === 0) return null;
  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.5 }}>
      {withExtras.map((st) => (
        <Box key={st.seq} sx={{ borderLeft: `3px solid ${t.line}`, pl: 1.25, py: 0.25 }}>
          <Typography
            sx={{ fontFamily: t.mono, fontSize: 9, letterSpacing: '0.08em', color: t.muted }}
          >
            {`STEP ${String(st.seq)} · ${st.operation}${st.hook ? ' · HOOK' : ''}`}
          </Typography>
          {st.probes.map((p, i) => (
            <Typography
              key={`${p.for}-${String(i)}`}
              sx={{ fontFamily: t.mono, fontSize: 11, color: t.ink }}
            >
              {probeLine(p)}
            </Typography>
          ))}
          {st.unobservable.length > 0 ? (
            <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}>
              {`unobservable: ${st.unobservable.join(', ')}`}
            </Typography>
          ) : null}
        </Box>
      ))}
    </Box>
  );
}

/**
 * Scenario browser for a component's deterministic test plan (spec §2.4, §3):
 * pick a projected scenario; it renders as the shared layered step-through, each
 * stimulus a call from the generated harness into the component, with the
 * binding's concrete inputs → expectation surfaced in the step caption and the
 * projection's expected outputs / states / hints as what the step proves.
 * Mirrors the architecture dynamic-view selector.
 *
 * `linked` makes the scenario follow the URL's `sc` deep link (scenarioLink.ts):
 * the plan's own browser takes it, so a component's "reached through" row opens
 * at the scenario it names. A browser inside another pane keeps its own pick.
 */
export function ScenarioBrowser({
  scenarios,
  component,
  t,
  statusChip,
  linked = false,
}: {
  scenarios: ScenarioView[];
  /** The component the scenarios are projected onto (the anchor's `testPlan[<comp>]`). */
  component: string;
  t: Tokens;
  statusChip?: (s: ScenarioView) => ReactNode;
  linked?: boolean;
}): ReactNode {
  const { setAnchor } = useComments();
  const link = useContext(ScenarioLinkContext);
  const deepLink = linked ? link : undefined;
  const [selectedId, setSelectedId] = useState<string>('');
  const activeId = activeScenarioId(scenarios, [deepLink?.scenarioId, selectedId]);
  const active = scenarios.find((s) => s.id === activeId);
  const seq = active !== undefined ? scenarioToDynamic(active, component) : null;

  // Arm an anchored comment on one bound step (seq → the binding's step), so the
  // reviewer can attach feedback to a specific call in the plan.
  const onCommentStep =
    active !== undefined
      ? (edge: SequencedCall): void => {
          setAnchor({
            kind: 'node',
            label: `${edge.label} (step ${String(edge.seq)})`,
            source: `${component} · ${active.id}`,
            jsonPath: testPlanStepAnchor(component, active.id, edge.seq),
          });
        }
      : undefined;

  return (
    <Box
      data-active-scenario={activeId}
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

      {active !== undefined && seq !== null ? (
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.75 }}>
          {/* The scenario's header is the scenario-level anchor (spec §6.4):
              `…testPlan[<comp>].bindings[scenario=<id>]`, the binding as a whole — a
              skip reason, a missing binding — where a step anchor would be the wrong
              grain. The step-through below arms the per-step child. */}
          <CommentableList
            ariaLabel="Scenario binding"
            getAnchor={(s) => ({
              kind: 'node',
              label: `scenario:${s.id}`,
              source: `${component} · ${s.id}`,
              jsonPath: testPlanBindingAnchor(component, s.id),
            })}
            getKey={(s) => s.id}
            getLabel={(s) => `${s.id} ${s.title}`}
            getLabelKind={() => 'scenario binding'}
            items={[active]}
            renderItem={(s) => (
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
                <Chip
                  label={s.useCase}
                  size="small"
                  sx={{
                    height: 18,
                    fontSize: 9,
                    bgcolor: t.chatArchitectBg,
                    color: t.chatArchitectFg,
                  }}
                />
                <Typography
                  sx={{ fontFamily: t.body, fontWeight: 700, fontSize: 14, color: t.ink }}
                >
                  {s.title}
                </Typography>
                <Box sx={{ flexGrow: 1 }} />
                {bindingChip(s, t)}
                {statusChip?.(s)}
              </Box>
            )}
          />

          {/* the path's guards: what must hold for this scenario to be the one taken */}
          {active.guards.length > 0 ? (
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, flexWrap: 'wrap' }}>
              <Typography
                sx={{ fontFamily: t.mono, fontSize: 10, letterSpacing: '0.08em', color: t.muted }}
              >
                GUARDS
              </Typography>
              {active.guards.map((g) => (
                <Chip
                  key={`${g.at}:${g.guard}`}
                  label={`${g.guard} @ ${g.at}`}
                  size="small"
                  sx={{
                    fontFamily: t.mono,
                    fontSize: 10,
                    bgcolor: t.paperAlt,
                    color: t.ink,
                    border: `1px solid ${t.line}`,
                  }}
                />
              ))}
            </Box>
          ) : null}

          {active.preconditions.hints.length > 0 ||
          active.preconditions.expectedStates.length > 0 ? (
            <Box sx={{ borderLeft: `3px solid ${t.line}`, pl: 1.25, py: 0.25 }}>
              <Typography
                sx={{ fontFamily: t.mono, fontSize: 9, letterSpacing: '0.08em', color: t.muted }}
              >
                GIVEN
              </Typography>
              <Typography
                sx={{ fontFamily: t.body, fontSize: 12.5, color: t.ink, lineHeight: 1.5 }}
              >
                {[
                  ...active.preconditions.expectedStates.map((x) => `${x.object} ${x.inState}`),
                  ...active.preconditions.hints,
                ].join(' · ')}
              </Typography>
            </Box>
          ) : null}

          <DynamicViewFlow
            detailBySeq={seq.detailBySeq}
            dv={seq.dv}
            height={440}
            resetKey={active.id}
            statusBySeq={seq.statusBySeq}
            {...(onCommentStep ? { onCommentStep } : {})}
          />

          <BindingDetails s={active} t={t} />
        </Box>
      ) : null}
    </Box>
  );
}
