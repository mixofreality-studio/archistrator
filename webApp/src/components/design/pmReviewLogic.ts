/**
 * Pure presentation mapping for GatePanel's "PM REVIEW" section (F-QA2-7), split
 * out of GatePanel.tsx so it is unit-testable headlessly (see pmReviewLogic.test.ts)
 * — the same React-free pattern as sendBackLogic.ts / roleLine.ts (this repo's
 * `node --test` harness has no jsdom/RTL, so JSX components are not renderable).
 *
 * Given the surfaced PM-critique conclusion (verdict + rationale + judged round,
 * mapped from the wire in wire.ts), it returns the badge text/tone, the header
 * caption naming the critic and round, and the rationale body. No inference: the
 * copy only restates what the server reported, with one honest fallback — an
 * approve that carried no notes still gets a sentence, so the body never renders
 * blank under an APPROVED badge. The fallback deliberately does NOT claim "no
 * reservations": today's setCritiqueVerdict tool discards notes on approve (see
 * the F-QA2-7 gap report), so an empty summary only proves none were RECORDED.
 */
import type { PmCritiqueView } from '../../contracts/types';

export interface PmReviewPresentation {
  /**
   * Section heading naming the judging role honestly: "PM REVIEW" for the
   * productManager critic, "ARCHITECT SELF-REVIEW" for the architect
   * self-critiqued architecture (system-critique amendment 2026-07-17).
   */
  heading: string;
  /** Verdict badge text. */
  badge: 'APPROVED' | 'PUSHED BACK';
  /** true → approved (positive tone); false → pushed back (attention tone). */
  approved: boolean;
  /** Header caption naming the critic and, past round 0, the judged round. */
  caption: string;
  /** Rationale body (the critic's notes verbatim, or the clean-approve fallback). */
  summary: string;
}

/** Human label for a wire critic role; unknown roles pass through verbatim. */
function roleLabel(role: string): string {
  if (role === 'productManager') return 'Product manager';
  if (role === 'architect') return 'Architect (self-review)';
  return role;
}

export function pmReviewPresentation(c: PmCritiqueView): PmReviewPresentation {
  const approved = c.verdict === 'approve';
  const architect = c.role === 'architect';
  const label = roleLabel(c.role);
  return {
    heading: architect ? 'ARCHITECT SELF-REVIEW' : 'PM REVIEW',
    badge: approved ? 'APPROVED' : 'PUSHED BACK',
    approved,
    caption: c.round > 0 ? `${label} · judged draft round ${String(c.round)}` : label,
    summary:
      c.summary.length > 0
        ? c.summary
        : approved
          ? architect
            ? 'The architect self-reviewed this draft and approved it.'
            : 'The product manager reviewed this draft and approved it.'
          : '',
  };
}
