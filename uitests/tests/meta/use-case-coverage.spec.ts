/**
 * tests/meta/use-case-coverage.spec.ts — the "does the UI test suite actually
 * cover the product's core use cases" meta-check (Method Phase-1 "2-6 core use
 * cases" — see the-method-core-use-cases; committed under `.coreUseCases` in
 * project.json).
 *
 * This is a META-check, not a UI test: it drives no browser page. It:
 *
 *   (a) fetches the CORE-classified use case ids over the wire — the SAME
 *       GetProject("archistrator") read gating.ts's constructionArtifactsAvailable
 *       uses, via the new fetchCoreUseCases — and self-skips (skipUnlessServer
 *       pattern) when the server behind the SPA proxy is unreachable;
 *   (b) STATICALLY scans tests/*.spec.ts SOURCE (readdir + regex — it does NOT
 *       run those specs) for literal `tagUseCase('<id>')` call sites;
 *   (c) asserts every core use case id has >=1 tagged spec, failing with a
 *       NAMED gap (use case id + name) rather than a generic "coverage < 100%".
 *
 * A core use case with genuinely NO reachable UI flow today is exempted via
 * KNOWN_GAPS below — each entry carries a `reason` that shows the gap was
 * investigated (which screen/component was checked, and why it doesn't
 * reach), not just asserted away. Do NOT add an entry to dodge a real,
 * fixable gap — only when the flow is provably unreachable in the UI today.
 */
import { test, expect } from '@playwright/test';
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { fetchCoreUseCases, skipUnlessServer } from '../support/gating.js';

const BASE = process.env.UITESTS_BASE_URL ?? process.env.UITESTS_SPA_URL ?? 'http://localhost:5173';

// tests/meta/ -> tests/ (the top-level spec directory; deliberately NOT
// recursive, so tests/support/*.ts and this file's own tests/meta/*.ts are
// excluded — mirrors a `tests/*.spec.ts` glob).
const SPECS_DIR = join(import.meta.dirname, '..');

const TAG_RE = /tagUseCase\(\s*['"]([a-z0-9-]+)['"]\s*\)/g;

/**
 * Core use cases with NO reachable UI flow today, and why — verified against
 * the webApp screen/route source, not assumed. Remove an entry the moment a
 * real spec tags that id.
 */
const KNOWN_GAPS: Record<string, string> = {
  'commit-to-a-project-option':
    "The SDP decision UI (SdpReviewView — option cards + commit/reject-all gate, " +
    'UI_IDENTIFIERS.SdpReview.*) only renders while a Phase-2 spine is LIVE and ' +
    "awaiting the SDP decision. ProjectDesignExperience.tsx short-circuits " +
    '`isSdpStep && committed` straight to the read-only AdvancePanel (not the ' +
    'decision UI) the instant sdpReview is committed. Reaching that live decision ' +
    'window needs driving the FULL Phase-2 co-author spine (8 sequential artifact ' +
    'drafts: planningAssumptions…riskModel) with no seed/import API — this harness ' +
    'is deliberately un-cheatable (see README) and drives no state-injection route. ' +
    'The one seeded project with committed Phase-2 state ("archistrator") has ' +
    'already advanced past this gate, so no committed OR live state reachable today ' +
    'renders the decision UI. Revisit once a live Phase-2-through-SDP flow is worth ' +
    'the cost (the Phase-1 equivalent already runs ~12,000,000ms in ' +
    'architecture-views.spec — Phase 2 would be strictly more expensive).',
};

test.beforeEach(async ({ request }) => {
  await skipUnlessServer(request, BASE);
});

test('every core use case has at least one tagged UI spec (or a documented gap)', async ({
  request,
}) => {
  const coreUseCases = await fetchCoreUseCases(request, BASE);
  test.skip(
    coreUseCases === undefined,
    'uitests: no committed coreUseCases slot on the "archistrator" project behind the SPA ' +
      'proxy — this meta-check asserts REAL coverage against the committed core use case ' +
      'list and cannot run against a fresh/empty project-state repo.',
  );
  const ids = coreUseCases ?? [];
  expect(
    ids.length,
    'expected the committed coreUseCases slot to carry >=1 core-classified use case',
  ).toBeGreaterThan(0);

  const specFiles = readdirSync(SPECS_DIR).filter((f) => f.endsWith('.spec.ts'));
  const taggedBy = new Map<string, string[]>(); // use-case id -> spec file names that tag it
  for (const file of specFiles) {
    const source = readFileSync(join(SPECS_DIR, file), 'utf8');
    for (const match of source.matchAll(TAG_RE)) {
      const id = match[1];
      if (id === undefined) continue;
      const specs = taggedBy.get(id) ?? [];
      specs.push(file);
      taggedBy.set(id, specs);
    }
  }

  const undocumentedGaps: string[] = [];
  const documentedGaps: string[] = [];
  let coveredCount = 0;
  for (const uc of ids) {
    const taggers = taggedBy.get(uc.id) ?? [];
    if (taggers.length > 0) {
      coveredCount += 1;
      continue;
    }
    const reason = KNOWN_GAPS[uc.id];
    if (reason !== undefined) {
      documentedGaps.push(`${uc.id} ("${uc.name}") — DOCUMENTED GAP: ${reason}`);
      continue;
    }
    undocumentedGaps.push(
      `${uc.id} ("${uc.name}") — no tagUseCase('${uc.id}') found in any tests/*.spec.ts`,
    );
  }

  test.info().annotations.push({
    type: 'use-case-coverage',
    description:
      `${String(coveredCount)}/${String(ids.length)} core use cases have a tagged UI spec; ` +
      `${String(documentedGaps.length)} documented gap(s); ` +
      `${String(undocumentedGaps.length)} UNDOCUMENTED gap(s).`,
  });
  for (const gap of documentedGaps) {
    test.info().annotations.push({ type: 'use-case-coverage-gap', description: gap });
  }

  expect(
    undocumentedGaps,
    `${String(undocumentedGaps.length)} core use case(s) have NO tagged UI spec and no ` +
      `documented gap in KNOWN_GAPS:\n${undocumentedGaps.map((g) => `  - ${g}`).join('\n')}`,
  ).toEqual([]);
});
