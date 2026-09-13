/**
 * Begin re-checks the control at CONFIRM time (final review minor), and the live
 * sessions THE in-flight set counts (final review I1).
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { ConstructionStage } from '../../../contracts/types';
import { beginConfirmAllowed, beginControlFor, liveSessionIdsOf } from './beginControl.ts';

void test('a confirm dispatches only while the control is still enabled and nothing is pending', () => {
  const enabled = beginControlFor({
    constructionStarted: false,
    projectLoading: false,
    running: false,
  });
  assert.equal(beginConfirmAllowed(enabled, false), true);
  // The state flipped while the dialog was open: work showed up in flight.
  const running = beginControlFor({
    constructionStarted: false,
    projectLoading: false,
    running: true,
  });
  assert.equal(beginConfirmAllowed(running, false), false);
  // ...or the project began loading, or a hold started.
  const checking = beginControlFor({
    constructionStarted: false,
    projectLoading: true,
    running: false,
  });
  assert.equal(beginConfirmAllowed(checking, false), false);
  // A dispatch already pending never sends a second.
  assert.equal(beginConfirmAllowed(enabled, true), false);
});

void test('the live sessions: a pump stage counts, no session and no answer do not', () => {
  const s = (stage: ConstructionStage): { stage: ConstructionStage } => ({ stage });
  assert.deepEqual(
    liveSessionIdsOf({
      b: s('pipelineRunning'),
      a: s('awaitingApproval'),
      exited: s('exited'),
      paused: s('paused'),
      unknown: s('unknown'),
      none: null,
      unanswered: undefined,
    }),
    ['a', 'b']
  );
});
