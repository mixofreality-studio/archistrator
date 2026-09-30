/// <reference types="node" />
/**
 * THE STALE-BASIS ACK'S REFUSAL IS WIRED TO ALL THREE OF ITS FACTS (stage 4b2 final fix
 * wave, F2 — the twin of `containers/McpSystemDesignContainer.test.ts`).
 *
 * `designAmendmentInFlight` itself is well held: `liveDesignGate.test.ts` fails on each of
 * its three disjuncts. Its ARGUMENTS were held by nothing. Passing `awaitingHuman: false`
 * at the one call site — pinning the refusal to `dispatchRunning`/`draftFailed` alone and
 * re-enabling the ack at exactly the state Task 9 restored it for, a draft sitting at its
 * gate — left `npm run check` at 1234/1234 with typecheck, lint and format clean. The
 * compiler cannot object: all three members are `boolean`/`string | undefined`, so a
 * literal, a rename or a copy-paste slip type-checks.
 *
 * That is verbatim the hole Task 9's review found for the MCP gate's `reviewTaskId`, which
 * Task 10 armed with a source scan — for ONE of the two call sites. This is the other, and
 * this screen has NO preview surface for the state in question: the ack's disabled arm is
 * reachable only with a live amendment, which no preview fixture stages.
 *
 * WHY THIS READS THE SOURCE: `npm test` is `node --test 'src/**\/*.test.ts'` over a harness
 * with no DOM and no JSX loader, so a `.test.tsx` would never even be globbed and this
 * component cannot be mounted at all. The house idiom for a claim about wiring is the
 * source scan — `containers/ActivityExperienceContainer.test.ts`,
 * `containers/McpSystemDesignContainer.test.ts`, `hooks/statusDecides.test.ts`.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const view = readFileSync(new URL('./SystemDesignView.tsx', import.meta.url), 'utf8');

/**
 * The facts handed to the ONE `designAmendmentInFlight({ … })` call, each normalised to
 * its key when the value is that same identifier. A member whose value is anything else —
 * a literal, or another fact's name — comes back as `key=value`, so it fails the
 * comparison below with the substitution readable in the diff rather than as a bare
 * "expected 3, got 3".
 */
function amendmentInFlightFacts(source: string): string[] {
  const calls = [...source.matchAll(/designAmendmentInFlight\(\s*\{([^}]*)\}\s*\)/g)];
  assert.equal(
    calls.length,
    1,
    'the predicate has exactly ONE call site in this screen; a second one is a second ' +
      'opinion about whether an amendment is in flight'
  );
  return (calls[0]?.[1] ?? '')
    .split(',')
    .map((entry) => entry.trim())
    .filter((entry) => entry.length > 0)
    .map((entry) => {
      const [key, value] = entry.split(':').map((part) => part.trim());
      const name = key ?? '';
      if (value === undefined) return name;
      return value === name ? name : `${name}=${value}`;
    });
}

void test('the stale-basis ack’s refusal is handed all THREE live facts, each as itself', () => {
  assert.deepEqual(amendmentInFlightFacts(view).sort(), [
    'awaitingHuman',
    'dispatchRunning',
    'stage',
  ]);
});

void test('the three facts come from three different authorities, so one cannot stand in for another', () => {
  // `awaitingHuman` and `dispatchRunning` are PROPS off the live read (`liveDesignGate`
  // over QueryActivityView); `stage` is the DERIVED session door. Reading any of the
  // first two off `stage` is the defect Task 9 existed to fix — the door's vocabulary is
  // {unknown, committed, withdrawn, draftFailed} and cannot answer them.
  assert.match(view, /^\s*awaitingHuman,$/m);
  assert.match(view, /^\s*dispatchRunning = false,$/m);
  assert.match(view, /^\s*const stage = session\?\.stage;$/m);
});

void test('the predicate’s answer is what disables the ack, and it reaches the chip', () => {
  // A `sessionLive` nothing consumes would leave the ack enabled over an amendment in
  // flight — the merge conflict the sentence itself warns about (F-GTD-12).
  assert.match(view, /const sessionLive = designAmendmentInFlight\(/);
  assert.match(view, /const ackDisabledReason = sessionLive\s*\n?\s*\?/);
  assert.match(view, /ackDisabledReason=\{ackDisabledReason\}/);
});
