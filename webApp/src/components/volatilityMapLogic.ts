/**
 * Pure selection/keyboard logic for the VolatilityMap lanes, extracted so the
 * roving-tabindex + single-select semantics are unit-testable without a render
 * harness (volatilityMapLogic.test.ts — the glossaryLogic pattern).
 *
 * Each lane is a single-select WAI-ARIA listbox (role=option + aria-selected)
 * where selection does NOT follow focus: ↑/↓/Home/End move the roving tab stop
 * within the lane, Enter/Space select the focused option. Escape (handled at
 * the map root) clears the selection.
 */
import type { Axis, RejectionClass } from '../contracts/types';

/** What a key press inside a lane listbox should do. */
export type LaneKeyAction = { kind: 'move'; index: number } | { kind: 'select' } | { kind: 'none' };

const NONE: LaneKeyAction = { kind: 'none' };

/**
 * Interpret a key press on the focused option (position `index`) of a lane with
 * `count` items. Movement clamps at the lane bounds (no wrap — matches the
 * CommentableList roving-tabindex precedent).
 */
export function laneKeyAction(key: string, index: number, count: number): LaneKeyAction {
  if (count <= 0) return NONE;
  const clamp = (i: number): LaneKeyAction => ({
    kind: 'move',
    index: Math.max(0, Math.min(i, count - 1)),
  });
  switch (key) {
    case 'ArrowDown':
      return clamp(index + 1);
    case 'ArrowUp':
      return clamp(index - 1);
    case 'Home':
      return clamp(0);
    case 'End':
      return clamp(count - 1);
    case 'Enter':
    case ' ':
      return { kind: 'select' };
    default:
      return NONE;
  }
}

/** Human axis label shared by the inspect card and the live announcement. */
export function axisLabel(axis: Axis): string {
  return axis === 'sameCustomerOverTime' ? 'Axis 1 — over time' : 'Axis 2 — across customers';
}

/** The polite live-region message announcing the current selection. */
export function selectionAnnouncement(name: string, axis: Axis): string {
  return `Selected: ${name}, ${axisLabel(axis)}`;
}

/** Human label for a rejected candidate's classification (the class chip text). */
export function rejectionClassLabel(cls: RejectionClass): string {
  switch (cls) {
    case 'variableNotVolatile':
      return 'variable, not volatile';
    case 'natureOfTheBusiness':
      return 'nature of the business';
    case 'speculative':
      return 'speculative';
    case 'foldedInto':
      return 'folded into another';
  }
}

// ── Axes-overview geometry ───────────────────────────────────────────────────
// The compact two-axis diagram above the lanes draws each volatility as a dot
// ALONG ITS OWN AXIS LINE — evenly spaced by per-axis order, never a fabricated
// 2D coordinate (the scrapped scatter collapsed those onto a diagonal). Dots
// carry NO inline name labels (founder 2026-07-17): the name surfaces in a hover
// tooltip and a click selects the item, so the sketch stays a compact quadrant
// instead of a tall label-driven L. All geometry is computed here so it is
// unit-testable without an SVG harness.

/** A point in SVG user units (y grows downward, per SVG convention). */
export interface XY {
  x: number;
  y: number;
}

export interface AxesLayout {
  /** SVG viewport size — grows with the dot counts, never squeezes spacing. */
  width: number;
  height: number;
  /** The shared origin of both axes (bottom-left). */
  origin: XY;
  /** Arrow tip of the VERTICAL axis (Axis 1 — same customer, over time). */
  yArrowTip: XY;
  /** Arrow tip of the HORIZONTAL axis (Axis 2 — all customers, one moment). */
  xArrowTip: XY;
  /** Dot centers along the vertical axis, in item order (all share origin.x). */
  yDots: XY[];
  /** Dot centers along the horizontal axis, in item order (all share origin.y). */
  xDots: XY[];
}

/** Base dot pitch along the vertical axis — shrinks once the height cap engages. */
const Y_STEP = 28;
/** Fixed dot pitch along the horizontal axis (dots only — no labels to clear). */
const X_STEP = 34;
/** Axis continues this far past the last dot before the arrowhead. */
const AXIS_TAIL = 22;
/** Room above the vertical arrow tip (its axis label sits BESIDE the tip).
 *  Pads sized so the sketch breathes inside its now fit-content frame. */
const TOP_PAD = 24;
/** Room under the horizontal axis for the Axis-2 caption. */
const BOTTOM_PAD = 32;
/** Origin inset from the left edge. */
const LEFT_PAD = 28;
/** Small overshoot past the horizontal arrow tip. */
const RIGHT_PAD = 24;
/**
 * The diagram never grows taller than this (founder 2026-07-17: ~260–300px).
 * Once the vertical dot count would exceed the capped span, the vertical pitch
 * scales DOWN to fit instead of the diagram towering.
 */
export const AXES_HEIGHT_CAP = 280;
/**
 * The diagram never gets narrower than this: the axis labels (drawn beside the
 * vertical arrow tip and as the caption under the horizontal axis) need the room
 * even when the dot counts are tiny.
 */
export const AXES_MIN_WIDTH = 300;

/**
 * Compute the axes-overview geometry for `yCount` dots on the vertical axis and
 * `xCount` dots on the horizontal axis. Dots are evenly spaced from the origin
 * (dot k sits at (k+1)·pitch) so every dot lies exactly on its axis line. The
 * horizontal pitch is fixed (the container scrolls); the vertical pitch is fixed
 * until the AXES_HEIGHT_CAP engages, then scales down so the diagram stays a
 * compact quadrant sketch at any count. A dotless axis still draws one full
 * step so it reads as an axis, not a stub.
 */
export function axesLayout(yCount: number, xCount: number): AxesLayout {
  const ySpan = AXES_HEIGHT_CAP - TOP_PAD - BOTTOM_PAD - AXIS_TAIL;
  const yPitch = Math.min(Y_STEP, ySpan / Math.max(yCount, 1));
  const yLen = Math.max(yCount, 1) * yPitch + AXIS_TAIL;
  const xLen = Math.max(xCount, 1) * X_STEP + AXIS_TAIL;
  const origin: XY = { x: LEFT_PAD, y: TOP_PAD + yLen };
  return {
    width: Math.max(origin.x + xLen + RIGHT_PAD, AXES_MIN_WIDTH),
    height: origin.y + BOTTOM_PAD,
    origin,
    yArrowTip: { x: origin.x, y: origin.y - yLen },
    xArrowTip: { x: origin.x + xLen, y: origin.y },
    yDots: Array.from({ length: yCount }, (_, k) => ({
      x: origin.x,
      y: origin.y - (k + 1) * yPitch,
    })),
    xDots: Array.from({ length: xCount }, (_, k) => ({
      x: origin.x + (k + 1) * X_STEP,
      y: origin.y,
    })),
  };
}
