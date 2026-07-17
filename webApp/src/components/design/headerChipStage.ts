/**
 * Maps the artifact header's StageChip display stage from the committed flag +
 * the live co-author session stage. Extracted from SystemDesignView's inline
 * ternary so the chip-honesty rule is unit-testable (headerChipStage.test.ts):
 * while a generation is in flight (stage drafting/redrafting — e.g. right after
 * a founder "Send back") the chip must read DRAFTING…/REDRAFTING…, never the
 * dishonest "NOT DRAFTED". Precedence is unchanged from the original mapping:
 * committed → awaitingReview → in-flight generation → empty.
 */
import type { SessionStage } from '../../contracts/types';
import type { StageChipStage } from '../StageChip';

export function headerChipStage(
  committed: boolean,
  stage: SessionStage | undefined
): StageChipStage {
  if (committed) return 'committed';
  if (stage === 'awaitingReview') return 'awaitingReview';
  if (stage === 'drafting' || stage === 'redrafting') return stage;
  return 'empty';
}
