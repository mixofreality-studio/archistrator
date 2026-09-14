/**
 * The Code tab's two forms, and the op → struct reading they share — pure, so
 * the rules are tested without a renderer (node:test cannot load `.tsx`).
 *
 * WHY TWO FORMS (designer check on renderers S1, B1)
 * -------------------------------------------------
 * ContractCodeFlow draws the «interface» node at a fixed 560px and an expanded
 * op adds 224px struct columns on either side (~1100px). The detail pane is
 * 360–820px, so the canvas fit itself to ~6px signatures there, and an expanded
 * op re-fit to ~4px: unreadable. So below CODE_CANVAS_MIN_WIDTH the tab is an
 * HTML signature list (12px mono, wrapping, each row expanding inline into its
 * request / response / error tables), and the canvas draws in the FOCUS VIEW
 * only — where it never fits below CODE_MIN_ZOOM, and pans instead.
 */
import type { ContractOp, ContractStruct, GoField } from '../../contracts/types';

// ---------------------------------------------------------------------------
// The canvas geometry, and the width it needs (designer recheck on S2, N1)
//
// Three FIXED columns: request | «interface» | response. Every node is a fixed
// width (border-box), so an expansion's width does not depend on what the op
// carries, only on whether it has both columns. Of the 29 committed contracts,
// 148 ops have both a request and a response, so the widest expansion is the
// full three-column span, measured in the browser at exactly CODE_EXPANDED_WIDTH
// across every op of every contract (renderers-s3-report.md).
//
// S2 drew the canvas from a 900px column and panned an expansion wider than it:
// at 1280 and 1366 the struct cards landed 0% / 22% on the canvas. Now the
// canvas draws only where the widest expansion fits at CODE_MIN_ZOOM; below that
// the focus view lists, and the list already expands each op's tables inline.
// ---------------------------------------------------------------------------

/** The «interface» node's width. */
export const CODE_IFACE_W = 560;
/** A struct column's width. */
export const CODE_STRUCT_W = 224;
/** Request column → interface: room for the call's edge label. */
export const CODE_INPUT_GAP = 300;
/** Interface → response column. */
export const CODE_OUTPUT_GAP = 150;

/** The widest expansion, in canvas pixels: request + gap + interface + gap + response. */
export const CODE_EXPANDED_WIDTH =
  CODE_STRUCT_W + CODE_INPUT_GAP + CODE_IFACE_W + CODE_OUTPUT_GAP + CODE_STRUCT_W;

/** The furthest a fit may zoom the code diagram out. Past it, the reader pans. */
export const CODE_MIN_ZOOM = 0.9;

/** Clear space kept on either side of the widest expansion, in screen pixels. */
export const CODE_CANVAS_GUTTER = 14;

/** The canvas frame's two 1.5px borders. */
const CODE_CANVAS_BORDERS = 3;

/**
 * Below this many pixels of column width the code diagram cannot show the widest
 * expansion at CODE_MIN_ZOOM; list instead. 1458 × 0.9 → 1313, plus the gutters
 * and the frame: 1344 (a focus window of about 1712px).
 */
export const CODE_CANVAS_MIN_WIDTH =
  Math.ceil(CODE_EXPANDED_WIDTH * CODE_MIN_ZOOM) + 2 * CODE_CANVAS_GUTTER + CODE_CANVAS_BORDERS;

/**
 * The canvas height a drawing needs: its measured height at CODE_MIN_ZOOM plus
 * the gutters and the frame, never below the caller's base height. The canvas
 * GROWS to it on expand, so a tall request column (10 params on
 * RecordActivityBranchOpened) lands on the canvas rather than past its edge.
 */
export function codeCanvasHeightFor(boundsHeight: number, baseHeight: number): number {
  return Math.max(
    baseHeight,
    Math.ceil(boundsHeight * CODE_MIN_ZOOM) + 2 * CODE_CANVAS_GUTTER + CODE_CANVAS_BORDERS
  );
}

export type CodeTabMode = 'canvas' | 'list';

/**
 * The canvas only in the focus view, and only with room for it. The pane is at
 * most 820px wide, so it always lists; the focus view lists too when the window
 * leaves its artifact column under CODE_CANVAS_MIN_WIDTH — every window below
 * about 1712px (500, 1100, 1280, 1366, 1600). An unmeasured width (0) lists: the
 * safe reading until the first measure.
 */
export function codeTabModeFor(width: number, inFocus: boolean): CodeTabMode {
  return inFocus && width >= CODE_CANVAS_MIN_WIDTH ? 'canvas' : 'list';
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
