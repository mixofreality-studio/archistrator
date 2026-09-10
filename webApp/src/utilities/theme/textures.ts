/**
 * The house TEXTURES — CSS background-image generators, and nothing else.
 *
 * `scan()` has lived in themes.ts since the port from the UX mock, where it
 * paints each palette's `texture` token. It moved here so a SECOND consumer
 * could reach it without dragging the theme module along: the construction
 * LIST lens draws provenance as a hatched rail (see components/construction/
 * provenance.ts), that module is a plain `.ts` covered by node:test, and
 * themes.ts value-imports `@mui/material/styles`. Importing themes.ts from a
 * tested module would pull MUI into the test runner for the sake of one
 * template string.
 *
 * This file therefore has ZERO imports and must keep it that way.
 *
 * `scanlines` is the geometry; `scan` is the alpha-over-rgb convenience the
 * palettes already use, now expressed in terms of it. Re-deriving the same
 * gradient in a second place would be exactly the hand-mirror this codebase has
 * paid for before — one geometry, one definition.
 */

/** The scanline geometry: 2px of `ink`, 2px of nothing, repeating horizontally. */
export const scanlines = (ink: string): string =>
  `repeating-linear-gradient(0deg, ${ink} 0 2px, transparent 2px 4px)`;

/** Scanlines in `rgba(<c>, <a>)` — the form the five palettes' `texture` token takes. */
export const scan = (a: number, c = '34,32,27'): string => scanlines(`rgba(${c},${String(a)})`);
