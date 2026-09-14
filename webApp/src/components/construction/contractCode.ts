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
import type { ContractOp, ContractStruct } from '../../contracts/types';

/** Below this many pixels of width the code diagram cannot be read; list instead. */
export const CODE_CANVAS_MIN_WIDTH = 900;

/** The furthest a fit may zoom the code diagram out. Past it, the reader pans. */
export const CODE_MIN_ZOOM = 0.9;

export type CodeTabMode = 'canvas' | 'list';

/**
 * The canvas only in the focus view, and only with room for it. The pane is at
 * most 820px wide, so it always lists; the focus view lists too when the window
 * leaves its artifact column under CODE_CANVAS_MIN_WIDTH (e.g. 1100 or 500).
 * An unmeasured width (0) lists: the safe reading until the first measure.
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
