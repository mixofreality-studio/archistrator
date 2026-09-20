/**
 * The single review verb, resolved from what is staged and where the slot is in
 * its lifecycle. One function so the bar (`SubmitBar.tsx`), its tests, and any
 * future surface agree on what pressing it will do — the founder's complaint this
 * fixes is that "I want this changed" used to land in three different places
 * (the committed header's Amend button, the gate's Send back, and the rail's
 * Ask), depending on the artifact's lifecycle stage. This is the one function all
 * three verbs (plus Approve) now resolve through.
 *
 * Pure and React-free so `node --test` can reach it directly (this repo's runner
 * is `node --test` over `src/**\/*.test.ts`, which cannot import `.tsx` — see
 * `reviewBatch.ts` / `commentMarginLayout.ts` for the same posture).
 */
export type SubmitAction = 'sendBack' | 'approve' | 'amend' | 'ask' | 'none';

export interface SubmitVerb {
  action: SubmitAction;
  label: string;
  /** Always shown beneath the verb: what pressing it actually dispatches. */
  consequence: string;
  disabled: boolean;
}

export function resolveSubmitVerb(input: {
  committed: boolean;
  stage: 'drafted' | 'awaitingReview' | 'other';
  stagedChangeRequests: number;
  stagedQuestions: number;
  openThreads: number;
}): SubmitVerb {
  const { committed, stagedChangeRequests: crs, stagedQuestions: qs, openThreads } = input;
  const staged = crs + qs;
  const consequence = describeConsequence(crs, qs, committed);

  // Questions alone never redraft — that is the whole point of the ask path.
  if (staged > 0 && crs === 0) {
    return { action: 'ask', label: `Ask (${String(qs)}) — no redraft`, consequence, disabled: false };
  }
  if (staged > 0) {
    return committed
      ? { action: 'amend', label: `Amend (${String(staged)})`, consequence, disabled: false }
      : { action: 'sendBack', label: `Send back (${String(staged)})`, consequence, disabled: false };
  }
  if (committed) {
    return { action: 'none', label: '', consequence: '', disabled: true };
  }
  if (openThreads > 0) {
    return {
      action: 'approve',
      label: `Resolve ${String(openThreads)} thread${openThreads === 1 ? '' : 's'} to approve`,
      consequence: 'Open change requests block approval',
      disabled: true,
    };
  }
  return { action: 'approve', label: 'Approve', consequence: 'Commits the artifact and advances', disabled: false };
}

/**
 * The line always shown beneath the verb button (founder: "I want to know what
 * this button will actually do"). Note `amend` replaces `redraft` only once the
 * slot is COMMITTED — an uncommitted slot's staged notes always ride a fresh
 * redraft, never an amendment.
 */
function describeConsequence(crs: number, qs: number, committed: boolean): string {
  const parts: string[] = [];
  if (crs > 0) {
    parts.push(`${String(crs)} change request${crs === 1 ? '' : 's'} → ${committed ? 'amend' : 'redraft'}`);
  }
  if (qs > 0) parts.push(`${String(qs)} question${qs === 1 ? '' : 's'} → PM`);
  return parts.join(' · ');
}
