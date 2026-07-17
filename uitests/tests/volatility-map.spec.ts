/**
 * volatility-map.spec — the volatilities artifact's VolatilityMap rendered for a
 * COMMITTED artifact carrying the newer model fields (rejected candidates +
 * per-item requirement traces).
 *
 * Behaviors under test (the 2026-07 axes-overview + rejected-candidates pass):
 *
 *   1. The compact axes overview renders (aria-hidden SVG) with one dot per
 *      volatility, beside the lane listboxes — never a fabricated 2D scatter.
 *   2. Each lane is a single-select WAI-ARIA listbox: role=listbox with
 *      role=option chips carrying aria-selected.
 *   3. Keyboard: ArrowDown moves the roving focus within a lane WITHOUT
 *      selecting; Enter selects, opening the side-rail DETAIL card (name +
 *      "Traces:" caption) and announcing "Selected: …" via the polite live
 *      region; Escape clears back to the SUMMARY card.
 *   4. The rejected-candidates disclosure (aria-expanded button) expands to the
 *      classified rows — name + human classification chip per rejected item.
 *
 * Route-intercepted (see support/designStubs.stubCommittedVolatilities): the
 * spec stubs the wire and drives the REAL SPA, so it runs hermetically WITHOUT
 * a live drafting stack — the design-experience-regressions tactic. No infra
 * gating needed: only the SPA process is required.
 */
import { test, expect, type Page } from '@playwright/test';
import { TESTID } from './support/testids.js';
import {
  stubCommittedVolatilities,
  type StubVolatility,
  type StubRejectedVolatility,
} from './support/designStubs.js';

/**
 * Three accepted volatilities — the flat-array INDEX is the chip()/dot() test-id
 * key and the $.items[n] comment-anchor identity. Indexes 0 and 1 sit on axis 1
 * (the left lane, in that order), index 2 on axis 2, so the keyboard walk below
 * (focus chip 0 → ArrowDown → chip 1 → Enter) selects 'Identity Provider'.
 */
const ITEMS: StubVolatility[] = [
  {
    name: 'Notification Channels',
    rationale: 'Delivery channels keep changing for one customer over time.',
    axis: 'sameCustomerOverTime',
    traces: ['SR-3'],
  },
  {
    name: 'Identity Provider',
    rationale: 'The IdP rotates as one customer’s org evolves.',
    axis: 'sameCustomerOverTime',
    traces: ['SR-1', 'SR-4'],
  },
  {
    name: 'Venue Data Source',
    rationale: 'Different customers integrate different venue feeds today.',
    axis: 'allCustomersAtOneTime',
    traces: ['SR-2'],
  },
];

/** Two rejected candidates with distinct classifications (the class chips). */
const REJECTED: StubRejectedVolatility[] = [
  {
    name: 'Database Vendor',
    reason: 'A configuration variable, not an area of volatility.',
    class: 'variableNotVolatile',
  },
  {
    name: 'Quantum Notification Fabric',
    reason: 'No customer signal — speculative future-proofing.',
    class: 'speculative',
  },
];

/** Stub the committed volatilities, open the design experience, select the step. */
async function openCommittedVolatilities(page: Page): Promise<void> {
  const projectId = await stubCommittedVolatilities(page, ITEMS, REJECTED);
  await page.goto(`/project/${projectId}/design/system`);
  await expect(page.getByTestId(TESTID.designExperience)).toBeVisible();

  // The committed volatilities slot is not the first-open step — select it.
  await page.getByTestId(TESTID.spineStep('volatilities')).click();
  await expect(page.getByTestId(TESTID.volatilityMap)).toBeVisible();
}

test.describe('volatility map (stubbed committed artifact — hermetic)', () => {
  test('the axes overview renders a dot per volatility beside listbox lanes', async ({ page }) => {
    await openCommittedVolatilities(page);

    // Axes overview: the sketch SVG plus one dot per flat-index point.
    await expect(page.getByTestId(TESTID.volatilityAxes)).toBeVisible();
    for (const i of [0, 1, 2]) {
      await expect(page.getByTestId(TESTID.volatilityDot(i))).toBeVisible();
    }

    // Each lane is a single-select listbox of role=option chips, none selected.
    const lane1 = page.getByTestId(TESTID.volatilityLane('sameCustomerOverTime'));
    const lane2 = page.getByTestId(TESTID.volatilityLane('allCustomersAtOneTime'));
    await expect(lane1).toHaveRole('listbox');
    await expect(lane2).toHaveRole('listbox');
    for (const i of [0, 1, 2]) {
      const chip = page.getByTestId(TESTID.volatilityChip(i));
      await expect(chip).toHaveRole('option');
      await expect(chip).toHaveAttribute('aria-selected', 'false');
    }

    // Nothing selected yet — the rail shows the summary card.
    await expect(page.getByTestId(TESTID.volatilitySummary)).toBeVisible();
    await expect(page.getByTestId(TESTID.volatilityDetail)).toHaveCount(0);
  });

  test('ArrowDown roves focus, Enter selects (detail + traces + announcement), Escape clears', async ({
    page,
  }) => {
    await openCommittedVolatilities(page);

    // Focus the axis-1 lane's first chip (its roving tab stop), then ArrowDown:
    // focus moves to the second chip WITHOUT selecting it (selection never
    // follows focus in this listbox).
    await page.getByTestId(TESTID.volatilityChip(0)).focus();
    await expect(page.getByTestId(TESTID.volatilityChip(0))).toBeFocused();
    await page.keyboard.press('ArrowDown');
    const chip1 = page.getByTestId(TESTID.volatilityChip(1));
    await expect(chip1).toBeFocused();
    await expect(chip1).toHaveAttribute('aria-selected', 'false');
    await expect(page.getByTestId(TESTID.volatilityDetail)).toHaveCount(0);

    // Enter selects: the chip becomes aria-selected and the rail swaps the
    // summary for the DETAIL card carrying the item name + its traces caption.
    await page.keyboard.press('Enter');
    await expect(chip1).toHaveAttribute('aria-selected', 'true');
    const detail = page.getByTestId(TESTID.volatilityDetail);
    await expect(detail).toBeVisible();
    await expect(detail).toContainText('Identity Provider');
    await expect(detail).toContainText('Traces: SR-1, SR-4');
    await expect(page.getByTestId(TESTID.volatilitySummary)).toHaveCount(0);

    // The polite live region announces the selection.
    await expect(page.getByTestId(TESTID.volatilityMap).getByRole('status')).toHaveText(
      'Selected: Identity Provider, Axis 1 — over time',
    );

    // Escape anywhere in the map clears back to the summary card.
    await page.keyboard.press('Escape');
    await expect(page.getByTestId(TESTID.volatilityDetail)).toHaveCount(0);
    await expect(page.getByTestId(TESTID.volatilitySummary)).toBeVisible();
    await expect(chip1).toHaveAttribute('aria-selected', 'false');
  });

  test('the rejected-candidates disclosure expands to classified rows', async ({ page }) => {
    await openCommittedVolatilities(page);

    // Collapsed by default: a real button carrying aria-expanded + the count.
    const toggle = page.getByTestId(TESTID.volatilityRejectedToggle);
    await expect(toggle).toBeVisible();
    await expect(toggle).toContainText('REJECTED CANDIDATES · 2');
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');

    // Expanding shows one row per rejected candidate — name + the human
    // classification chip (rejectionClassLabel) + the reason.
    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');
    const row0 = page.getByTestId(TESTID.volatilityRejectedItem(0));
    await expect(row0).toBeVisible();
    await expect(row0).toContainText('Database Vendor');
    await expect(row0).toContainText('variable, not volatile');
    const row1 = page.getByTestId(TESTID.volatilityRejectedItem(1));
    await expect(row1).toBeVisible();
    await expect(row1).toContainText('Quantum Notification Fabric');
    await expect(row1).toContainText('speculative');
  });
});
