/**
 * Which theme token carries each build status, and which carries selection
 * (designer palette ruling, fix I). No runtime imports, so node:test measures the
 * float ramp against these (bandRamp.test.ts: every band stays ≥15 CIEDE2000 from
 * every hued status colour and from the selection accent); status.tsx renders from
 * it.
 *
 * `blocked` is awaitingFg, not the accent: the accent means brand, selection,
 * focus and search match only. `failed` is dangerFg, which means failed/error only.
 */
import type { BuildStatus } from '../../contracts/constructionAdapters';
import type { Tokens } from '../../utilities/theme/themes';

export const STATUS_TOKEN = {
  integrated: 'committedDot',
  'in-review': 'chatPmFg',
  'in-construction': 'accent2',
  'in-detailed-design': 'chatArchitectFg',
  // teal/green: ready to start, distinct from in-construction
  eligible: 'chatPmFg',
  // blocked on a human: the awaiting tone, never the accent
  blocked: 'awaitingFg',
  'not-started': 'muted',
  // terminal failure: the danger tone
  failed: 'dangerFg',
  // same neutral tone as not-started; the label carries the distinction
  unclassified: 'muted',
} as const satisfies Record<BuildStatus, keyof Tokens>;

/** Selection, focus and search match: the accent, and nothing else is. */
export const SELECTION_TOKEN = 'accent' satisfies keyof Tokens;
