/**
 * The pane's no-phase note is lens-aware in all three lenses (designer P2; the
 * integration review's minor, fix I): beside the GRAPH it points at the activity's
 * card and its lifecycle bar, beside the TASKS lens — which draws no lifecycle — at
 * the List and Graph lenses, and never at a list that is not on screen. The list's
 * own words are unchanged.
 */
/// <reference types="node" />
import { test } from 'node:test';
import assert from 'node:assert/strict';

import type { ConstructionRow } from '../../../../contracts/types.ts';
import {
  NO_CURRENT_PHASE_NOTE,
  NO_CURRENT_PHASE_NOTE_GRAPH,
  NO_CURRENT_PHASE_NOTE_TASKS,
  NO_PROFILE_NOTE,
  OBSERVED_ONLY_NO_PHASE_NOTE,
  OBSERVED_ONLY_NO_PHASE_NOTE_GRAPH,
  OBSERVED_ONLY_NO_PHASE_NOTE_TASKS,
  noBriefingNoteFor,
} from './taskBriefing.ts';

const planned: ConstructionRow = {
  activityId: 'C-x',
  classified: true,
  hasBuildEvidence: false,
  recorded: false,
  kind: 'service',
  phases: [],
  attempts: [],
};

void test('beside the graph: click a segment of its lifecycle bar', () => {
  assert.equal(noBriefingNoteFor(planned, 0, 'graph'), NO_CURRENT_PHASE_NOTE_GRAPH);
  assert.match(NO_CURRENT_PHASE_NOTE_GRAPH, /click a segment of its lifecycle bar/);
  assert.doesNotMatch(NO_CURRENT_PHASE_NOTE_GRAPH, /in the list/);
  assert.equal(noBriefingNoteFor(planned, 3, 'graph'), OBSERVED_ONLY_NO_PHASE_NOTE_GRAPH);
  assert.match(OBSERVED_ONLY_NO_PHASE_NOTE_GRAPH, /click a segment of its lifecycle bar/);
});

void test('beside the list (and by default) the words are unchanged', () => {
  assert.equal(noBriefingNoteFor(planned, 0, 'list'), NO_CURRENT_PHASE_NOTE);
  assert.equal(noBriefingNoteFor(planned), NO_CURRENT_PHASE_NOTE);
  assert.equal(noBriefingNoteFor(planned, 2), OBSERVED_ONLY_NO_PHASE_NOTE);
});

void test('beside the tasks lens: open it in the List or Graph lens, never "in the list" or "there" alone', () => {
  assert.equal(noBriefingNoteFor(planned, 0, 'tasks'), NO_CURRENT_PHASE_NOTE_TASKS);
  assert.equal(noBriefingNoteFor(planned, 3, 'tasks'), OBSERVED_ONLY_NO_PHASE_NOTE_TASKS);
  for (const note of [NO_CURRENT_PHASE_NOTE_TASKS, OBSERVED_ONLY_NO_PHASE_NOTE_TASKS]) {
    assert.match(note, /Open it in the List or Graph lens to see its lifecycle/);
    assert.doesNotMatch(note, /drawn in the list|drawn on its card/);
  }
});

void test('each lens has its own words: no two lenses share a note', () => {
  for (const hidden of [0, 3]) {
    const notes = (['list', 'graph', 'tasks'] as const).map((lens) =>
      noBriefingNoteFor(planned, hidden, lens)
    );
    assert.equal(new Set(notes).size, 3, `hidden ${String(hidden)}: ${notes.join(' | ')}`);
  }
});

void test('an unclassified activity reads the same in every lens', () => {
  const unclassified: ConstructionRow = {
    activityId: 'C-y',
    classified: false,
    hasBuildEvidence: false,
    recorded: false,
    phases: [],
    attempts: [],
  };
  for (const lens of ['list', 'graph', 'tasks'] as const) {
    assert.equal(noBriefingNoteFor(unclassified, 0, lens), NO_PROFILE_NOTE, lens);
  }
});
