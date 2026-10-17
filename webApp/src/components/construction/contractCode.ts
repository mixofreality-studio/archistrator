/**
 * The Code tab's two forms, and the op → struct reading they share — pure, so
 * the rules are tested without a renderer (node:test cannot load `.tsx`).
 *
 * WHY TWO FORMS
 * -------------
 * ContractCodeFlow draws the contract as a UML-class diagram, left to right,
 * about 950px wide. Where a column is narrower than CODE_CANVAS_MIN_WIDTH (a phone, a
 * narrow side pane) the tab is the HTML operation list instead — each op as its
 * name, labelled inputs and results, expanding inline into its request /
 * response / error tables. Neither form ever shows a signature string.
 */
import type { ContractOp, ContractStruct, GoField } from '../../contracts/types';
import { DIAGRAM_MIN_COLUMN_WIDTH } from './codeDiagram/codeDiagramModel.ts';

// ---------------------------------------------------------------------------
// The canvas — the shared code-level diagram (codeDiagram/codeDiagramModel.ts):
// three columns read left to right, what the selected operation takes | the
// «interface» | what it returns and fails with. About 950px wide, it draws at
// full size in the activity's artifact column at 1440; narrower than
// CODE_CANVAS_MIN_WIDTH (≈840px: a phone, a narrow side pane) the tab lists.
// ---------------------------------------------------------------------------

/** Below this column width the Interface tab lists instead of drawing. */
export const CODE_CANVAS_MIN_WIDTH = DIAGRAM_MIN_COLUMN_WIDTH;

export type CodeTabMode = 'canvas' | 'list';

/**
 * The diagram wherever there is room for it — the activity's artifact column, the
 * focus view — and the labelled list below that. An unmeasured width (0) lists:
 * the safe reading until the first measure.
 */
export function codeTabModeFor(width: number): CodeTabMode {
  return width >= CODE_CANVAS_MIN_WIDTH ? 'canvas' : 'list';
}

// ---------------------------------------------------------------------------
// An operation's SHAPE — what the reader is shown instead of its signature.
//
// The contract states each op as one Go-ish line (`Foo(a: A, b: *B) → (C,
// fwm.Error)`). That line is the generator's input; the reviewer sees its parts:
// the operation's name, each input by name with its type, what it returns, and
// what it fails with — as labelled fields and diagram rows.
// ---------------------------------------------------------------------------

/** Splits a comma list at bracket depth 0 (a `map[K]V` or `func(a, b)` stays whole). */
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
    } else cur += ch;
  }
  if (cur.trim().length > 0) out.push(cur.trim());
  return out;
}

export interface OpParam {
  name: string;
  /** The type as the contract states it (`*ReviewFeedback`, `[]AnchoredComment`). */
  type: string;
}

export interface OpShape {
  /** The operation's name (`DispatchActivityTask`). */
  name: string;
  /** Its inputs, the context parameter dropped (it is plumbing, not a contract term). */
  params: OpParam[];
  /** What a success returns (types), errors excluded. */
  returns: string[];
  /** What it fails with (error types). */
  errors: string[];
}

/** An op's name, inputs, results and errors, read off its signature line. */
export function opShape(signature: string): OpShape {
  const open = signature.indexOf('(');
  const name = (open < 0 ? signature : signature.slice(0, open)).trim();
  let depth = 0;
  let close = -1;
  for (let i = Math.max(open, 0); i < signature.length; i++) {
    const ch = signature[i];
    if (ch === '(') depth++;
    if (ch === ')') {
      depth--;
      if (depth === 0) {
        close = i;
        break;
      }
    }
  }
  const inner = open < 0 ? '' : signature.slice(open + 1, close < 0 ? signature.length : close);
  const params = splitTop(inner)
    .map((p): OpParam => {
      const colon = p.indexOf(':');
      return colon < 0
        ? { name: p.trim(), type: '' }
        : { name: p.slice(0, colon).trim(), type: p.slice(colon + 1).trim() };
    })
    .filter(
      (p) =>
        p.name.length > 0 &&
        p.name !== 'ctx' &&
        p.type !== 'context.Context' &&
        p.type !== 'Context'
    );
  const arrow = signature.indexOf('→', close < 0 ? 0 : close);
  let ret = arrow < 0 ? '' : signature.slice(arrow + 1).trim();
  if (ret.startsWith('(') && ret.endsWith(')')) ret = ret.slice(1, -1);
  const results = splitTop(ret).filter((r) => r.length > 0);
  return {
    name,
    params,
    returns: results.filter((r) => !isErrorStructName(r.replace(/^\*/, ''))),
    errors: results.filter((r) => isErrorStructName(r.replace(/^\*/, ''))),
  };
}

/**
 * A type as a reader says it: no pointer star, a slice as "list of X", a map as
 * "map of K to V", a package qualifier dropped (`fwm.Error` → `Error`; the full
 * name stays available as a tooltip where it is drawn).
 */
export function readableType(type: string): string {
  let t = type.trim();
  if (t.length === 0) return '';
  t = t.replace(/^\*/, '');
  if (t.startsWith('[]')) return `list of ${readableType(t.slice(2))}`;
  const map = /^map\[([^\]]+)\](.+)$/.exec(t);
  if (map !== null) return `map of ${readableType(map[1] ?? '')} to ${readableType(map[2] ?? '')}`;
  const dot = t.lastIndexOf('.');
  return dot >= 0 ? t.slice(dot + 1) : t;
}

/** The error type every op of a contract shares, if they all share one (said once, in the header). */
export function sharedErrorOf(ops: readonly ContractOp[]): string | undefined {
  let shared: string | undefined;
  for (const op of ops) {
    const errs = opShape(op.signature).errors;
    if (errs.length !== 1) return undefined;
    const e = errs[0];
    if (shared === undefined) shared = e;
    else if (shared !== e) return undefined;
  }
  return shared;
}

// ---------------------------------------------------------------------------
// Signature parsing — the fallback struct names an op carries in its signature.
//
//   foo(intent: DesignPhaseIntent) → DesignPhaseAck
//   bar(ctx: Context, cmd: BuildCommand) → (BuildResult, error)
//   baz() → error
// ---------------------------------------------------------------------------

export function parseSignature(sig: string): { inputNames: string[]; outputNames: string[] } {
  const parenMatch = /\(([^)]*)\)/.exec(sig);
  const inputNames: string[] = [];
  const parenContent = parenMatch?.[1] ?? '';
  if (parenContent.trim().length > 0) {
    for (const param of parenContent.split(',')) {
      const colonIdx = param.indexOf(':');
      const typePart = colonIdx >= 0 ? param.slice(colonIdx + 1) : param;
      const name = typePart.trim().replace(/^\*/, '').replace(/\[\]/, '');
      if (name.length > 0 && name !== 'ctx' && name !== 'context.Context') {
        inputNames.push(name);
      }
    }
  }

  const arrowIdx = sig.indexOf('→');
  const outputNames: string[] = [];
  if (arrowIdx >= 0) {
    let returnPart = sig.slice(arrowIdx + 1).trim();
    if (returnPart.startsWith('(') && returnPart.endsWith(')')) {
      returnPart = returnPart.slice(1, -1);
    }
    for (const part of returnPart.split(',')) {
      const name = part.trim().replace(/^\*/, '').replace(/\[\]/, '');
      if (name.length > 0) outputNames.push(name);
    }
  }

  return { inputNames, outputNames };
}

/** A struct read off the contract, or named only by the signature (`fallback`). */
export type ResolvedStruct = ContractStruct & { _fallback?: boolean };

/** The contract's own structs, else the signature's names with no fields. */
export function resolveStructs(
  structs: ContractStruct[] | undefined,
  fallbackNames: string[]
): ResolvedStruct[] {
  if (structs !== undefined && structs.length > 0) return structs;
  return fallbackNames.map((name) => ({ name, fields: [], _fallback: true }));
}

/**
 * A PARAMETER, not a struct: a primitive or an alias the contract wraps as a
 * one-field struct named by its own type — `string { tickID: string }`,
 * `ProjectID { projectID: ProjectID }`, `fwm.Error { fault: fwm.Error }`. Drawn
 * as a struct it repeated the type as a table header ("string / tickID string");
 * it is one row, `tickID  string` (designer recheck on S2). Undefined for a real
 * struct.
 */
export function paramOf(struct: ContractStruct): GoField | undefined {
  if (struct.fields.length !== 1) return undefined;
  const field = struct.fields[0];
  return field?.type === struct.name ? field : undefined;
}

/** An error struct, by the contract's naming convention. */
export function isErrorStructName(name: string): boolean {
  return /^error$/i.test(name) || name.endsWith('Error') || name.endsWith('Err');
}

export interface OpStructs {
  request: ResolvedStruct[];
  response: ResolvedStruct[];
  error: ResolvedStruct[];
}

/** One op's request, response and error structs — the three tables a list row expands into. */
export function opStructsFor(op: ContractOp): OpStructs {
  const { inputNames, outputNames } = parseSignature(op.signature);
  const outputs = resolveStructs(op.outputs, outputNames);
  return {
    request: resolveStructs(op.inputs, inputNames),
    response: outputs.filter((s) => !isErrorStructName(s.name)),
    error: outputs.filter((s) => isErrorStructName(s.name)),
  };
}

/**
 * A diagram column's classes: each run of plain single-value inputs folded into
 * ONE «parameters» class (name → type rows, like a UML class's attributes), every
 * real struct its own class, and an error never folded — it is drawn apart, in red.
 */
export type ColumnSegment =
  | { kind: 'params'; params: GoField[] }
  | { kind: 'struct'; struct: ResolvedStruct; isError: boolean };

export function columnSegments(structs: readonly ResolvedStruct[]): ColumnSegment[] {
  const out: ColumnSegment[] = [];
  for (const s of structs) {
    const isError = isErrorStructName(s.name);
    const param = isError ? undefined : paramOf(s);
    const last = out[out.length - 1];
    if (param === undefined) out.push({ kind: 'struct', struct: s, isError });
    else if (last?.kind === 'params') last.params.push(param);
    else out.push({ kind: 'params', params: [param] });
  }
  return out;
}
