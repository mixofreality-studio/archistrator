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
// 2D coordinate (the scrapped scatter collapsed those onto a diagonal). All
// geometry is computed here so it is unit-testable without an SVG harness.

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

/** Fixed dot pitch along the vertical axis. */
const Y_STEP = 30;
/** Fixed dot pitch along the horizontal axis — wide enough that the rotated
 *  dot labels underneath never collide (pitch ≫ line-height / sin 35°). */
const X_STEP = 64;
/** Axis continues this far past the last dot before the arrowhead. */
const AXIS_TAIL = 26;
/** Room above the vertical arrow tip for its axis label. */
const TOP_PAD = 30;
/** Room under the horizontal axis for the rotated (−35°) truncated dot labels. */
const BOTTOM_PAD = 88;
/** Origin inset from the left edge. */
const LEFT_PAD = 26;
/** Room past the horizontal arrow tip for its axis label and the y-dot labels
 *  that extend rightward into the (empty) quadrant. */
const RIGHT_PAD = 190;

/**
 * Compute the axes-overview geometry for `yCount` dots on the vertical axis and
 * `xCount` dots on the horizontal axis. Dots are evenly spaced from the origin
 * at a fixed pitch (dot k sits at (k+1)·pitch), so the diagram grows with the
 * counts instead of crowding, and every dot lies exactly on its axis line. A
 * dotless axis still draws one full step so it reads as an axis, not a stub.
 */
export function axesLayout(yCount: number, xCount: number): AxesLayout {
  const yLen = Math.max(yCount, 1) * Y_STEP + AXIS_TAIL;
  const xLen = Math.max(xCount, 1) * X_STEP + AXIS_TAIL;
  const origin: XY = { x: LEFT_PAD, y: TOP_PAD + yLen };
  return {
    width: origin.x + xLen + RIGHT_PAD,
    height: origin.y + BOTTOM_PAD,
    origin,
    yArrowTip: { x: origin.x, y: origin.y - yLen },
    xArrowTip: { x: origin.x + xLen, y: origin.y },
    yDots: Array.from({ length: yCount }, (_, k) => ({
      x: origin.x,
      y: origin.y - (k + 1) * Y_STEP,
    })),
    xDots: Array.from({ length: xCount }, (_, k) => ({
      x: origin.x + (k + 1) * X_STEP,
      y: origin.y,
    })),
  };
}

/** Truncate a dot label so the fixed label margins always contain it. */
export function truncateLabel(name: string, max = 18): string {
  return name.length <= max ? name : `${name.slice(0, max - 1).trimEnd()}…`;
}
