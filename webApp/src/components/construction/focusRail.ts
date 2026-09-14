/**
 * THE FOCUS VIEW'S SIDE PANEL, AND THE WINDOW THE CODE DIAGRAM NEEDS — pure
 * (designer check on renderers S3).
 *
 * The focus view is a 320px rail (the header, what judges the artifact, the
 * action bar) beside the artifact column, which keeps 24px of padding either
 * side. The code canvas draws only where that column reaches
 * CODE_CANVAS_MIN_WIDTH (contractCode.ts, 1344px), so with the rail open it
 * needs a window of about 1712px: a 14" MacBook at 1512 never saw the diagram.
 * The rail COLLAPSES, and then the column is the window less its padding — the
 * diagram draws from a window of about 1392px.
 *
 * The collapse is a per-viewer convenience, remembered in this browser only
 * (localStorage, every access guarded: it can throw or come back empty).
 */
import { CODE_CANVAS_MIN_WIDTH } from './contractCode.ts';

/** The rail's grid track. Its 1.5px border sits inside it. */
export const FOCUS_RAIL_WIDTH = 320;

/** The artifact column's horizontal padding, both sides (px: 3). */
export const FOCUS_COLUMN_PADDING = 48;

/** The window at which the focus view's artifact column reaches `columnWidth`. */
export function focusWindowFor(columnWidth: number, railOpen: boolean): number {
  return columnWidth + FOCUS_COLUMN_PADDING + (railOpen ? FOCUS_RAIL_WIDTH : 0);
}

/** The window the code diagram needs: 1712 with the rail open, 1392 collapsed. */
export function codeCanvasWindowFor(railOpen: boolean): number {
  return focusWindowFor(CODE_CANVAS_MIN_WIDTH, railOpen);
}

/**
 * Why the focus view lists instead of drawing, as something the reader can act
 * on: the window it needs, and — where the rail is open and can collapse — the
 * other way to get there. `action` is the one the note offers as a button.
 */
export interface NeedsRoomCopy {
  lead: string;
  action: 'collapse' | undefined;
  /** The whole sentence, as read aloud. */
  text: string;
}

export function needsRoomCopy(args: {
  /** The rail is beside the artifact (not collapsed, not stacked above it). */
  railOpen: boolean;
  /** The rail can collapse here (a window of 600px or more; below it, it stacks). */
  collapsible: boolean;
}): NeedsRoomCopy {
  if (args.railOpen && args.collapsible) {
    const lead = `Needs a window about ${String(codeCanvasWindowFor(true))} px wide`;
    return { lead, action: 'collapse', text: `${lead} — or collapse the side panel` };
  }
  const lead = `Needs a window about ${String(codeCanvasWindowFor(false))} px wide`;
  // Stacked below 600px, the rail is above the artifact, not beside it: the
  // diagram still needs the collapsed panel of a wide window.
  const text = args.collapsible ? lead : `${lead}, with the side panel collapsed`;
  return { lead: text, action: undefined, text };
}

/** The per-viewer key. Versioned so a later meaning never reads an old value. */
export const FOCUS_RAIL_STORAGE_KEY = 'archistrator.construction.focusRail.collapsed.v1';

/** Minimal storage surface, so the tests pass a fake (or one that throws). */
export interface RailStorage {
  getItem: (key: string) => string | null;
  setItem: (key: string, value: string) => void;
}

function defaultStorage(): RailStorage | undefined {
  try {
    return typeof window !== 'undefined' ? window.localStorage : undefined;
  } catch {
    return undefined;
  }
}

/** Whether this viewer last left the rail collapsed. Anything unreadable: open. */
export function readRailCollapsed(storage: RailStorage | undefined = defaultStorage()): boolean {
  try {
    return storage?.getItem(FOCUS_RAIL_STORAGE_KEY) === '1';
  } catch {
    return false;
  }
}

/** Remember the choice; a storage that refuses it is ignored (the page still works). */
export function writeRailCollapsed(
  collapsed: boolean,
  storage: RailStorage | undefined = defaultStorage()
): void {
  try {
    storage?.setItem(FOCUS_RAIL_STORAGE_KEY, collapsed ? '1' : '0');
  } catch {
    // Private windows and blocked site data throw; the collapse still applies now.
  }
}
