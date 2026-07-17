/// <reference types="node" />
/**
 * Unit tests for the gate-decision fault message (gateFaultLogic.ts) — F-QA2-47.
 * A definite HTTP refusal keeps the server's precise message; every indeterminate
 * fault (5xx / network) gets the cause-neutral "could not be confirmed" copy,
 * because the decision's Temporal signal may have been delivered even though the
 * response was lost (the QA2 send-back incident).
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { ApiError } from '../../contracts/errors.ts';
import { DECISION_UNCONFIRMED_MESSAGE, gateDecisionErrorMessage } from './gateFaultLogic.ts';

void test('a definite HTTP refusal (< 500) keeps the server message', () => {
  const race = new ApiError(
    409,
    'failed_precondition',
    '2 open review comments must be addressed or waived before approve'
  );
  assert.equal(gateDecisionErrorMessage(race), race.message);
  const gone = new ApiError(404, 'not_found', 'no active design session');
  assert.equal(gateDecisionErrorMessage(gone), gone.message);
});

void test('F-QA2-47: an indeterminate 5xx gets the cause-neutral copy', () => {
  for (const status of [500, 502, 503]) {
    assert.equal(
      gateDecisionErrorMessage(new ApiError(status, 'unavailable', 'design session unavailable')),
      DECISION_UNCONFIRMED_MESSAGE,
      String(status)
    );
  }
});

void test('non-HTTP faults (network, unknown) get the cause-neutral copy', () => {
  assert.equal(
    gateDecisionErrorMessage(new TypeError('Failed to fetch')),
    DECISION_UNCONFIRMED_MESSAGE
  );
  assert.equal(gateDecisionErrorMessage(new Error('socket hang up')), DECISION_UNCONFIRMED_MESSAGE);
  assert.equal(gateDecisionErrorMessage(undefined), DECISION_UNCONFIRMED_MESSAGE);
});
