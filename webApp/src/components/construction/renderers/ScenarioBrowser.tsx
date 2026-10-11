import { type ReactNode, useContext, useMemo, useState } from 'react';
import Box from '@mui/material/Box';
import Chip from '@mui/material/Chip';
import Typography from '@mui/material/Typography';
import FormControl from '@mui/material/FormControl';
import Select from '@mui/material/Select';
import MenuItem from '@mui/material/MenuItem';
import { UI_IDENTIFIERS } from '../../../utilities/constants/UIIdentifiers';
import type { ScenarioView } from '../../../contracts/types';
import type { SequencedCall } from '../../../contracts/adapters';
import { toUseCaseView } from '../../../contracts/useCaseViews';
import type { Tokens } from '../../../utilities/theme/themes';
import { DynamicViewFlow } from '../../flow/DynamicViewFlow';
import { layerColors } from '../../flow/flowLayout';
import { ActivityFlow, type ActivityHighlight } from '../../usecase/ActivityFlow';
import {
  CommentsOff,
  useComments,
  testPlanBindingAnchor,
  testPlanStepAnchor,
} from '../../comments/CommentContext';
import { CommentableList } from '../../comments/CommentableList';
import { ScenarioLinkContext } from './scenarioLink';
import { activeScenarioId } from './scenarioSelection.ts';
import {
  presentScenario,
  type Expectation,
  type Party,
  type ScenarioContext,
  type ScenarioPresentation,
  type StimulusRow,
} from './scenarioPresentation';

/** A small uppercase section label (the plan view's typographic convention). */
function Eyebrow({ t, children }: { t: Tokens; children: ReactNode }): ReactNode {
  return (
    <Typography
      sx={{ fontFamily: t.mono, fontSize: 9.5, letterSpacing: '0.08em', color: t.muted, mb: 0.25 }}
    >
      {children}
    </Typography>
  );
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

/** A participant's name, tagged with its layer in that layer's colour (the diagrams' legend). */
function PartyName({ p, t }: { p: Party; t: Tokens }): ReactNode {
  const colors = layerColors(t);
  const color =
    p.kind === 'component' && p.layer !== undefined
      ? colors[p.layer]
      : p.kind === 'unknown'
        ? t.dangerFg
        : colors.person;
  const tag =
    p.kind === 'actor'
      ? 'actor'
      : p.kind === 'timer'
        ? 'time'
        : p.kind === 'unknown'
          ? 'unresolved'
          : (p.layer ?? '');
  return (
    <Box component="span" sx={{ whiteSpace: 'nowrap' }}>
      <Box component="span" sx={{ fontWeight: 700, color: t.ink }}>
        {p.name}
      </Box>
      {tag.length > 0 ? (
        <Box
          component="span"
          sx={{
            ml: 0.5,
            px: 0.5,
            fontFamily: t.mono,
            fontSize: 8.5,
            letterSpacing: '0.04em',
            border: `1px solid ${color}`,
            color,
            borderRadius: 0.5,
            verticalAlign: 'middle',
          }}
        >
          {tag}
        </Box>
      ) : null}
    </Box>
  );
}

function kindColor(kind: StimulusRow['kind'], t: Tokens): string {
  switch (kind) {
    case 'actor input':
      return t.accent;
    case 'component call':
      return t.accent2;
    case 'time event':
      return t.awaitingFg;
    case 'decision':
      return t.muted;
  }
}

function KindChip({ kind, t }: { kind: StimulusRow['kind']; t: Tokens }): ReactNode {
  const c = kindColor(kind, t);
  return (
    <Chip
      label={kind}
      size="small"
      sx={{
        height: 18,
        fontFamily: t.mono,
        fontSize: 9,
        fontWeight: 700,
        bgcolor: 'transparent',
        color: c,
        border: `1.5px solid ${c}`,
      }}
    />
  );
}

/** How one expected output/state is proven, as one line. */
function ProofLine({ e, t }: { e: Expectation; t: Tokens }): ReactNode {
  const p = e.proof;
  switch (p.kind) {
    case 'probe':
      return (
        <Box
          component="span"
          sx={{ fontFamily: t.mono, fontSize: 11, color: t.ink, wordBreak: 'break-all' }}
        >
          <Box component="span" sx={{ color: t.committedFg, fontWeight: 700 }}>
            probe{' '}
          </Box>
          {p.text}
        </Box>
      );
    case 'unobservable':
      return (
        <Box component="span" sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}>
          declared unobservable — no probe
        </Box>
      );
    case 'missing':
      return (
        <Box component="span" sx={{ fontFamily: t.mono, fontSize: 11, color: t.dangerFg }}>
          no probe, not declared unobservable — owed
        </Box>
      );
    case 'unbound':
      return (
        <Box component="span" sx={{ fontFamily: t.mono, fontSize: 11, color: t.dangerFg }}>
          unbound — needs a probe (component.op + expect) or “unobservable”
        </Box>
      );
  }
}

/** The outputs / states / also-proves a stimulus carries, each with its proof. */
function ExpectationList({
  title,
  items,
  shape,
  t,
}: {
  title: string;
  items: Expectation[];
  shape: 'signal' | 'object' | 'other';
  t: Tokens;
}): ReactNode {
  if (items.length === 0) return null;
  return (
    <Box sx={{ minWidth: 0 }}>
      <Eyebrow t={t}>{title}</Eyebrow>
      <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.5 }}>
        {items.map((e) => (
          <Box
            key={e.node}
            sx={{ display: 'flex', flexWrap: 'wrap', alignItems: 'baseline', columnGap: 0.75 }}
          >
            {/* the node's UML shape in miniature: a send-signal flag, an object box */}
            <Box
              aria-hidden
              component="span"
              sx={{
                display: 'inline-block',
                width: 14,
                height: 10,
                border: `1.5px solid ${t.ink}`,
                borderRadius: shape === 'other' ? '3px' : 0,
                clipPath:
                  shape === 'signal'
                    ? 'polygon(0 0, 70% 0, 100% 50%, 70% 100%, 0 100%)'
                    : undefined,
                bgcolor: shape === 'signal' ? t.ink : 'transparent',
                flexShrink: 0,
              }}
            />
            <Typography component="span" sx={{ fontFamily: t.body, fontSize: 12.5, color: t.ink }}>
              {e.text}
              {e.to.length > 0 ? (
                <Box component="span" sx={{ color: t.muted }}>
                  {` → ${e.to}`}
                </Box>
              ) : null}
            </Typography>
            <ProofLine e={e} t={t} />
          </Box>
        ))}
      </Box>
    </Box>
  );
}

function expectText(row: StimulusRow): string {
  switch (row.expect.kind) {
    case 'result':
      return row.expect.text;
    case 'error':
      return `error ${row.expect.text}`;
    case 'none':
      return 'no result stated (returns without error)';
    case 'missing':
      return '— unbound: a result or an expected error';
  }
}

/** One stimulus, read whole: who calls what with which values, and what must follow. */
function StimulusCard({
  row,
  selected,
  onSelect,
  t,
}: {
  row: StimulusRow;
  selected: boolean;
  onSelect: () => void;
  t: Tokens;
}): ReactNode {
  return (
    <Box
      data-selected={selected ? 'true' : 'false'}
      data-seq={row.seq}
      data-testid={UI_IDENTIFIERS.Construction.SCENARIO_STIMULUS}
      sx={{
        display: 'flex',
        flexDirection: 'column',
        gap: 1,
        pl: 1.25,
        py: 0.75,
        borderLeft: `3px solid ${selected ? t.accent : t.line}`,
        cursor: 'pointer',
        minWidth: 0,
      }}
      onClick={onSelect}
    >
      {/* line 1: seq, kind, caller → component.op */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap', minWidth: 0 }}>
        <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 12, color: t.muted }}>
          {`#${String(row.seq)}`}
        </Typography>
        <KindChip kind={row.kind} t={t} />
        <Typography
          component="span"
          sx={{ fontFamily: t.body, fontSize: 13.5, color: t.ink, wordBreak: 'break-word' }}
        >
          <PartyName p={row.caller} t={t} />
          <Box component="span" sx={{ color: t.muted, mx: 0.75 }}>
            →
          </Box>
          <PartyName p={row.target} t={t} />
          <Box component="span" sx={{ fontFamily: t.mono, fontWeight: 700, color: t.ink }}>
            {row.op.length > 0 ? ` .${row.op}()` : ''}
          </Box>
          {row.op.length === 0 ? (
            <Box component="span" sx={{ fontFamily: t.mono, color: t.dangerFg, ml: 0.75 }}>
              op unresolved
            </Box>
          ) : null}
        </Typography>
        <Box sx={{ flexGrow: 1 }} />
        {row.hook ? (
          <Chip
            label="hook"
            size="small"
            sx={{ height: 18, fontFamily: t.mono, fontSize: 9, bgcolor: t.paperAlt, color: t.ink }}
          />
        ) : null}
      </Box>
      {/* line 2: where on the path, the guard, the alternative callers */}
      <Typography
        component="div"
        sx={{ fontFamily: t.body, fontSize: 12, color: t.muted, lineHeight: 1.45 }}
      >
        {`at “${row.nodeLabel}”`}
        {row.guard.length > 0 ? (
          <Box component="span" sx={{ fontFamily: t.mono, color: t.ink }}>
            {`  ·  guard ${row.guard}`}
          </Box>
        ) : null}
        {row.via.length > 0 ? (
          <Box component="span">
            {'  ·  also reachable via '}
            {row.via.map((v, i) => (
              <Box component="span" key={v.id}>
                {i > 0 ? ', ' : ''}
                <PartyName p={v} t={t} />
              </Box>
            ))}
          </Box>
        ) : null}
      </Typography>

      {/* inputs | expected result */}
      <Box
        sx={{
          display: 'grid',
          gridTemplateColumns: { xs: 'minmax(0,1fr)', md: 'minmax(0,1.2fr) minmax(0,1fr)' },
          gap: 1.5,
        }}
      >
        <Box sx={{ minWidth: 0 }}>
          <Eyebrow t={t}>INPUTS</Eyebrow>
          {row.inputs.length === 0 ? (
            <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}>
              none stated by the contract
            </Typography>
          ) : (
            <Box
              sx={{
                display: 'grid',
                gridTemplateColumns: 'max-content minmax(0,1fr)',
                columnGap: 1.25,
                rowGap: 0.25,
              }}
            >
              {row.inputs.map((i) => (
                <Box key={i.name} sx={{ display: 'contents' }}>
                  <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.ink }}>
                    {i.name}
                    {i.type.length > 0 ? (
                      <Box component="span" sx={{ color: t.muted }}>{` ${i.type}`}</Box>
                    ) : null}
                  </Typography>
                  <Typography
                    sx={{
                      fontFamily: t.mono,
                      fontSize: 11,
                      color: i.value === undefined ? t.dangerFg : t.ink,
                      wordBreak: 'break-all',
                    }}
                  >
                    {i.value === undefined ? '— unbound' : `= ${i.value}`}
                  </Typography>
                </Box>
              ))}
            </Box>
          )}
        </Box>
        <Box sx={{ minWidth: 0 }}>
          <Eyebrow t={t}>
            {`EXPECTED${row.resultType.length > 0 ? ` · returns ${row.resultType}` : ''}`}
          </Eyebrow>
          <Typography
            sx={{
              fontFamily: t.mono,
              fontSize: 11,
              wordBreak: 'break-all',
              color:
                row.expect.kind === 'missing' || row.expect.kind === 'error' ? t.dangerFg : t.ink,
            }}
          >
            {expectText(row)}
          </Typography>
        </Box>
      </Box>

      <ExpectationList
        items={row.outputs}
        shape="signal"
        t={t}
        title="EXPECTED OUTPUTS (sendSignal)"
      />
      <ExpectationList
        items={row.states}
        shape="object"
        t={t}
        title="EXPECTED STATES (objectNode [inState])"
      />
      <ExpectationList
        items={row.alsoProves}
        shape="other"
        t={t}
        title="THE BINDING ALSO PROVES (attached to other stimuli on the path)"
      />

      {row.hints.length > 0 ? (
        <Box>
          <Eyebrow t={t}>HINTS (anchored notes)</Eyebrow>
          {row.hints.map((h) => (
            <Typography
              key={h}
              sx={{ fontFamily: t.body, fontSize: 12, color: t.ink, lineHeight: 1.45 }}
            >
              {h}
            </Typography>
          ))}
        </Box>
      ) : null}

      {row.missing.length > 0 ? (
        <Box
          data-testid={UI_IDENTIFIERS.Construction.SCENARIO_MISSING}
          sx={{ display: 'flex', alignItems: 'center', gap: 0.5, flexWrap: 'wrap' }}
        >
          <Typography
            sx={{ fontFamily: t.mono, fontSize: 9.5, letterSpacing: '0.08em', color: t.dangerFg }}
          >
            MISSING
          </Typography>
          {row.missing.map((m) => (
            <Chip
              key={m}
              label={m}
              size="small"
              sx={{
                height: 18,
                fontFamily: t.mono,
                fontSize: 9,
                bgcolor: 'transparent',
                color: t.dangerFg,
                border: `1px solid ${t.dangerFg}`,
              }}
            />
          ))}
        </Box>
      ) : null}
    </Box>
  );
}

/** The scenario's header: what a reviewer reads first. */
function ScenarioHeaderCard({
  s,
  p,
  t,
  statusChip,
}: {
  s: ScenarioView;
  p: ScenarioPresentation;
  t: Tokens;
  statusChip: ((s: ScenarioView) => ReactNode) | undefined;
}): ReactNode {
  const h = p.header;
  const target = p.stimuli[0]?.target;
  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.SCENARIO_HEADER}
      sx={{ display: 'flex', flexDirection: 'column', gap: 0.5, minWidth: 0 }}
    >
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
        <Typography sx={{ fontFamily: t.body, fontWeight: 700, fontSize: 16, color: t.ink }}>
          {h.useCaseName}
        </Typography>
        {h.pathNumber > 0 ? (
          <Chip
            label={`path ${String(h.pathNumber)}`}
            size="small"
            sx={{ height: 18, fontSize: 9, bgcolor: t.chatArchitectBg, color: t.chatArchitectFg }}
          />
        ) : null}
        <Box sx={{ flexGrow: 1 }} />
        {bindingChip(s, t)}
        {statusChip?.(s)}
      </Box>
      <Box
        sx={{
          display: 'grid',
          gridTemplateColumns: 'max-content minmax(0,1fr)',
          columnGap: 1.25,
          rowGap: 0.25,
          alignItems: 'baseline',
        }}
      >
        <Eyebrow t={t}>TRIGGER</Eyebrow>
        <Typography sx={{ fontFamily: t.body, fontSize: 12.5, color: t.ink }}>
          {h.trigger.length > 0
            ? h.trigger
            : 'unknown (the use case is not in the committed model)'}
          {h.entry.length > 0 ? (
            <Box component="span" sx={{ color: t.muted }}>{` — “${h.entry}”`}</Box>
          ) : null}
        </Typography>
        <Eyebrow t={t}>ACTORS</Eyebrow>
        <Typography sx={{ fontFamily: t.body, fontSize: 12.5, color: t.ink }}>
          {h.actors.length > 0 ? h.actors.join(' · ') : '—'}
        </Typography>
        <Eyebrow t={t}>GUARDS TAKEN</Eyebrow>
        <Typography component="div" sx={{ fontFamily: t.body, fontSize: 12.5, color: t.ink }}>
          {h.guards.length === 0
            ? 'none — the path takes no branch'
            : h.guards.map((g, i) => (
                <Box component="span" key={`${g.at}:${g.guard}:${String(i)}`}>
                  {i > 0 ? '  ·  ' : ''}
                  <Box component="span" sx={{ fontFamily: t.mono, fontWeight: 700 }}>
                    {g.guard}
                  </Box>
                  <Box component="span" sx={{ color: t.muted }}>{` at “${g.atLabel}”`}</Box>
                </Box>
              ))}
        </Typography>
        <Eyebrow t={t}>COVERS</Eyebrow>
        <Typography component="div" sx={{ fontFamily: t.body, fontSize: 12.5, color: t.ink }}>
          {`${String(h.steps)} steps on the path · ${String(p.stimuli.length)} ${p.stimuli.length === 1 ? 'stimulus' : 'stimuli'} into `}
          {target !== undefined ? <PartyName p={target} t={t} /> : null}
        </Typography>
      </Box>
      <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted }}>{s.id}</Typography>
    </Box>
  );
}

/**
 * Scenario browser for a component's deterministic test plan (spec §2.4, §3,
 * §6.4 plan lens). Pick a projected scenario; it reads whole:
 *
 *   header    the use case's name, trigger, actors and the guards the path takes
 *   stimuli   one row per stimulus into this component (CommentableList, anchored
 *             `…bindings[scenario=<id>].steps[seq=<n>]`): the real caller → op,
 *             the contract's params with the bound values, the expectation, each
 *             expected output / state with the probe that proves it (or
 *             `unobservable`), hints, and what the binding still owes
 *   path      the path on the use case's own activity diagram (every node, the
 *             path lit, guards on the taken edges, UML shapes, note connectors)
 *   chain     the use case's call chain along the path: the real callers in their
 *             committed layers, the stimulus edges marked; selecting a stimulus
 *             lands the step-through on it and rings its node on the path
 *
 * `linked` makes the scenario follow the URL's `sc` deep link (scenarioLink.ts):
 * the plan's own browser takes it, so a component's "reached through" row opens
 * at the scenario it names. A browser inside another pane keeps its own pick.
 */
export function ScenarioBrowser({
  scenarios,
  component,
  context,
  t,
  statusChip,
  linked = false,
}: {
  scenarios: ScenarioView[];
  /** The component the scenarios are projected onto (the anchor's `testPlan[<comp>]`). */
  component: string;
  /** The committed models the scenarios are read against (absent ones degrade). */
  context: ScenarioContext;
  t: Tokens;
  statusChip?: (s: ScenarioView) => ReactNode;
  linked?: boolean;
}): ReactNode {
  const { setAnchor } = useComments();
  const link = useContext(ScenarioLinkContext);
  const deepLink = linked ? link : undefined;
  const [selectedId, setSelectedId] = useState<string>('');
  const [selectedSeq, setSelectedSeq] = useState<{ scenario: string; seq: number } | null>(null);
  const activeId = activeScenarioId(scenarios, [deepLink?.scenarioId, selectedId]);
  const active = scenarios.find((s) => s.id === activeId);
  const presentations = useMemo(
    () => new Map(scenarios.map((s) => [s.id, presentScenario(s, component, context)])),
    [scenarios, component, context]
  );
  const p = active !== undefined ? presentations.get(active.id) : undefined;
  const seq = selectedSeq?.scenario === activeId ? selectedSeq.seq : (active?.stimuli[0]?.seq ?? 0);

  const decision =
    p !== undefined && p.header.useCaseIndex >= 0
      ? context.decisions[p.header.useCaseIndex]
      : undefined;
  const ucView = useMemo(
    () => (decision !== undefined ? toUseCaseView(decision) : undefined),
    [decision]
  );
  const highlight: ActivityHighlight | undefined = useMemo(() => {
    const pr = presentations.get(activeId);
    if (pr === undefined) return undefined;
    // the same pick as `seq` below, read off state so the memo keys on it directly
    const at = selectedSeq?.scenario === activeId ? selectedSeq.seq : pr.stimuli[0]?.seq;
    return {
      current: pr.stimuli.find((r) => r.seq === at)?.node ?? '',
      visitedNodes: pr.path.visitedNodes,
      visitedEdges: pr.path.visitedEdges,
    };
  }, [presentations, activeId, selectedSeq]);

  // Arm an anchored comment from the call chain's caption: a stimulus edge anchors
  // its step; any other call on the path anchors the scenario (its context).
  const onCommentStep =
    active !== undefined && p !== undefined
      ? (edge: SequencedCall): void => {
          const stim = p.chain.stimulusByEdgeSeq.get(edge.seq);
          setAnchor(
            stim !== undefined
              ? {
                  kind: 'node',
                  label: `${edge.label} (stimulus ${String(stim)})`,
                  source: `${component} · ${active.id}`,
                  jsonPath: testPlanStepAnchor(component, active.id, stim),
                }
              : {
                  kind: 'node',
                  label: `${edge.label} (call ${String(edge.seq)} on the path)`,
                  source: `${component} · ${active.id}`,
                  jsonPath: testPlanBindingAnchor(component, active.id),
                }
          );
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
        <FormControl size="small" sx={{ flex: '1 1 auto', minWidth: 0, maxWidth: 640 }}>
          <Select
            data-testid={UI_IDENTIFIERS.Construction.SCENARIO_PICKER}
            inputProps={{ 'aria-label': 'Scenario' }}
            sx={{ fontFamily: t.body, fontSize: 13 }}
            value={activeId}
            onChange={(e) => {
              setSelectedId(e.target.value);
              deepLink?.onScenarioChange(e.target.value);
            }}
          >
            {scenarios.map((s) => (
              <MenuItem key={s.id} sx={{ fontFamily: t.body, fontSize: 13 }} value={s.id}>
                {presentations.get(s.id)?.pickerLabel ?? s.title}
              </MenuItem>
            ))}
          </Select>
        </FormControl>
      </Box>

      {active !== undefined && p !== undefined ? (
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5, minWidth: 0 }}>
          {/* The scenario-level anchor (spec §6.4): `…testPlan[<comp>].bindings[scenario=<id>]`,
              the binding as a whole — a skip reason, a missing binding. */}
          <CommentableList
            ariaLabel="Scenario binding"
            getAnchor={(s) => ({
              kind: 'node',
              label: `scenario:${s.id}`,
              source: `${component} · ${s.id}`,
              jsonPath: testPlanBindingAnchor(component, s.id),
            })}
            getKey={(s) => s.id}
            getLabel={() => p.pickerLabel}
            getLabelKind={() => 'scenario binding'}
            items={[active]}
            renderItem={(s) => <ScenarioHeaderCard p={p} s={s} statusChip={statusChip} t={t} />}
          />

          {active.preconditions.hints.length > 0 ||
          active.preconditions.expectedStates.length > 0 ? (
            <Box sx={{ borderLeft: `3px solid ${t.line}`, pl: 1.25, py: 0.25 }}>
              <Eyebrow t={t}>GIVEN (before the first stimulus)</Eyebrow>
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

          {active.binding?.skip !== undefined ? (
            <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.ink }}>
              <Box
                component="span"
                sx={{ fontFamily: t.mono, fontWeight: 700, color: t.awaitingFg }}
              >
                SKIPPED{' '}
              </Box>
              {active.binding.skip.reason} — until {active.binding.skip.until}. A skipped scenario
              is not a pass; the testing gate counts it.
            </Typography>
          ) : active.binding === undefined ? (
            <Typography
              data-testid={UI_IDENTIFIERS.Construction.SCENARIO_UNBOUND}
              sx={{ fontFamily: t.body, fontSize: 12, color: t.dangerFg }}
            >
              No binding yet: the test plan has not bound this scenario. Everything below is derived
              from the use case and the contracts; each stimulus is a target until it carries
              concrete inputs, an expectation and a proof for every expected state.
            </Typography>
          ) : null}

          <Box sx={{ minWidth: 0 }}>
            <Eyebrow t={t}>
              {`STIMULI · ${String(p.stimuli.length)} · select one to place it on the path and in the call chain`}
            </Eyebrow>
            <CommentableList
              ariaLabel="Scenario stimuli"
              gap={0.75}
              getAnchor={(r) => ({
                kind: 'node',
                label: `${r.caller.name} → ${r.target.name}.${r.op} (stimulus ${String(r.seq)})`,
                source: `${component} · ${active.id}`,
                jsonPath: testPlanStepAnchor(component, active.id, r.seq),
              })}
              getKey={(r) => `${active.id}#${String(r.seq)}`}
              getLabel={(r) => `#${String(r.seq)} ${r.caller.name} → ${r.target.name}.${r.op}`}
              getLabelKind={() => 'stimulus'}
              items={p.stimuli}
              renderItem={(r) => (
                <StimulusCard
                  row={r}
                  selected={r.seq === seq}
                  t={t}
                  onSelect={() => {
                    setSelectedSeq({ scenario: active.id, seq: r.seq });
                  }}
                />
              )}
            />
          </Box>

          <Box data-testid={UI_IDENTIFIERS.Construction.SCENARIO_PATH} sx={{ minWidth: 0 }}>
            <Eyebrow t={t}>
              {`PATH THROUGH “${p.header.useCaseName}” · lit: the nodes and edges this scenario takes · centred and ringed: stimulus #${String(seq)} · the fit control frames the whole diagram`}
            </Eyebrow>
            {ucView !== undefined && highlight !== undefined ? (
              <CommentsOff>
                <ActivityFlow
                  focusFirst
                  height={600}
                  highlight={highlight}
                  uc={ucView}
                  useCaseIndex={p.header.useCaseIndex}
                />
              </CommentsOff>
            ) : (
              <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.muted }}>
                {`The use case “${active.useCase}” is not in the committed core use cases, so its activity diagram cannot be drawn. Path: ${active.path.join(' → ')}`}
              </Typography>
            )}
          </Box>

          <Box data-testid={UI_IDENTIFIERS.Construction.SCENARIO_CHAIN} sx={{ minWidth: 0 }}>
            <Eyebrow t={t}>
              {`CALL CHAIN ALONG THE PATH · ${String(p.chain.dv.edges.length)} calls · stimulus calls carry the “target” tag`}
            </Eyebrow>
            <DynamicViewFlow
              detailBySeq={p.chain.detailBySeq}
              dv={p.chain.dv}
              height={460}
              initialStep={p.chain.edgeIndexByStimulus.get(seq) ?? 0}
              resetKey={`${active.id}#${String(seq)}`}
              statusBySeq={p.chain.statusBySeq}
              {...(p.chain.focalComponentId !== undefined
                ? { focalComponentId: p.chain.focalComponentId }
                : {})}
              {...(onCommentStep ? { onCommentStep } : {})}
            />
          </Box>
        </Box>
      ) : null}
    </Box>
  );
}
