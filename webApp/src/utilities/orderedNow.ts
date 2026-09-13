/**
 * `Date.now()`, but strictly increasing across calls: no two calls ever return the
 * same value, so "requested AFTER" has an exact answer even inside one millisecond.
 *
 * WHY (fix I). The Begin hold counts a read as the pickup only when it was requested
 * after the dispatch's record (beginControl.pickupEvidencedSince: `requestedAt > at`).
 * The console writes that record and then requests the refresh, synchronously — at
 * millisecond resolution both usually land on the SAME value, so the refresh read,
 * the first read that can show the pickup, never counted, and the rule that the
 * record comes before the refresh could not be observed at all. Both stamps now come
 * from this one clock: the record's, and every read's request (readRequestTimes).
 *
 * The values stay epoch milliseconds — a call that would repeat the last value moves
 * on by a microsecond — so a duration measured against `Date.now()` (the 60s hold)
 * is unchanged. Zero imports, so node:test pins it (orderedNow.test.ts).
 */
let last = 0;

export function orderedNow(): number {
  const now = Date.now();
  last = now > last ? now : last + 0.001;
  return last;
}
