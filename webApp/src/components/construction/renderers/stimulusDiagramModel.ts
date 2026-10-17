/**
 * One test-plan STIMULUS as a code-level diagram — pure (no React).
 *
 * The same left-to-right language as the contract's Interface tab
 * (codeDiagram/codeDiagramModel.ts):
 *
 *   LEFT    the caller («actor» Architect User, or the calling component), then
 *           what it passes in: plain values in one «parameters» class, each
 *           structured value (a UIAction, a ReviewFeedback) as its own class
 *   MIDDLE  the target component's «interface», the invoked operation highlighted
 *   RIGHT   what must come back — the «result» (or the «error» it must fail
 *           with), then every expected output («signal») and state («state»),
 *           each followed by the «proof» that reads it back, joined by a dotted
 *           edge
 *
 * A value the plan has not bound draws as a marked "not bound yet" row inside
 * its class, never as loose text. `stimulusDiagram` returns the boxes per side
 * plus a plain-text summary for a screen reader.
 */
import type { ProbeView } from '../../../contracts/types';
import { readableType } from '../contractCode.ts';
import {
  isRecordValue,
  owedRow,
  rowsFromValue,
  type ClassBoxSpec,
  type ClassRow,
} from '../codeDiagram/codeDiagramModel.ts';
import { readValue, summarize, valueSummary } from '../primitives/valueTree.ts';
import type { Expectation, Party, StimulusRow } from './scenarioPresentation';

export interface StimulusDiagram {
  left: ClassBoxSpec[];
  right: ClassBoxSpec[];
  /** The interface box: the target, the invoked operation, one line about it. */
  iface: { stereotype: string; title: string; op: string; detail: string };
  summary: string;
}

/** A party's stereotype, in words a first-time reader knows. */
function partyStereotype(p: Party): string {
  switch (p.kind) {
    case 'actor':
      return '«actor» a person using the system';
    case 'timer':
      return '«time event» a schedule fires';
    case 'unknown':
      return '«unresolved» not in the committed architecture';
    case 'component':
      return `«${p.layer ?? 'component'}»`;
  }
}

function proofBox(id: string, pr: ProbeView): ClassBoxSpec {
  const ex = pr.expect;
  const asks: ClassRow[] =
    pr.inputs.length === 0
      ? [
          {
            key: 'asks:none',
            name: 'with',
            value: 'no inputs',
            tone: 'muted',
            section: 'Asks with',
          },
        ]
      : pr.inputs.flatMap((a): ClassRow[] =>
          // A structured argument (a query) lists its fields under its own heading.
          isRecordValue(a.value)
            ? rowsFromValue(a.value, a.name, `asks:${a.name}:`).map((r) => ({
                ...r,
                section: `Asks with ${a.name}`,
              }))
            : [
                {
                  key: `asks:${a.name}`,
                  name: a.name,
                  value: valueSummary(a.value),
                  section: 'Asks with',
                },
              ]
        );
  const answers: ClassRow[] = ex.errorExpected
    ? [
        {
          key: 'answer:error',
          name: 'fails with',
          value: ex.errorCode.length > 0 ? ex.errorCode : 'any error',
          tone: 'danger',
          section: 'Must answer',
        },
      ]
    : ex.result.length > 0
      ? rowsFromValue(ex.result, 'answer', 'answer:').map((r) => ({ ...r, section: 'Must answer' }))
      : [
          {
            key: 'answer:any',
            name: 'answer',
            value: 'anything, without an error',
            tone: 'muted',
            section: 'Must answer',
          },
        ];
  return {
    id,
    role: 'proof',
    stereotype: `«proof» read back from ${pr.component}`,
    title: pr.operation,
    rows: [...asks, ...answers],
    provesAbove: true,
  };
}

/** An expected output or state, and — right below it — the proof that reads it. */
function expectationBoxes(
  e: Expectation,
  kind: 'signal' | 'state' | 'elsewhere',
  id: string
): ClassBoxSpec[] {
  const rows: ClassRow[] = [];
  if (e.to.length > 0) rows.push({ key: 'to', name: 'sent to', value: e.to });
  const p = e.proof;
  if (p.kind === 'unobservable') {
    rows.push({
      key: 'proof',
      name: 'proof',
      value: 'unobservable — nothing exposed can read it',
      tone: 'muted',
    });
  } else if (p.kind === 'missing' || p.kind === 'unbound') {
    rows.push(owedRow('proof', 'proof', 'not bound yet'));
  }
  const stereotype =
    kind === 'signal'
      ? '«signal» sent to an actor'
      : kind === 'state'
        ? '«state» true afterwards'
        : '«state» proved here, shown elsewhere on the path';
  const box: ClassBoxSpec = {
    id,
    role: 'output',
    stereotype,
    title: e.text,
    rows,
    provedBelow: p.kind === 'probe',
  };
  return p.kind === 'probe' ? [box, proofBox(`${id}-proof`, p.probe)] : [box];
}

/** `action 1`, `action 2`… — the numbered inputs one repeated param became. */
function sequenceOf(
  inputs: readonly StimulusRow['inputs'][number][],
  name: string
): { base: string; members: StimulusRow['inputs'][number][] } | undefined {
  const m = /^(.*) 1$/.exec(name);
  if (m?.[1] === undefined) return undefined;
  const base = m[1];
  const members = inputs.filter(
    (x) => x.name.startsWith(`${base} `) && /^\d+$/.test(x.name.slice(base.length + 1))
  );
  return members.length > 1 ? { base, members } : undefined;
}

/**
 * One step of a sequence as a row: its number and its `kind` as the name
 * ("1 navigate"), the other fields' values as the value ("amend-rationale · Add
 * the use case…"). A step with no `kind` is its summary.
 */
function stepRow(raw: string, k: number): { name: string; value: string } {
  const tree = readValue(raw);
  if (tree.kind !== 'record') return { name: String(k + 1), value: summarize(tree) };
  const kind = tree.fields.find((f) => f.key === 'kind');
  const rest = tree.fields.filter((f) => f.key !== 'kind').map((f) => summarize(f.value));
  return {
    name: `${String(k + 1)}${kind !== undefined ? ` ${summarize(kind.value)}` : ''}`,
    value: rest.join(' · '),
  };
}

export function stimulusDiagram(row: StimulusRow): StimulusDiagram {
  const left: ClassBoxSpec[] = [
    {
      id: 'caller',
      role: 'caller',
      stereotype: partyStereotype(row.caller),
      title: row.caller.name,
      rows: [],
      emptyText:
        row.via.length > 0 ? `also via ${row.via.map((v) => v.name).join(', ')}` : undefined,
    },
  ];

  // Plain values share one «parameters» class; a structured value is its own; a
  // SEQUENCE of one param (a frontend's UI actions: "action 1", "action 2"…) is
  // one class with a numbered row per step.
  const plain: ClassRow[] = [];
  const done = new Set<string>();
  for (const i of row.inputs) {
    if (done.has(i.name)) continue;
    const seq = sequenceOf(row.inputs, i.name);
    if (seq !== undefined) {
      for (const m of seq.members) done.add(m.name);
      left.push({
        id: `in-${seq.base}`,
        role: 'input',
        stereotype: `«${i.type.length > 0 ? readableType(i.type) : 'value'}» ${String(seq.members.length)} steps, in order`,
        title: `${seq.base} sequence`,
        titleTip: i.type,
        rows: seq.members.map((m, k) =>
          m.value === undefined
            ? owedRow(`s-${String(k)}`, String(k + 1))
            : { key: `s-${String(k)}`, ...stepRow(m.value, k) }
        ),
      });
      continue;
    }
    if (i.value !== undefined && isRecordValue(i.value)) {
      left.push({
        id: `in-${i.name}`,
        role: 'input',
        stereotype: `«${i.type.length > 0 ? readableType(i.type) : 'value'}» passed as ${i.name}`,
        title: i.name,
        titleTip: i.type,
        rows: rowsFromValue(i.value, i.name),
      });
    } else if (i.value === undefined) {
      plain.push(owedRow(`p-${i.name}`, i.name));
    } else {
      plain.push({
        key: `p-${i.name}`,
        name: i.name,
        value: valueSummary(i.value),
        valueTip: i.type.length > 0 ? `type ${i.type}` : undefined,
      });
    }
  }
  if (plain.length > 0) {
    left.splice(1, 0, {
      id: 'params',
      role: 'input',
      stereotype: '«parameters» plain values passed in',
      title: 'Inputs',
      rows: plain,
    });
  }
  if (row.inputs.length === 0) {
    left.push({
      id: 'params',
      role: 'input',
      stereotype: '«parameters»',
      title: 'No inputs',
      rows: [],
      emptyText: 'the contract states none',
    });
  }

  const right: ClassBoxSpec[] = [];
  const resultTitle = row.resultType.length > 0 ? readableType(row.resultType) : 'Result';
  switch (row.expect.kind) {
    case 'result':
      right.push({
        id: 'result',
        role: 'output',
        stereotype: '«result» must come back',
        title: resultTitle,
        titleTip: row.resultType,
        rows: rowsFromValue(row.expect.text, 'value'),
      });
      break;
    case 'error':
      right.push({
        id: 'result',
        role: 'error',
        stereotype: '«error» it must fail with',
        title: row.expect.text,
        rows: [],
      });
      break;
    case 'none':
      right.push({
        id: 'result',
        role: 'output',
        stereotype: '«result»',
        title: resultTitle,
        rows: [{ key: 'none', name: 'returns', value: 'without an error', tone: 'muted' }],
      });
      break;
    case 'missing':
      right.push({
        id: 'result',
        role: 'output',
        stereotype: '«result»',
        title: resultTitle,
        rows: [owedRow('owed', 'expected', 'a result or an error — not bound yet')],
      });
      break;
  }
  row.outputs.forEach((e, i) => right.push(...expectationBoxes(e, 'signal', `out-${String(i)}`)));
  row.states.forEach((e, i) => right.push(...expectationBoxes(e, 'state', `state-${String(i)}`)));
  row.alsoProves.forEach((e, i) =>
    right.push(...expectationBoxes(e, 'elsewhere', `also-${String(i)}`))
  );

  const op = row.op.length > 0 ? row.op : 'operation not resolved';
  const passed = left.filter((b) => b.role === 'input' && b.rows.length > 0);
  const detail = [
    passed.length > 0
      ? `takes ${passed.map((b) => (b.id === 'params' ? b.rows.map((r) => r.name).join(', ') : b.title)).join(', ')}`
      : 'takes nothing',
    row.resultType.length > 0
      ? `returns ${readableType(row.resultType)}`
      : 'returns nothing stated',
  ].join(' · ');

  const said = (b: ClassBoxSpec): string =>
    `${b.stereotype.replace(/[«»]/g, '')} ${b.title}${
      b.rows.length > 0
        ? `: ${b.rows.map((r) => `${r.name} ${r.value ?? ''}`.trim()).join(', ')}`
        : ''
    }`;
  const summary =
    `${row.caller.name} calls ${row.target.name} ${op}. ` +
    `Passed in: ${left.slice(1).map(said).join('; ') || 'nothing'}. ` +
    `Expected: ${right.map(said).join('; ')}.`;

  return {
    left,
    right,
    iface: {
      stereotype: `«interface» ${partyStereotype(row.target).replace(/[«»]/g, '')}`.trim(),
      title: row.target.name,
      op,
      detail,
    },
    summary,
  };
}
