/**
 * THE CODE-LEVEL DIAGRAM'S SHARED LANGUAGE — pure (no React).
 *
 * Two views draw a C4 level-4 (code) diagram: the service contract's Interface
 * tab (ContractCodeFlow) and each test-plan stimulus (StimulusDiagram). Both
 * read LEFT TO RIGHT, in three columns:
 *
 *   LEFT    what goes in    — the caller, the «parameters» / «request» classes
 *   MIDDLE  the interface   — the component's «interface» class, the operation
 *                             under discussion highlighted
 *   RIGHT   what comes out  — «result» / «response», «error», and (for a
 *                             stimulus) the expected states, each followed by
 *                             the «proof» that reads it back
 *
 * Edges run from each left class INTO the highlighted operation's row and OUT of
 * that row to each right class, coloured by role (the caption is the legend; no
 * edge labels, per the diagram conventions). This module holds the geometry, the
 * role colours, the edge builder and the class-box row model, so the two views
 * cannot drift apart.
 */
import { MarkerType, type Edge } from '@xyflow/react';

import type { Tokens } from '../../../utilities/theme/themes';
import { readValue, summarize, type ValueTree } from '../primitives/valueTree.ts';

// ---------------------------------------------------------------------------
// Geometry
// ---------------------------------------------------------------------------

/** The left column (what goes in). */
export const DIAGRAM_LEFT_W = 250;
/** The middle column (the «interface»). */
export const DIAGRAM_MID_W = 300;
/** The right column (what comes out). */
export const DIAGRAM_RIGHT_W = 270;
/** The gap between columns: room for the edges to turn. */
export const DIAGRAM_GAP = 64;
/** The gap between stacked classes in one column. */
export const DIAGRAM_STACK_GAP = 14;

export const DIAGRAM_MID_X = DIAGRAM_LEFT_W + DIAGRAM_GAP;
export const DIAGRAM_RIGHT_X = DIAGRAM_MID_X + DIAGRAM_MID_W + DIAGRAM_GAP;
/** The widest drawing, in canvas pixels. */
export const DIAGRAM_WIDTH = DIAGRAM_RIGHT_X + DIAGRAM_RIGHT_W;

/** The furthest a fit zooms out; below it the view lists instead of drawing. */
export const DIAGRAM_MIN_ZOOM = 0.85;
/** Clear space either side of the drawing, in screen pixels. */
export const DIAGRAM_GUTTER = 12;
/** The canvas frame's two 1.5px borders. */
export const DIAGRAM_BORDERS = 3;
/** The least canvas height. */
export const DIAGRAM_MIN_HEIGHT = 120;

/**
 * Below this column width the three columns cannot draw at DIAGRAM_MIN_ZOOM
 * without text shrinking past legibility, so both views fall back to their
 * labelled HTML list. About 840px: the activity column at 1440 (≈1000px) draws;
 * a phone or a narrow side pane lists.
 */
export const DIAGRAM_MIN_COLUMN_WIDTH =
  Math.ceil(DIAGRAM_WIDTH * DIAGRAM_MIN_ZOOM) + 2 * DIAGRAM_GUTTER + DIAGRAM_BORDERS;

export function diagramFits(width: number): boolean {
  return width >= DIAGRAM_MIN_COLUMN_WIDTH;
}

// ---------------------------------------------------------------------------
// Roles and colours
// ---------------------------------------------------------------------------

/**
 * What a class box IS in the diagram, which fixes its colour and its edge:
 * `caller` (who calls), `input` (what is passed in), `output` (what comes back,
 * an expected state or signal), `error` (a failure), `proof` (the read that
 * proves a state).
 */
export type DiagramRole = 'caller' | 'input' | 'output' | 'error' | 'proof';

export function roleColor(t: Tokens, role: DiagramRole): string {
  switch (role) {
    case 'caller':
      return t.ink;
    case 'input':
      return t.accent2;
    case 'output':
      return t.committedDot;
    case 'error':
      return t.dangerFg;
    case 'proof':
      return t.muted;
  }
}

/** The dash pattern an edge of this role draws with ('' = solid). */
export function roleDash(role: DiagramRole): string {
  if (role === 'error') return '6 3';
  if (role === 'proof') return '2 3';
  return '';
}

/** What each role's edge means, as the legend says it. */
export const ROLE_LEGEND: Readonly<Record<DiagramRole, string>> = {
  caller: 'calls',
  input: 'passed in',
  output: 'comes back',
  error: 'fails with',
  proof: 'proved by',
};

export function diagramEdge(
  t: Tokens,
  role: DiagramRole,
  edge: { source: string; target: string; sourceHandle: string; targetHandle: string }
): Edge {
  const stroke = roleColor(t, role);
  const dash = roleDash(role);
  return {
    id: `${edge.source}:${edge.sourceHandle}->${edge.target}:${edge.targetHandle}`,
    ...edge,
    type: 'smoothstep',
    style: { stroke, strokeWidth: 1.75, ...(dash.length > 0 ? { strokeDasharray: dash } : {}) },
    markerEnd: { type: MarkerType.ArrowClosed, color: stroke },
    focusable: false,
    selectable: false,
  };
}

// ---------------------------------------------------------------------------
// Class boxes
// ---------------------------------------------------------------------------

/**
 * One attribute row of a class box. `tone` marks what a reviewer must notice:
 * `owed` (not bound yet — drawn as a marked row, never loose text), `danger`
 * (an error), `muted` (a stated absence). `section` starts a sub-heading above
 * the row when it differs from the row before ("Asks with", "Must answer").
 */
export interface ClassRow {
  key: string;
  name: string;
  value?: string | undefined;
  /** The exact value or type, on hover, when the row shows a readable form of it. */
  valueTip?: string | undefined;
  note?: string | undefined;
  tone?: 'normal' | 'owed' | 'danger' | 'muted' | undefined;
  section?: string | undefined;
}

export interface ClassBoxSpec {
  id: string;
  /** The UML stereotype line, in words: «parameters» values passed in. */
  stereotype: string;
  title: string;
  /** The exact name on hover (a qualified type), when the title is a readable form. */
  titleTip?: string | undefined;
  role: DiagramRole;
  rows: ClassRow[];
  /** Said in the box when it has no rows and no title says enough. */
  emptyText?: string | undefined;
  /** This box is proved by the next box in its column (a dotted edge runs down). */
  provedBelow?: boolean | undefined;
  /** This box proves the box above it (its dotted edge arrives from above). */
  provesAbove?: boolean | undefined;
  testId?: string | undefined;
  nameTestId?: string | undefined;
  rowTestId?: string | undefined;
  /** `data-struct` — what an e2e test keys the box on. */
  dataStruct?: string | undefined;
}

/** A box's edge handle ids. */
export const boxHandle = {
  /** Where the box's in/out edge meets it (its inner side). */
  side: (id: string): string => `${id}:side`,
  /** The bottom of a state box, where its proof's dotted edge leaves. */
  proofOut: (id: string): string => `${id}:proof-out`,
  /** The top of a proof box, where that edge arrives. */
  proofIn: (id: string): string => `${id}:proof-in`,
};

/** The interface's highlighted-row handles. */
export const IFACE_IN = 'iface:in';
export const IFACE_OUT = 'iface:out';

function rowsFromTree(tree: ValueTree, fallbackName: string, prefix: string): ClassRow[] {
  switch (tree.kind) {
    case 'empty':
      return [
        { key: `${prefix}${fallbackName}`, name: fallbackName, value: 'none', tone: 'muted' },
      ];
    case 'text':
    case 'scalar':
      return [{ key: `${prefix}${fallbackName}`, name: fallbackName, value: tree.text }];
    case 'record':
      return tree.fields.map((f) => ({
        key: `${prefix}${f.key}`,
        name: f.key,
        value: summarize(f.value),
      }));
    case 'list':
      return tree.items.map((item, i) => {
        // A list of one-field records (`[{visible: x}, {visible: y}]`) reads as
        // that field per row; anything else is numbered.
        if (item.kind === 'record' && item.fields.length === 1) {
          const f = item.fields[0];
          if (f !== undefined) {
            return { key: `${prefix}${String(i)}`, name: f.key, value: summarize(f.value) };
          }
        }
        return { key: `${prefix}${String(i)}`, name: String(i + 1), value: summarize(item) };
      });
  }
}

/**
 * A bound value (JSON or text) as class-box rows: a record's fields one per row,
 * a list's items one per row, a scalar as one row named `fallbackName`. Never
 * the JSON itself.
 */
export function rowsFromValue(raw: string, fallbackName: string, prefix = ''): ClassRow[] {
  return rowsFromTree(readValue(raw), fallbackName, prefix);
}

/** Whether a bound value is a record — drawn as its own class rather than one row. */
export function isRecordValue(raw: string): boolean {
  return readValue(raw).kind === 'record';
}

/** The row an unbound value draws: marked, so it can never read as a value. */
export function owedRow(key: string, name: string, what = 'not bound yet'): ClassRow {
  return { key, name, value: what, tone: 'owed' };
}
