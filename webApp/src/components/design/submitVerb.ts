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
  /**
   * Verbs offered in the overflow menu ALONGSIDE Withdraw/Retry, never as the
   * primary button — today only ever `['sendBack']`, and only when
   * `allowEmptySendBack` keeps Send back reachable despite nothing being
   * staged (RULING P18: this must never demote `action` away from `approve`).
   * Gated on a LIVE DRAFT under review, not on `committed` (RULING P20) — an
   * amendment under active review needs this exactly as much as an original
   * draft does. Empty whenever nothing extra applies, including on a
   * genuinely inert, sealed (`stage: 'other'`) artifact.
   */
  secondaryActions: SubmitAction[];
}

export function resolveSubmitVerb(input: {
  committed: boolean;
  stage: 'drafted' | 'awaitingReview' | 'other';
  stagedChangeRequests: number;
  stagedQuestions: number;
  openThreads: number;
  /**
   * MCP has no client-side comment accumulator (`stagedChangeRequests` is
   * always 0 there) — its own composer collects reject feedback AFTER the
   * click, not before. SPA default (false): behavior is untouched. True
   * (MCP) keeps Send back reachable as a SECONDARY action (see
   * `secondaryActions`) whenever nothing is staged and a draft is LIVE under
   * review — an original draft OR an amendment (RULING P20) — it must NEVER
   * replace `approve` as the primary verb (RULING P18): a reviewer with
   * nothing staged and nothing blocking always sees Approve, on every surface.
   */
  allowEmptySendBack?: boolean;
}): SubmitVerb {
  const {
    committed,
    stage,
    stagedChangeRequests: crs,
    stagedQuestions: qs,
    openThreads,
    allowEmptySendBack = false,
  } = input;
  const staged = crs + qs;
  const consequence = describeConsequence(crs, qs, committed);
  // RULING P19: STAGE decides whether a live draft is under review, NOT
  // `committed` — `committed` only ever colours the WORDING (Send back vs.
  // Amend, redraft vs. amend). A committed slot's AMENDMENT under review
  // (stage: 'awaitingReview') is exactly `committed: true` — treating
  // `committed` as an unconditional override ahead of `stage` (the original
  // defect) made an amendment permanently unapprovable: a reviewer opening
  // that screen before typing new feedback saw NO primary verb at all.
  const liveDraft = stage === 'drafted' || stage === 'awaitingReview';

  // Questions alone never redraft — that is the whole point of the ask path.
  if (staged > 0 && crs === 0) {
    return {
      action: 'ask',
      label: `Ask (${String(qs)}) — no redraft`,
      consequence,
      disabled: false,
      secondaryActions: [],
    };
  }
  if (staged > 0) {
    return committed
      ? {
          action: 'amend',
          label: `Amend (${String(staged)})`,
          consequence,
          disabled: false,
          secondaryActions: [],
        }
      : {
          action: 'sendBack',
          label: `Send back (${String(staged)})`,
          consequence,
          disabled: false,
          secondaryActions: [],
        };
  }
  // Nothing staged. `allowEmptySendBack` only ever ADDS a secondary — Approve
  // (enabled or its disabled/open-threads variant) stays primary either way.
  // RULING P20: gated on `liveDraft`, NOT `!committed` — a committed slot's
  // AMENDMENT under active review (`committed: true, stage: 'awaitingReview'`)
  // is still a live draft an MCP host needs to be able to send back, exactly
  // like an uncommitted one. Only a genuinely inert, sealed artifact
  // (`!liveDraft`, i.e. `stage: 'other'`) offers no secondary at all.
  const secondaryActions: SubmitAction[] = allowEmptySendBack && liveDraft ? ['sendBack'] : [];
  // No live draft AND the slot is committed: a truly clean, sealed artifact —
  // nothing to approve, nothing to send back. An amendment under active review
  // (`liveDraft` above) is NOT this case even though it too is `committed`.
  if (!liveDraft && committed) {
    return { action: 'none', label: '', consequence: '', disabled: true, secondaryActions };
  }
  if (openThreads > 0) {
    return {
      action: 'approve',
      label: `Resolve ${String(openThreads)} thread${openThreads === 1 ? '' : 's'} to approve`,
      consequence: 'Open change requests block approval',
      disabled: true,
      secondaryActions,
    };
  }
  return {
    action: 'approve',
    label: 'Approve',
    consequence: 'Commits the artifact and advances',
    disabled: false,
    secondaryActions,
  };
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
    parts.push(
      `${String(crs)} change request${crs === 1 ? '' : 's'} → ${committed ? 'amend' : 'redraft'}`
    );
  }
  if (qs > 0) parts.push(`${String(qs)} question${qs === 1 ? '' : 's'} → PM`);
  return parts.join(' · ');
}
