/**
 * Ch. 4 band context for the use-case corpus summary line: The Method targets
 * 2–6 CORE use cases (identify the essence through abstraction — more means the
 * abstraction failed, fewer means the system is under-specified). Pure logic so
 * the label + warning flag are unit-testable apart from the carousel chrome.
 */

/** The Method's lower bound on core use cases (Righting Software ch. 4). */
export const CORE_TARGET_MIN = 2;
/** The Method's upper bound on core use cases (Righting Software ch. 4). */
export const CORE_TARGET_MAX = 6;

export interface CoreBand {
  /** Compact, informational label ("target 2–6") appended to the summary line. */
  label: string;
  /** False when the core count falls outside 2–6 — the warning accent applies. */
  inBand: boolean;
}

/** Band context for a committed core-use-case count. */
export function coreBand(coreCount: number): CoreBand {
  return {
    label: `target ${String(CORE_TARGET_MIN)}–${String(CORE_TARGET_MAX)}`,
    inBand: coreCount >= CORE_TARGET_MIN && coreCount <= CORE_TARGET_MAX,
  };
}
