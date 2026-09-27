# The replay ARCHIVE

This directory holds the nineteen **pre-4b1** replay fixtures. Stage 4b1 Task 13 moved
them here out of `testdata/replay/` in the same commit that deleted the seven workflows
they were captured against, and the numbers below were re-measured **before** the move,
while every one of the nineteen was still green and still re-measurable — an archive's
justification written after the fixtures stopped running is a justification nobody can
check.

**They last replayed green at commit `4039c0e8`** (`fix(delivery): a critic's send-back
holds the gate under vibes`), the commit immediately before the deletion: nineteen
subtests, three tests, all `ok`. From the deletion commit onward nothing in this repo can
replay them, because `CoAuthorArtifactWorkflow`, `CoAuthorPhase2ArtifactWorkflow`,
`AssembleSDPReviewWorkflow`, `SystemDesignPhaseWorkflow`, `PhaseAdvanceWorkflow`,
`Phase2AdvanceWorkflow` and `ConstructActivityWorkflow` no longer exist.

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

Every one of the seven directories has the SAME last-green commit — `4039c0e8` — because
all three replay tests (`Test_Replay_DesignHistories_StayDeterministic`,
`Test_Replay_Phase2Histories_StayDeterministic`,
`Test_Replay_PreB1Histories_StayDeterministic`) ran as one suite on every commit of this
wave and were deleted together with the case lists that named them. The per-directory
capture commits are older and are recorded in the table's third column rather than as
shas, because a re-capture is forbidden for a `pre-*` directory anyway: they are the
record of what already ran.

**TWO FACTS ABOUT THE PUMP FIXTURES**, both worth keeping where the next reader of
`pre-b1/pump-*` and `pre-d/pump-*` will meet them:

- **A project could run activity N on the retired child and N+1 on the generic one.** The
  pump continues-as-new PER TICK, so the `changeGenericActivityChild` fence Task 11 added
  resolved independently on each run: a pump whose recorded history predated the marker
  kept starting `constructionConstructActivity` for the activity it was already blocked
  on, and its NEXT tick — a fresh execution — recorded v1 and started
  `deliveryActivity`. That is benign, and it is the reason the fence was per-tick rather
  than per-project: the two children have deterministic, DIFFERENT ids, so neither
  collapses onto the other and each activity is run once by exactly one of them. Task 13
  retired the fence with the workflow, which is why the drain has to precede the deploy.
- **The retired rail's own un-mirrored store rule dies with `constructactivity.go`.** The
  store refuses a RE-OPEN of an already-resolved attempt
  (`attempt %s is already resolved %q and cannot be re-resolved %q`), and the retired
  child's `DefaultVersion` resume path provoked exactly that: it skipped
  `seedResumeFromLedger`, re-minted attempt 1 and re-opened an attempt the ledger already
  held resolved. The test double deliberately did NOT mirror that rule, because mirroring
  it would have turned a pre-existing production hazard on a pre-marker history into a red
  test for a rail that was being deleted. The generic child cannot reach it —
  `seedTaskAttempts` runs unconditionally — so the hazard goes with the file rather than
  being carried forward.

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
under `testdata/replay/post-4b1/`, where both vacuity guards apply to it. There are
**eight**, one per shape spec §9 names, and from the deletion commit on they are the ones
that hold the line across a deploy:

| fixture                  | events | what it records                                            |
| ------------------------ | -----: | ---------------------------------------------------------- |
| `linear-deployment`      |    223 | three phases, dispatch→review each, the autogate throughout |
| `fork-join-stp-first`    |    417 | the srsReview fan-out, STP branch terminal first            |
| `fork-join-design-first` |    408 | the same DAG, the other completion order                    |
| `join-waits-for-all`     |    452 | `testing` waiting on a predecessor it does not gate         |
| `sendback-judged-pair`   |    537 | reject → reopen → re-dispatch → round 2 → approve           |
| `requirements-approve`   |    415 | a design activity's four phases, four gates, four commits   |
| `architecture-sendback`  |    268 | a design send-back, plus a HELD local-merge gate            |
| `m0-no-sendback`         |    201 | the M0 reject REFUSED at the gate, then the approve         |

`Test_Replay_DeliveryHistories` replays all eight and additionally fails a fixture holding
fewer than 20 events — a thin history records a park rather than a shape, and replays green
against any walker because there is barely a command sequence to disagree with.
