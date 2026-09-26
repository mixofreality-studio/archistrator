# The replay ARCHIVE

This directory is where the nineteen **pre-4b1** replay fixtures come to rest once
stage 4b1 Task 13 Step 4 moves them out of `testdata/replay/`. It is created ahead of
that move, and its numbers are re-measured **now**, while every one of the nineteen is
still green and still re-measurable — an archive's justification written after the
fixtures stopped running is a justification nobody can check.

**Nothing has moved yet.** Task 13 Step 4 does the move; this file is the reason it is
allowed to.

## What the nineteen prove

They prove ONE thing, and it is worth stating precisely because it is narrower than
"the rails worked": **no durable command sequence moved under an in-flight execution
between stage B1 and stage 4a.** A Temporal replay fixture is a recorded history plus
the workflow code that must still produce the same commands from it. It protects an
execution that is ALREADY RUNNING across a deploy; it says nothing about whether the
behaviour is right, only that it did not change shape beneath a live run.

Nineteen subtests across seven directories, measured on 2026-09-26:

| directory           | fixtures | what its histories were captured from                          |
| ------------------- | -------: | -------------------------------------------------------------- |
| `pre-b1`            |        7 | the construction child BEFORE B1/D1                            |
| `pre-d`             |        2 | the pump's other-choice selection and the stored ledger seed    |
| `post-b1`           |        3 | the B1.4 construction child (notes, rail sync)                  |
| `post-b17`          |        1 | the pump after the recorded-pause binding                       |
| `post-stage3`       |        2 | the code that WRITES the attempt and review-round ledgers       |
| `design-pre-stage4` |        3 | `systemDesignCoAuthor` and its phase advance                    |
| `phase2-pre-stage4` |        1 | `projectDesignCoAuthor`'s SDP-review commit                     |

They last replayed green at commit `c3165d9590d6df485b4be0d5be4382b0a72d0dd7`
(`Test_Replay_DesignHistories_StayDeterministic`,
`Test_Replay_Phase2Histories_StayDeterministic`,
`Test_Replay_PreB1Histories_StayDeterministic`, all `ok`, nineteen subtests passing).

## Why they sit OUTSIDE `testdata/replay/*`

Deliberately, and this is the only reason the directory is a sibling rather than a
child. Two vacuity guards live over `testdata/replay/`:

- `replayFixtureFiles` globs `testdata/replay/<dir>/*.json` and **fails** a directory
  with no fixtures in it, so an emptied directory cannot pass by finding nothing.
- `Test_Replay_EveryFixtureDirectoryIsNamed` fails on any directory under
  `testdata/replay/` that no case list names, and on finding zero directories at all.

An archived fixture is by definition one no case list names any more. Left in place it
would trip the orphan guard; deleted, it would take its evidence with it. Moved here it
is outside both globs, so neither guard sees it and neither guard is weakened to
accommodate it.

## What discharged them

The single **stage 3 + 4a + 4b drain**. A replay fixture protects an execution that is
in flight; after the drain nothing is in flight on the three workflow types these
histories were captured against — `constructionConstructActivity`,
`systemDesignCoAuthor` or `projectDesignCoAuthor`. With no live execution left to
protect, the guarantee they encode has already been delivered: keeping them as a gate
would pin the command sequence of three workflows that no longer run, which is how a
replay suite becomes a brake on the rewrite it was meant to make safe.

Their history is not discarded, which is the point of archiving rather than deleting:
the bytes stay readable, and the three replay tests' own doc comments stay in the file
that once ran them.

## What replaces them

A FRESH set, captured against `deliveryActivity` — the generic DAG child — and living
under `testdata/replay/`, where both vacuity guards apply to it. Task 13 Step 3 captures
eight of them, one per lifecycle-shape case, and from that commit on they are the ones
that hold the line across a deploy.
