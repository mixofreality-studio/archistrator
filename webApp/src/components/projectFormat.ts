/**
 * Tiny presentation helpers shared by the catalog tiles, project menu, and home
 * base header — human labels for the typed ProjectPhase enum and a compact
 * relative "updated" rendering of the ISO timestamp. Pure, no React.
 */
import type { ProjectPhase } from '../api/types';

/** Human-facing label for each lifecycle phase. */
export const PHASE_LABELS: Record<ProjectPhase, string> = {
  systemDesign: 'System Design',
  projectDesign: 'Project Design',
  construction: 'Construction',
  unknown: 'Draft',
};

/** Compact relative timestamp, e.g. "just now", "3h ago", "5d ago", or a date. */
export function formatUpdatedAt(iso: string): string {
  const then = Date.parse(iso);
  if (Number.isNaN(then)) return '';
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
