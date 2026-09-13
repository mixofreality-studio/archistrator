/**
 * The provenance axis (provenanceAxis.ts) — pure, so it is tested with no renderer
 * in the way. The three cases the Stage-B brief prescribes come first, verbatim
 * in intent; the rest pin the rules the brief states but does not exercise
 * (grade mapping, the group/row contagion walk, the basis pass-through, and the
 * one that matters most in production right now: the founder's ruling reaching
 * the tooltip).
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type { RecordOriginRow } from '../../contracts/types.ts';
import {
  provenanceBasesOf,
  provenanceGradeOf,
  provenanceRailFor,
  provenanceRailTooltipFor,
  provenanceSubGradeLabel,
  provenanceTooltipFor,
  reconstructedHintFor,
  worstOriginOf,
  type ProvenanceBearing,
} from './provenanceAxis.ts';

// ---------------------------------------------------------------------------
// Fixtures — shaped like the real tree: an activity holds phases, a phase holds
// tasks, a task holds attempts. `nodeWith` is the brief's own helper: the flat
// list of origins becomes one attempt each on one task under one phase, so the
// contagion walk is genuinely exercised rather than short-circuited.
// ---------------------------------------------------------------------------

function nodeWith(origins: readonly RecordOriginRow[], basis?: string): ProvenanceBearing {
  return {
    phases: [
      {
        tasks: origins.map((origin) => ({
          attempts: [{ provenance: { origin, ...(basis !== undefined ? { basis } : {}) } }],
        })),
      },
    ],
  };
}

const FOUNDER_BASIS =
  'serviceContracts[artifactAccess] + activityConstruction[C-artifact-access].produced[code]=implementation/log' +
  ' + founderRuling[2026-09-09]=assume any component that is fully implemented is done and' +
  ' reviewed and integrated';

// ---------------------------------------------------------------------------
// The brief's three cases
// ---------------------------------------------------------------------------

void test('takes the worst origin among descendants', () => {
  assert.equal(worstOriginOf(nodeWith(['observed', 'backfilled'])), 'backfilled');
  assert.equal(worstOriginOf(nodeWith(['observed', 'backfilled', 'synthesized'])), 'synthesized');
});

void test('reports unknown, not observed, for a node with no attempts at all', () => {
  // The whole point. The server's roll-up seeds an EMPTY ledger to `observed`,
  // so the wire drops `worstOrigin` there; if this ever returns `observed`, every
  // empty-ledger row starts claiming to be trustworthy about nothing.
  assert.equal(worstOriginOf(nodeWith([])), 'unknown');
  assert.equal(worstOriginOf({}), 'unknown');
  assert.equal(worstOriginOf({ phases: [{ tasks: [{ attempts: [] }] }] }), 'unknown');
});

void test('never expresses provenance through colour', () => {
  assert.notEqual(provenanceRailFor('backfilled').texture, undefined);
  assert.equal(provenanceRailFor('backfilled').color, undefined);
  // The whole axis, not just the one grade the brief names.
  for (const origin of ['observed', 'backfilled', 'synthesized', 'unknown'] as const) {
    assert.equal(provenanceRailFor(origin).color, undefined);
  }
});

// ---------------------------------------------------------------------------
// The grades
// ---------------------------------------------------------------------------

void test('backfilled and inferred share one grade; observed and unrecorded do not', () => {
  assert.equal(provenanceGradeOf('backfilled'), 'reconstructed');
  assert.equal(provenanceGradeOf('synthesized'), 'reconstructed');
  assert.equal(provenanceGradeOf('observed'), 'recorded');
  assert.equal(provenanceGradeOf('unknown'), 'unknown');
});

void test('recorded draws nothing at all — the default is the absence of a mark', () => {
  const rail = provenanceRailFor('observed');
  assert.equal(rail.texture, undefined);
  assert.equal(rail.outline, undefined);
  assert.equal(rail.widthPx, 0);
});

void test('unknown is a dashed outline and NO rail — never the reconstructed hatch', () => {
  const unknown = provenanceRailFor('unknown');
  assert.equal(unknown.outline, 'dashed');
  assert.equal(unknown.texture, undefined);
  // "no rail" literally: the column reserves its space and draws nothing, so
  // the hatch is the ONLY thing ever inked there. Every activity with no
  // ledger is unknown; a hairline on each is the ruled-paper texture
  // FloatRail already measured and removed from this surface.
  assert.equal(unknown.widthPx, 0);
  assert.notDeepEqual(unknown, provenanceRailFor('backfilled'));
});

void test('only the reconstructed grade draws a rail at all', () => {
  assert.ok(provenanceRailFor('backfilled').widthPx > 0);
  assert.ok(provenanceRailFor('synthesized').widthPx > 0);
  assert.equal(provenanceRailFor('observed').widthPx, 0);
  assert.equal(provenanceRailFor('unknown').widthPx, 0);
});

void test('the two reconstructed sub-grades are visually identical', () => {
  // Density rule 2: the hatch does not fork. Only the tooltip tells them apart.
  assert.deepEqual(provenanceRailFor('backfilled'), provenanceRailFor('synthesized'));
});

// ---------------------------------------------------------------------------
// Contagion
// ---------------------------------------------------------------------------

void test('a collapsed parent cannot hide a fake three tiers down', () => {
  const activity: ProvenanceBearing = {
    phases: [
      { tasks: [{ attempts: [{ provenance: { origin: 'observed' } }] }] },
      { tasks: [{ attempts: [] }] },
      { tasks: [{ attempts: [{ provenance: { origin: 'backfilled' } }] }] },
    ],
  };
  assert.equal(worstOriginOf(activity), 'backfilled');
});

void test('an un-attempted sibling does not drag a known node back to unknown', () => {
  // `unknown` is the empty answer, never a rank — otherwise every activity with
  // one un-run task would read `unknown` and bury the 21 reconstructed rows.
  assert.equal(
    worstOriginOf({ phases: [{ tasks: [{ attempts: [] }, { attempts: [] }] }] }),
    'unknown'
  );
  assert.equal(
    worstOriginOf({
      phases: [
        { tasks: [{ attempts: [] }, { attempts: [{ provenance: { origin: 'observed' } }] }] },
      ],
    }),
    'observed'
  );
});

void test("folds in the server's row roll-up, so an off-profile attempt cannot slip through", () => {
  // The tree only carries attempts whose task key is in the activity's profile.
  // A reconstructed attempt outside it exists on the row and in no descendant.
  assert.equal(worstOriginOf({ worstOrigin: 'backfilled', phases: [] }), 'backfilled');
  assert.equal(
    worstOriginOf({
      worstOrigin: 'backfilled',
      phases: [{ tasks: [{ attempts: [{ provenance: { origin: 'observed' } }] }] }],
    }),
    'backfilled'
  );
});

// ---------------------------------------------------------------------------
// The words
// ---------------------------------------------------------------------------

void test('the tooltip surfaces the founder ruling that stands in for the evidence', () => {
  const node = nodeWith(['backfilled', 'backfilled'], FOUNDER_BASIS);
  const bases = provenanceBasesOf(node);
  assert.deepEqual(bases, [FOUNDER_BASIS]); // distinct, not once per attempt
  const tip = provenanceTooltipFor(worstOriginOf(node), bases);
  assert.match(tip, /Reconstructed \(backfilled\)/);
  assert.match(tip, /founderRuling\[2026-09-09\]/);
});

// Final web review: the attempt ledger read a second label table ("reconstructed",
// "synthesized") while the tooltip on the same row said "backfilled" / "inferred".
// The ledger now reads provenanceSubGradeLabel — this pins its words to the tooltip's.
void test('the sub-grade words are the tooltip’s own: "backfilled" and "inferred"', () => {
  assert.equal(provenanceSubGradeLabel('backfilled'), 'backfilled');
  assert.equal(provenanceSubGradeLabel('synthesized'), 'inferred');
  for (const origin of ['backfilled', 'synthesized'] as const) {
    assert.ok(
      provenanceTooltipFor(origin, []).includes(`(${provenanceSubGradeLabel(origin)})`),
      `${origin}: the ledger word is not the tooltip's`
    );
  }
});

void test('an observed attempt contributes no basis to quote', () => {
  assert.deepEqual(provenanceBasesOf(nodeWith(['observed'], FOUNDER_BASIS)), []);
});

void test('the unknown tooltip refuses to say the work did not happen', () => {
  const tip = provenanceTooltipFor('unknown', []);
  assert.match(tip, /unknown, not observed/);
  assert.doesNotMatch(tip, /not started/i);
});

// Fix I: the rail's nested tooltip is ONE fixed hint, the ribbon's own line, and
// never a basis — however long the basis behind it.
void test("the rail's tooltip is the fixed hint, never a basis, for both reconstructed sub-grades", () => {
  const wall = `The founder's ruling of 2026-09-09 ${'widened the backfill '.repeat(80)}`;
  assert.equal(
    reconstructedHintFor('backfilled'),
    'Reconstructed (backfilled): written from a basis, not observed.'
  );
  for (const origin of ['backfilled', 'synthesized'] as const) {
    const tip = provenanceRailTooltipFor(origin);
    assert.equal(tip, `${reconstructedHintFor(origin)} Select it for its basis.`);
    assert.ok(tip.length < 120, `${origin}: ${String(tip.length)} characters`);
    assert.doesNotMatch(tip, /Basis:|founder/);
    // The full tooltip, by contrast, reads the basis out — which is why the rail
    // must not use it.
    assert.match(provenanceTooltipFor(origin, [wall]), /Basis:/);
  }
  assert.match(provenanceRailTooltipFor('synthesized'), /\(inferred\)/);
});
