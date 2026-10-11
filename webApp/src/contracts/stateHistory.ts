/**
 * THE PROJECT'S STATE HISTORY, projected from its head state (use case
 * view-the-project-state-log; spec §12 B7). Pure; no React.
 *
 * The use case reads the state repo's commit log, cross-references the applied-
 * mutations ledger, and diffs a chosen commit's typed state against its parent. The
 * web entry's basis is `QueryProjectView{summary}` — the head state — and the head
 * state carries a stamp for every change it has recorded: each committed slot's
 * provenance (when, who approved, which rail drafted, how many revisions), each
 * activity's start and finish, each lifecycle phase's exit, and each operator note.
 * This module turns those stamps into one log, newest first.
 *
 * What the head state does NOT carry is the commit-level record: the commit ids, the
 * applied-mutation entries, and any state AS OF an older commit. So the log is
 * honest about grain — one entry per recorded change, not per commit — and a drilled
 * entry says the typed diff against its parent is not on the read model, rather than
 * inventing one.
 */
import { METHOD_METADATA } from './methodMetadata.ts';
import type { ArtifactKindFull, ProjectStateWithGit } from './types';

/** What kind of recorded change an entry is. */
export type StateHistoryKind =
  | 'slotCommitted'
  | 'activityStarted'
  | 'activityCompleted'
  | 'phaseExited';

/** One labelled fact about an entry, shown when it is drilled into. */
export interface StateHistoryFact {
  label: string;
  value: string;
}

export interface StateHistoryEntry {
  /** Stable within one head state: the kind plus what it is about. */
  key: string;
  kind: StateHistoryKind;
  /** RFC3339, verbatim from the head state. */
  at: string;
  /** What changed, in a few words. */
  title: string;
  /** Where in the head state the change was recorded. */
  address: string;
  facts: StateHistoryFact[];
}

function slotTitle(kind: ArtifactKindFull): string {
  return METHOD_METADATA[kind].title;
}

/**
 * Every dated change the head state records, newest first. Ties keep a stable order
 * (by key) so the list does not reshuffle between two reads of the same state.
 */
export function stateHistoryFrom(project: ProjectStateWithGit | undefined): StateHistoryEntry[] {
  if (project === undefined) return [];
  const out: StateHistoryEntry[] = [];

  for (const slot of project.slots) {
    const at = slot.provenance?.committedAt;
    if (at === undefined || at === '') continue;
    const facts: StateHistoryFact[] = [];
    if (slot.revisions !== undefined) {
      facts.push({ label: 'Revisions committed', value: String(slot.revisions) });
    }
    if (slot.provenance?.approvedBy !== undefined && slot.provenance.approvedBy !== '') {
      facts.push({ label: 'Approved by', value: slot.provenance.approvedBy });
    }
    if (slot.provenance?.draftedBy !== undefined && slot.provenance.draftedBy !== '') {
      facts.push({ label: 'Drafted by', value: slot.provenance.draftedBy });
    }
    if (slot.staleBasis === true) {
      facts.push({ label: 'Stale basis', value: slot.staleCause ?? 'an upstream slot changed' });
    }
    out.push({
      key: `slot-${slot.kind}`,
      kind: 'slotCommitted',
      at,
      title:
        slot.revisions !== undefined && slot.revisions > 1
          ? `${slotTitle(slot.kind)} re-committed (revision ${String(slot.revisions)})`
          : `${slotTitle(slot.kind)} committed`,
      address: METHOD_METADATA[slot.kind].stateAddress,
      facts,
    });
  }

  for (const row of Object.values(project.constructionRows ?? {})) {
    const address = `project.json → activityExecution.${row.activityId}`;
    if (row.startedAt !== undefined && row.startedAt !== '') {
      out.push({
        key: `start-${row.activityId}`,
        kind: 'activityStarted',
        at: row.startedAt,
        title: `${row.activityId} started`,
        address,
        facts: row.kind !== undefined ? [{ label: 'Activity type', value: row.kind }] : [],
      });
    }
    if (row.completedAt !== undefined && row.completedAt !== '') {
      out.push({
        key: `done-${row.activityId}`,
        kind: 'activityCompleted',
        at: row.completedAt,
        title: `${row.activityId} completed`,
        address,
        facts: row.status !== undefined ? [{ label: 'Build status', value: row.status }] : [],
      });
    }
    for (const phase of row.phases) {
      if (!phase.completed || phase.completedAt === undefined || phase.completedAt === '') continue;
      out.push({
        key: `phase-${row.activityId}-${phase.phase}`,
        kind: 'phaseExited',
        at: phase.completedAt,
        title: `${row.activityId} · ${phase.label !== '' ? phase.label : phase.phase} exited`,
        address: `${address}.Phases`,
        facts: [{ label: 'Earned-value weight', value: String(phase.weight) }],
      });
    }
  }

  return out.sort((a, b) => {
    const t = Date.parse(b.at) - Date.parse(a.at);
    return t !== 0 && !Number.isNaN(t) ? t : a.key.localeCompare(b.key);
  });
}
