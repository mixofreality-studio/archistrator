/**
 * How the test plan's scenario view READS one projected scenario — pure, so the
 * reading is tested without a renderer (node:test cannot load `.tsx`).
 *
 * The founder's finding on the first plan view: one call edge and a long id
 * "doesn't tell me nothing about the input/output/expectations or the context",
 * and it was wrong besides — every stimulus was drawn from a "Test harness" in
 * the Clients lane into the component pinned to the Managers lane, whatever its
 * layer and whoever really calls it. A scenario is a PATH through a use case's
 * activity diagram (spec §2.3) whose stimuli into this component (§2.4, A1) each
 * have a real caller (`InputEv.From`: an actor, a component, a timer), a contract
 * op, bound inputs, an expectation, and the outputs / states the path says must
 * be observable after it. Everything here is read off committed data:
 *
 *   header   the use case's name, trigger, actors and the guards the path takes
 *   path     the nodes and edges it walks on the use case's own diagram (plus the
 *            notes anchored to them), for ActivityFlow's highlight
 *   stimuli  one row per stimulus: caller → component.op (and the `via`
 *            alternatives, A4), the contract's params with the bound values, the
 *            expectation, each expected output/state with the probe that proves
 *            it (or `unobservable`), hints, and the binding fields still owed
 *   chain    the use case's own dynamic view restricted to the path, in path
 *            order: real participants in their real layers (the committed system
 *            model), the stimulus edges marked for status, detail and landing
 */
import type {
  C4Component,
  DynamicViewModel,
  PersonView,
  SequencedCall,
} from '../../../contracts/adapters';
import type {
  ActivityDiagram,
  ActivityNode,
  Layer,
  ProbeView,
  ScenarioStimulusView,
  ScenarioView,
  ServiceContract,
  ServiceContracts,
  StepBindingView,
  System,
  TestArgView,
  UseCase,
  UseCaseDecision,
} from '../../../contracts/types';
import type { StepDetail, StepStatus } from '../../flow/DynamicViewFlow';
import { valueSummary } from '../primitives/valueTree.ts';
import { altLabelsForStep } from '../../../contracts/realization.ts';

/** The committed models the reading joins against (any may be absent). */
export interface ScenarioContext {
  system: System | undefined;
  decisions: readonly UseCaseDecision[];
  contracts: ServiceContracts | undefined;
}

/** Who stands at one end of a stimulus. */
export interface Party {
  /** The id as the model writes it ('' for an un-called time event). */
  id: string;
  name: string;
  kind: 'actor' | 'component' | 'timer' | 'unknown';
  /** The committed layer, for a component. */
  layer?: Layer;
}

export interface GuardTaken {
  at: string;
  atLabel: string;
  guard: string;
}

export interface ScenarioHeader {
  useCaseId: string;
  useCaseName: string;
  /** The use case's index in the committed decisions (comment anchors), -1 when absent. */
  useCaseIndex: number;
  /** n of the `-P<n>` id suffix; 0 when the id carries none. */
  pathNumber: number;
  /** 'Client action' | 'Timer' | 'Bus message' | '' (no use case). */
  trigger: string;
  /** The label of the node the path enters by (its first non-start node). */
  entry: string;
  actors: string[];
  guards: GuardTaken[];
  /** Path nodes, excluding the start / end pseudo-nodes. */
  steps: number;
}

export type StimulusKind = 'actor input' | 'component call' | 'time event' | 'decision';

/** How an expected output or state is (or is not yet) proven. */
export type Proof =
  | { kind: 'probe'; text: string; probe: ProbeView }
  | { kind: 'unobservable' }
  /** bound, but neither probed nor declared unobservable — owed (TP-STATE). */
  | { kind: 'missing' }
  /** the step is not bound at all. */
  | { kind: 'unbound' };

export interface Expectation {
  /** The node id the expectation sits on (an objectNode / sendSignal / note). */
  node: string;
  text: string;
  /** For an output: the receiving actor's name ('' when none). */
  to: string;
  proof: Proof;
}

export interface InputRow {
  name: string;
  /** The contract param type ('' when the contract does not state it). */
  type: string;
  /** The bound value; undefined when the plan has not bound this param. */
  value: string | undefined;
}

export type ExpectRow =
  | { kind: 'result'; text: string }
  | { kind: 'error'; text: string }
  /** bound with no stated result (a void op, or a gap the gate reads). */
  | { kind: 'none' }
  | { kind: 'missing' };

export interface StimulusRow {
  seq: number;
  node: string;
  nodeLabel: string;
  kind: StimulusKind;
  caller: Party;
  /** The OTHER callers of an alt group collapsed onto this stimulus (A4). */
  via: Party[];
  target: Party;
  /** The contract op ('' when neither the projection nor the binding resolves one). */
  op: string;
  guard: string;
  inputs: InputRow[];
  /** The op's result type off the contract signature ('' when unknown / void). */
  resultType: string;
  expect: ExpectRow;
  outputs: Expectation[];
  states: Expectation[];
  /** Probes / unobservables the binding states for nodes the projection attached elsewhere. */
  alsoProves: Expectation[];
  hints: string[];
  hook: boolean;
  bound: boolean;
  skipped: boolean;
  /** The binding fields still owed, human-readable ([] = complete). */
  missing: string[];
}

export interface ScenarioChain {
  dv: DynamicViewModel;
  /** The system component the scenario is projected onto (the lit node). */
  focalComponentId: string | undefined;
  /** edge seq → stimulus seq, for every edge that IS a stimulus (via alternatives included). */
  stimulusByEdgeSeq: Map<number, number>;
  /** stimulus seq → 0-based edge index the step-through lands on. */
  edgeIndexByStimulus: Map<number, number>;
  statusBySeq: Map<number, StepStatus>;
  detailBySeq: Map<number, StepDetail>;
}

export interface ScenarioPresentation {
  header: ScenarioHeader;
  pickerLabel: string;
  path: { visitedNodes: Set<string>; visitedEdges: Set<string> };
  stimuli: StimulusRow[];
  chain: ScenarioChain;
}

/** framework-go/scenario.Normalize: lower-case letters and digits only. */
export function normalizeId(id: string): string {
  return id.toLowerCase().replace(/[^\p{L}\p{N}]/gu, '');
}

const TRIGGERS: Record<string, string> = {
  clientAction: 'Client action',
  timer: 'Timer',
  busMessage: 'Bus message',
};

/** The contract's layer string ("Engine", "ResourceAccess") as the model's Layer. */
function contractLayer(layer: string): Layer | undefined {
  const l = (layer[0] ?? '').toLowerCase() + layer.slice(1);
  switch (l) {
    case 'client':
    case 'manager':
    case 'engine':
    case 'resourceAccess':
    case 'resource':
    case 'utility':
      return l;
    default:
      return undefined;
  }
}

/** "reviewEngine" → "Review Engine" (a contract key with no system component). */
function humanize(key: string): string {
  const spaced = key.replace(/([a-z0-9])([A-Z])/g, '$1 $2');
  return spaced.length > 0 ? (spaced[0]?.toUpperCase() ?? '') + spaced.slice(1) : spaced;
}

/** The use case a scenario walks, its committed index, and the diagram it walks. */
function findUseCase(
  ctx: ScenarioContext,
  id: string
): { uc: UseCase | undefined; index: number; diagram: ActivityDiagram | undefined } {
  const index = ctx.decisions.findIndex((d) => d.useCase.id === id);
  const uc = ctx.decisions[index]?.useCase;
  let diagram = uc?.activity ?? undefined;
  if ((diagram?.nodes ?? []).length === 0 && uc?.variationOf != null) {
    diagram =
      ctx.decisions.find((d) => d.useCase.id === uc.variationOf)?.useCase.activity ?? undefined;
  }
  return { uc, index, diagram };
}

/** The contract key a (normalized) component id names, if any. */
function contractKeyFor(ctx: ScenarioContext, id: string): string | undefined {
  const n = normalizeId(id);
  return Object.keys(ctx.contracts ?? {}).find((k) => normalizeId(k) === n);
}

/**
 * The key the component's test plan is committed under (`testPlan[<key>]`): its
 * contract key. An activity names its component kebab ("review-engine"); the
 * plan, its bindings and every comment anchored in it are keyed camel
 * ("reviewEngine"). Falls back to the id as given when no contract matches.
 */
export function planKeyFor(contracts: ServiceContracts | undefined, component: string): string {
  return contractKeyFor({ system: undefined, decisions: [], contracts }, component) ?? component;
}

type SystemComponent = NonNullable<System['components']>[number];

function systemComponent(ctx: ScenarioContext, id: string): SystemComponent | undefined {
  const comps = ctx.system?.components ?? [];
  const exact = comps.find((c) => c.id === id);
  if (exact !== undefined) return exact;
  const n = normalizeId(id);
  return comps.find(
    (c) => normalizeId(c.id) === n || (c.contractKey != null && normalizeId(c.contractKey) === n)
  );
}

/**
 * One end of a stimulus, by the id the model writes. A contract key that is a
 * FACET of another component's contract (activityExecutionAccess on
 * projectStateAccess) resolves to the owning component, named with the facet.
 */
function resolveParty(ctx: ScenarioContext, uc: UseCase | undefined, id: string): Party {
  if (id.length === 0) return { id, name: 'Timer', kind: 'timer' };
  const actor = (uc?.actors ?? []).find((a) => a.id === id);
  if (actor !== undefined) return { id, name: actor.role, kind: 'actor' };
  const comp = systemComponent(ctx, id);
  if (comp !== undefined)
    return { id: comp.id, name: comp.name, kind: 'component', layer: comp.layer };
  const key = contractKeyFor(ctx, id);
  const contract: ServiceContract | undefined =
    key !== undefined ? ctx.contracts?.[key] : undefined;
  if (key !== undefined && contract !== undefined) {
    const owner = contract.component !== key ? systemComponent(ctx, contract.component) : undefined;
    if (owner !== undefined) {
      return {
        id: owner.id,
        name: `${owner.name} · ${key}`,
        kind: 'component',
        layer: owner.layer,
      };
    }
    const layer = contractLayer(contract.layer);
    return {
      id: key,
      name: humanize(key),
      kind: 'component',
      ...(layer !== undefined ? { layer } : {}),
    };
  }
  return { id, name: id, kind: 'unknown' };
}

function stimulusKind(kind: string): StimulusKind {
  if (kind === 'call') return 'component call';
  if (kind === 'timeEvent') return 'time event';
  if (kind === 'decision' || kind === 'switch') return 'decision';
  return 'actor input';
}

/** Splits a comma list at paren/bracket depth 0. */
function splitTop(s: string): string[] {
  const out: string[] = [];
  let depth = 0;
  let cur = '';
  for (const ch of s) {
    if (ch === '(' || ch === '[' || ch === '{') depth++;
    if (ch === ')' || ch === ']' || ch === '}') depth--;
    if (ch === ',' && depth === 0) {
      out.push(cur.trim());
      cur = '';
    } else {
      cur += ch;
    }
  }
  if (cur.trim().length > 0) out.push(cur.trim());
  return out;
}

/** An op's params (name + type) and result type, read off its signature. */
function opSignature(
  contract: ServiceContract | undefined,
  op: string
): { params: { name: string; type: string }[]; result: string } | undefined {
  if (contract === undefined || op.length === 0) return undefined;
  const found = (contract.ops ?? []).find((o) => o.signature.trim().startsWith(`${op}(`));
  if (found === undefined) return undefined;
  const sig = found.signature.trim();
  const open = sig.indexOf('(');
  let depth = 0;
  let close = -1;
  for (let i = open; i < sig.length; i++) {
    if (sig[i] === '(') depth++;
    if (sig[i] === ')') depth--;
    if (depth === 0) {
      close = i;
      break;
    }
  }
  const params = splitTop(sig.slice(open + 1, close < 0 ? sig.length : close))
    .map((p) => {
      const colon = p.indexOf(':');
      return colon < 0
        ? { name: p, type: '' }
        : { name: p.slice(0, colon).trim(), type: p.slice(colon + 1).trim() };
    })
    .filter((p) => p.name.length > 0 && p.type !== 'context.Context');
  const arrow = sig.indexOf('→', close);
  let ret = arrow < 0 ? '' : sig.slice(arrow + 1).trim();
  if (ret.startsWith('(') && ret.endsWith(')')) ret = ret.slice(1, -1);
  const result = splitTop(ret).filter((r) => !/(^|\.)error$/i.test(r) && !r.endsWith('Error'));
  return { params, result: result.join(', ') };
}

function argList(args: readonly TestArgView[]): string {
  return args.map((a) => `${a.name}=${a.value}`).join(', ');
}

function probeText(p: ProbeView): string {
  const outcome = p.expect.errorExpected
    ? ` → error ${p.expect.errorCode}`
    : p.expect.result.length > 0
      ? ` → ${p.expect.result}`
      : '';
  return `${p.component}.${p.operation}(${argList(p.inputs)})${outcome}`;
}

function proofFor(node: string, step: StepBindingView | undefined): Proof {
  if (step === undefined) return { kind: 'unbound' };
  const probe = step.probes.find((p) => p.for === node);
  if (probe !== undefined) return { kind: 'probe', text: probeText(probe), probe };
  if (step.unobservable.includes(node)) return { kind: 'unobservable' };
  return { kind: 'missing' };
}

/** What a diagram node says must be observable, as one line. */
function nodeExpectationText(n: ActivityNode | undefined, id: string): string {
  if (n === undefined) return id;
  if (n.kind === 'objectNode') return `${n.label} ${n.inState ?? ''}`.trim();
  return n.label.length > 0 ? n.label : id;
}

function stimulusRow(
  s: ScenarioView,
  st: ScenarioStimulusView,
  ctx: ScenarioContext,
  uc: UseCase | undefined,
  nodes: Map<string, ActivityNode>
): StimulusRow {
  const skipped = s.binding?.skip !== undefined;
  const step = skipped ? undefined : s.binding?.steps.find((x) => x.seq === st.seq);
  const bound = step !== undefined;
  const caller = resolveParty(ctx, uc, st.input.from);
  const via = st.input.via.filter((v) => v !== st.input.from).map((v) => resolveParty(ctx, uc, v));
  const target = resolveParty(ctx, uc, st.input.to);
  const op = st.input.op.length > 0 ? st.input.op : (step?.operation ?? '');
  const key = contractKeyFor(ctx, st.input.to);
  const sig = opSignature(key !== undefined ? ctx.contracts?.[key] : undefined, op);

  const args = step?.inputs ?? [];
  // A param can be bound SEVERAL times — a frontend stimulus is a sequence of UI
  // actions, each an `action` arg. Every bound value is kept, numbered in order
  // ("action 1", "action 2"…); keeping only the first silently dropped the rest.
  const inputs: InputRow[] = [];
  const named = (name: string, type: string): void => {
    const bound = args.filter((a) => a.name === name);
    if (bound.length <= 1) {
      inputs.push({ name, type, value: bound[0]?.value });
      return;
    }
    bound.forEach((a, k) => {
      inputs.push({ name: `${name} ${String(k + 1)}`, type, value: a.value });
    });
  };
  for (const p of sig?.params ?? []) named(p.name, p.type);
  const seen = new Set((sig?.params ?? []).map((p) => p.name));
  for (const a of args) {
    if (seen.has(a.name)) continue;
    seen.add(a.name);
    named(a.name, a.schemaRef.replace(/^#\/\$defs\//, ''));
  }

  let expect: ExpectRow;
  if (step === undefined) expect = { kind: 'missing' };
  else if (step.expect.errorExpected)
    expect = {
      kind: 'error',
      text: step.expect.errorCode.length > 0 ? step.expect.errorCode : 'any error',
    };
  else if (step.expect.result.length > 0) expect = { kind: 'result', text: step.expect.result };
  else expect = { kind: 'none' };

  const outputs: Expectation[] = st.outcome.expectedOutputs.map((o) => ({
    node: o.node,
    text: o.label.length > 0 ? o.label : nodeExpectationText(nodes.get(o.node), o.node),
    to: o.to.length > 0 ? resolveParty(ctx, uc, o.to).name : '',
    proof: proofFor(o.node, step),
  }));
  const states: Expectation[] = st.outcome.expectedStates.map((x) => ({
    node: x.node,
    text: `${x.object} ${x.inState}`.trim(),
    to: '',
    proof: proofFor(x.node, step),
  }));
  const covered = new Set([...outputs, ...states].map((e) => e.node));
  const alsoProves: Expectation[] = [];
  for (const p of step?.probes ?? []) {
    if (covered.has(p.for)) continue;
    covered.add(p.for);
    alsoProves.push({
      node: p.for,
      text: nodeExpectationText(nodes.get(p.for), p.for),
      to: '',
      proof: { kind: 'probe', text: probeText(p), probe: p },
    });
  }
  for (const u of step?.unobservable ?? []) {
    if (covered.has(u)) continue;
    covered.add(u);
    alsoProves.push({
      node: u,
      text: nodeExpectationText(nodes.get(u), u),
      to: '',
      proof: { kind: 'unobservable' },
    });
  }

  const missing: string[] = [];
  if (!skipped) {
    if (step === undefined) {
      // Nothing bound: name every field the binding owes, not just "binding".
      missing.push(s.binding === undefined ? 'binding' : `step ${String(st.seq)}`);
      if (op.length === 0) missing.push('operation');
      for (const i of inputs) missing.push(`input ${i.name}`);
      missing.push('expected result or error');
      for (const e of [...outputs, ...states]) missing.push(`proof for “${e.text}”`);
    } else {
      if (op.length === 0) missing.push('operation');
      for (const i of inputs) if (i.value === undefined) missing.push(`input ${i.name}`);
      if (expect.kind === 'none' && (sig?.result ?? '').length > 0) missing.push('expected result');
      for (const e of [...outputs, ...states]) {
        if (e.proof.kind === 'missing') missing.push(`proof for “${e.text}”`);
      }
    }
  }

  return {
    seq: st.seq,
    node: st.node,
    nodeLabel: nodes.get(st.node)?.label ?? st.node,
    kind: stimulusKind(st.input.kind),
    caller,
    via,
    target,
    op,
    guard: st.input.guard,
    inputs,
    resultType: sig?.result ?? '',
    expect,
    outputs,
    states,
    alsoProves,
    hints: st.outcome.hints,
    hook: step?.hook ?? false,
    bound,
    skipped,
    missing,
  };
}

/** The call provenance `<useCase>/<node>/<callIndex>[.<k>]`, parsed. */
function parseCall(call: string): { node: string; index: number } | undefined {
  const m = /\/([^/]+)\/(\d+)(?:\.\d+)?$/.exec(call);
  if (m === null) return undefined;
  return { node: m[1] ?? '', index: Number(m[2]) };
}

/** One stimulus waiting to be placed on the chain: its row, and the realized
 *  call (index into the node's CallStep) it rides, if any. */
interface Pending {
  row: StimulusRow;
  index: number | undefined;
}

const TIMER_PERSON: PersonView = { id: 'time-event', role: 'Timer' };

function stepDetail(row: StimulusRow, step: StepBindingView | undefined): StepDetail {
  const proves = [
    ...row.outputs.map((o) => `output ${o.text}${o.to.length > 0 ? ` → ${o.to}` : ''}`),
    ...row.states.map((x) => x.text),
    ...row.hints,
  ];
  const expect = step?.expect;
  return {
    // The step caption is plain text: each value is said as one readable line,
    // never the harness's JSON (valueTree.valueSummary).
    // Numbered where a name repeats (a UI action sequence), so every value shows once.
    inputs: (step?.inputs ?? []).map((a, k, all) => ({
      name:
        all.filter((x) => x.name === a.name).length > 1
          ? `${a.name} ${String(all.slice(0, k + 1).filter((x) => x.name === a.name).length)}`
          : a.name,
      value: valueSummary(a.value),
    })),
    ...(expect !== undefined && expect.result.length > 0
      ? { result: valueSummary(expect.result) }
      : {}),
    errorExpected: expect?.errorExpected ?? false,
    ...(expect !== undefined && expect.errorCode.length > 0 ? { errorCode: expect.errorCode } : {}),
    ...(proves.length > 0 ? { assertion: proves.join(' · ') } : {}),
  };
}

function buildChain(
  s: ScenarioView,
  rows: readonly StimulusRow[],
  ctx: ScenarioContext,
  uc: UseCase | undefined,
  nodes: Map<string, ActivityNode>,
  focal: Party
): ScenarioChain {
  const view = (ctx.system?.dynamicViews ?? []).find((v) => v.useCaseId === s.useCase);
  const stepsByNode = new Map((view?.steps ?? []).map((st) => [st.activityNodeId, st]));

  // The stimuli that ride a realized call, by node, and those that do not.
  const byNode = new Map<string, Pending[]>();
  for (const [i, st] of s.stimuli.entries()) {
    const row = rows[i];
    if (row === undefined) continue;
    const parsed = parseCall(st.input.call);
    const realized =
      view !== undefined &&
      parsed?.node === st.node &&
      stepsByNode.get(st.node)?.calls?.[parsed.index] !== undefined
        ? parsed.index
        : undefined;
    const list = byNode.get(st.node) ?? [];
    list.push({ row, index: realized });
    byNode.set(st.node, list);
  }

  type Raw = Omit<SequencedCall, 'seq'> & { stimulus?: number };
  const raw: Raw[] = [];
  const synth = (row: StimulusRow, node: string): Raw => ({
    from: row.caller.kind === 'timer' ? TIMER_PERSON.id : row.caller.id,
    to: row.target.id,
    mode: 'sync',
    label: row.op.length > 0 ? `${row.op}()` : '(operation unresolved)',
    stepNodeId: node,
    stepLabel: nodes.get(node)?.label ?? node,
    callInStep: 1,
    callsInStep: 1,
    stimulus: row.seq,
  });

  // A loop is unrolled once, so a node may recur: each visit takes the stimuli
  // left for that node in order (a node's stimuli land on its FIRST visit — the
  // projection emits them per visit in the same order, so this pairs them).
  const consumed = new Map<string, number>();
  const pathNodes = view === undefined ? s.stimuli.map((st) => st.node) : s.path;
  const seenNode = new Set<string>();
  for (const nodeId of pathNodes) {
    if (view === undefined && seenNode.has(nodeId)) continue;
    seenNode.add(nodeId);
    const pending = byNode.get(nodeId) ?? [];
    const visit = consumed.get(nodeId) ?? 0;
    consumed.set(nodeId, visit + 1);
    // the stimuli of THIS visit: on a node visited once (the elementary path) all of them
    const mine = visit === 0 ? pending : [];
    for (const p of mine) if (p.index === undefined) raw.push(synth(p.row, nodeId));
    if (view === undefined) continue;
    const calls = stepsByNode.get(nodeId)?.calls ?? [];
    const alts = altLabelsForStep(calls);
    calls.forEach((c, i) => {
      const direct = mine.find((p) => p.index === i);
      // an alt alternative into the same callee is the same stimulus (A4)
      let stimulus = direct?.row.seq;
      if (stimulus === undefined && c.alt != null && c.alt.length > 0) {
        for (const p of mine) {
          const head = p.index !== undefined ? calls[p.index] : undefined;
          if (head?.alt === c.alt && normalizeId(head.to) === normalizeId(c.to)) {
            stimulus = p.row.seq;
          }
        }
      }
      const altLabel = alts[i];
      raw.push({
        from: c.from,
        to: c.to,
        mode: c.mode,
        label: c.label,
        stepNodeId: nodeId,
        stepLabel: nodes.get(nodeId)?.label ?? nodeId,
        callInStep: i + 1,
        callsInStep: calls.length,
        ...(altLabel !== undefined ? { altLabel } : {}),
        ...(stimulus !== undefined ? { stimulus } : {}),
      });
    });
  }

  // Participants: system components in their committed layers; the use case's
  // actors (and a timer) as people; anything else is surfaced, never dropped.
  const participants: C4Component[] = [];
  const persons: PersonView[] = [];
  const unresolved: string[] = [];
  const placed = new Set<string>();
  const place = (id: string): void => {
    if (placed.has(id)) return;
    placed.add(id);
    if (id === TIMER_PERSON.id) {
      persons.push(TIMER_PERSON);
      return;
    }
    const party = resolveParty(ctx, uc, id);
    if (party.kind === 'actor') persons.push({ id, role: party.name });
    else if (party.kind === 'component' && party.layer !== undefined) {
      const comp = systemComponent(ctx, id);
      participants.push({
        id,
        name: comp?.name ?? party.name,
        kind: comp?.kind ?? party.layer,
        layer: party.layer,
        encapsulates: '',
        encapsulatesVolatilities: [],
        contractKey: comp?.contractKey ?? '',
      });
    } else unresolved.push(id);
  };
  for (const r of raw) {
    place(r.from);
    place(r.to);
  }

  const edges: SequencedCall[] = [];
  const stimulusByEdgeSeq = new Map<number, number>();
  const edgeIndexByStimulus = new Map<number, number>();
  const statusBySeq = new Map<number, StepStatus>();
  const detailBySeq = new Map<number, StepDetail>();
  const rowBySeq = new Map(rows.map((r) => [r.seq, r]));
  raw.forEach((r, i) => {
    const { stimulus, ...call } = r;
    const seq = i + 1;
    edges.push({ ...call, seq });
    if (stimulus === undefined) return;
    const row = rowBySeq.get(stimulus);
    if (row === undefined) return;
    stimulusByEdgeSeq.set(seq, stimulus);
    if (!edgeIndexByStimulus.has(stimulus)) edgeIndexByStimulus.set(stimulus, i);
    statusBySeq.set(seq, row.bound || row.skipped ? 'planned' : 'red');
    const step = s.binding?.steps.find((x) => x.seq === stimulus);
    detailBySeq.set(seq, stepDetail(row, row.skipped ? undefined : step));
  });

  const focalComponentId = placed.has(focal.id) ? focal.id : undefined;
  return {
    dv: { title: view?.title ?? s.title, participants, persons, edges, unresolved },
    focalComponentId,
    stimulusByEdgeSeq,
    edgeIndexByStimulus,
    statusBySeq,
    detailBySeq,
  };
}

export function presentScenario(
  s: ScenarioView,
  component: string,
  ctx: ScenarioContext
): ScenarioPresentation {
  const { uc, index, diagram } = findUseCase(ctx, s.useCase);
  const nodes = new Map((diagram?.nodes ?? []).map((n) => [n.id, n]));
  const name = uc?.name ?? s.useCase;
  const pathNumber = Number(/-P(\d+)$/.exec(s.id)?.[1] ?? '0');
  const guards: GuardTaken[] = s.guards.map((g) => ({
    at: g.at,
    atLabel: nodes.get(g.at)?.label ?? g.at,
    guard: g.guard,
  }));
  const entryId = s.path.find((id) => nodes.get(id)?.kind !== 'start') ?? '';
  const header: ScenarioHeader = {
    useCaseId: s.useCase,
    useCaseName: name,
    useCaseIndex: uc !== undefined ? index : -1,
    pathNumber,
    trigger: uc !== undefined ? (TRIGGERS[uc.trigger] ?? uc.trigger) : '',
    entry: nodes.get(entryId)?.label ?? entryId,
    actors: (uc?.actors ?? []).map((a) => a.role),
    guards,
    steps: s.path.filter((id) => {
      const k = nodes.get(id)?.kind;
      return k !== 'start' && k !== 'end';
    }).length,
  };
  const guardText = guards.map((g) => g.guard.replace(/^\[|\]$/g, '')).filter((g) => g.length > 0);
  const pickerLabel =
    name +
    (guardText.length > 0 ? ` — ${guardText.join(' · ')}` : '') +
    (pathNumber > 0 ? ` · path ${String(pathNumber)}` : '');

  const visitedNodes = new Set(s.path);
  const visitedEdges = new Set<string>();
  for (let i = 1; i < s.path.length; i++)
    visitedEdges.add(`${s.path[i - 1] ?? ''}-${s.path[i] ?? ''}`);
  for (const n of diagram?.nodes ?? []) {
    if (n.kind === 'note' && n.anchor !== undefined && visitedNodes.has(n.anchor)) {
      visitedNodes.add(n.id);
      visitedEdges.add(`${n.id}-${n.anchor}`);
    }
  }

  const rows = s.stimuli.map((st) => stimulusRow(s, st, ctx, uc, nodes));
  const focal = resolveParty(ctx, uc, component);
  return {
    header,
    pickerLabel,
    path: { visitedNodes, visitedEdges },
    stimuli: rows,
    chain: buildChain(s, rows, ctx, uc, nodes, focal),
  };
}
