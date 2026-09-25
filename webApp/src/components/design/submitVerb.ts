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
  /**
   * A second line beneath the consequence, for what the verb will NOT do. Empty
   * whenever there is nothing to warn about — today it is written by exactly one
   * situation: questions staged on a rail with no question op (`allowAsk: false`),
   * which no batch verb carries, because `toWireEntries` and `freeformNotesFrom`
   * both exclude questions. Kept OUT of `consequence` so an `approveCopy` that
   * replaces the consequence cannot swallow the warning with it.
   */
  notice: string;
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
  /**
   * False on a surface where sending back is not a verb at all (spec R7: the
   * Project Design M0 gate — to change the plan you amend the Architecture).
   * Staged change requests then STAY comments: they ride the approval as
   * recorded feedback rather than flipping the primary verb to a redraft that
   * has nowhere to go. Default true — every existing caller is unchanged.
   */
  allowSendBack?: boolean;
  /**
   * False on a rail with NO question op — every construction activity type, where
   * `constructionManager` has neither `AskQuestions` nor `SetReviewCommentStatus`
   * (R2/GAP-6). The ask branch is then skipped entirely and the verb resolves as
   * if no question had been staged, so one staged question can never leave the
   * reviewer with a dead Ask button and no Approve or Send back at all. What the
   * questions will NOT do is said in {@link SubmitVerb.notice} rather than
   * silently dropped. Default true — every existing caller is unchanged.
   */
  allowAsk?: boolean;
  /** Overrides the approve verb's wording where the consequence is bigger than "commits and advances". */
  approveCopy?: { label: string; consequence: string } | undefined;
}): SubmitVerb {
  const {
    committed,
    stage,
    stagedChangeRequests: crs,
    stagedQuestions: rawQuestions,
    openThreads,
    allowEmptySendBack = false,
    allowSendBack = true,
    allowAsk = true,
    approveCopy,
  } = input;
  // A question staged on a rail that cannot send one is not part of this batch:
  // it counts towards nothing, colours no consequence, and is reported once in
  // `notice`. Counting it would put a number on the button that the button does
  // not send.
  const qs = allowAsk ? rawQuestions : 0;
  const notice = allowAsk ? '' : questionsNotSent(rawQuestions);
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
  // Unreachable when `allowAsk` is false: `qs` is 0 there, so a questions-only
  // batch falls through to Approve (or Send back, if change requests are staged
  // too) instead of offering an Ask this rail cannot dispatch.
  if (staged > 0 && crs === 0) {
    return {
      action: 'ask',
      label: `Ask (${String(qs)}) — no redraft`,
      consequence,
      notice,
      disabled: false,
      secondaryActions: [],
    };
  }
  // RULING R11 (spec §6/R7): on a surface with no send-back — today only the
  // Project Design M0 gate — staged change requests are not a redraft trigger,
  // because there is nothing to redraft: the plan is computed, and changing it
  // means amending the Architecture. They ride the approval as recorded
  // feedback and the primary verb stays Approve.
  if (staged > 0 && allowSendBack) {
    return committed
      ? {
          action: 'amend',
          label: `Amend (${String(staged)})`,
          consequence,
          notice,
          disabled: false,
          secondaryActions: [],
        }
      : {
          action: 'sendBack',
          label: `Send back (${String(staged)})`,
          consequence,
          notice,
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
  const secondaryActions: SubmitAction[] =
    allowEmptySendBack && liveDraft && allowSendBack ? ['sendBack'] : [];
  // No live draft AND the slot is committed: a truly clean, sealed artifact —
  // nothing to approve, nothing to send back. An amendment under active review
  // (`liveDraft` above) is NOT this case even though it too is `committed`.
  if (!liveDraft && committed) {
    return {
      action: 'none',
      label: '',
      consequence: '',
      notice,
      disabled: true,
      secondaryActions,
    };
  }
  if (openThreads > 0) {
    return {
      action: 'approve',
      label: `Resolve ${String(openThreads)} thread${openThreads === 1 ? '' : 's'} to approve`,
      consequence: 'Open change requests block approval',
      notice,
      disabled: true,
      secondaryActions,
    };
  }
  return {
    action: 'approve',
    label: approveCopy?.label ?? 'Approve',
    consequence: approveCopy?.consequence ?? 'Commits the artifact and advances',
    notice,
    disabled: false,
    secondaryActions,
  };
}

/**
 * The second line on a rail with no question op: what the staged questions will
 * NOT do. It lives here, beside {@link describeConsequence}, because it is the
 * same kind of sentence — the bar's own account of what pressing the verb sends —
 * and because `submitVerb.ts` is shared with the MCP widget, which must not reach
 * into the Activity Experience's copy module for it.
 *
 * Empty for zero questions: there is nothing to warn about, and "0 questions will
 * not be sent" is noise on every gate in the product.
 */
export function questionsNotSent(questions: number): string {
  if (questions <= 0) return '';
  return `${String(questions)} staged question${questions === 1 ? '' : 's'} will NOT be sent: this review has no question op yet. Discard ${questions === 1 ? 'it' : 'them'}, or restage ${questions === 1 ? 'it' : 'them'} as a change request.`;
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
