/**
 * Tiny presentation helpers shared by the catalog tiles, project menu, and home
 * base header — human labels for the typed ProjectPhase enum and a compact
 * relative "updated" rendering of the ISO timestamp. Pure, no React.
 */
import type { ProjectPhase } from '../contracts/types';

/** Human-facing label for each lifecycle phase. */
export const PHASE_LABELS: Record<ProjectPhase, string> = {
  systemDesign: 'System Design',
  projectDesign: 'Project Design',
  construction: 'Construction',
  unknown: 'Draft',
};

/**
 * The chip label for a project, applying the "Operating" override (Task 14,
 * finish-construction) when the derived construction-complete signal is true.
 * Presentation-only: `phase` stays the 3-phase enum (PHASE_LABELS is untouched,
 * so the decode-fallback semantics for every other phase are unaffected);
 * "Operating" is applied here, at the render sites, on top of it.
 */
export function phaseLabel(phase: ProjectPhase, operating?: boolean): string {
  return operating === true ? 'Operating' : PHASE_LABELS[phase];
}

/** Compact relative timestamp, e.g. "just now", "3h ago", "5d ago", or a date. */
export function formatUpdatedAt(iso: string): string {
  const then = Date.parse(iso);
  // Unset/zero timestamps — Go's zero time ("0001-01-01T00:00:00Z", which parses
  // to a hugely negative epoch and would otherwise render as a bogus "12/31/1"),
  // empty strings, or anything unparseable — carry no meaning. Render an em dash
  // rather than a fake date. A real project updatedAt is always after the epoch.
  if (Number.isNaN(then) || then <= 0) return '—';
  const diffMs = Date.now() - then;
  const mins = Math.floor(diffMs / 60_000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${String(mins)}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${String(hours)}h ago`;
  const days = Math.floor(hours / 24);
  if (days < 30) return `${String(days)}d ago`;
  return new Date(then).toLocaleDateString();
}
