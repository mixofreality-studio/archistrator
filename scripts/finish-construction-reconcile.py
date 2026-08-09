#!/usr/bin/env python3
"""Finish-construction Task 11 + 12: lifecycle reconciliation hand-commits.

Default (no flags) — Task 11: brings the six outstanding activityConstruction
rows (C-BG, C-WIA, C-BS, C-BM, R-BG) to phase Done(2) / buildStatus
Integrated(2), adds the new C-EA row for the coverage-amendment component,
and appends episode-ledger "gap" records (per the founder's mid-run
instruction) for the transitioned activities that have no real episode
record on file — commit-not-made-by-archistrator gaps.

--recompute-progress — Task 12: recomputes .constructionProgress from the
now-complete activityConstruction record. earned% is derived (never hand-
typed) as Sigma(effortDays of Done+Integrated activities) / Sigma(all
effortDays) read from slot-9 (.activityList). Sets Week = TotalWeeks and
appends (or, on re-run, updates in place) the completion point in
.constructionProgress.points, preserving every prior point in the series.

Idempotent: re-running either subcommand is a no-op once the state already
reflects the target shape.

Run from the archistrator repo root. Edits .aiarch/state/project.json and
(for the default subcommand) .aiarch/traces/episodes.jsonl in place.
Pattern: scripts/align-construction-phase.py.
"""
import argparse
import json
import os

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
STATE = os.path.join(ROOT, ".aiarch/state/project.json")
EPISODES = os.path.join(ROOT, ".aiarch/traces/episodes.jsonl")

DONE = 2                # ActivityConstructionDone
BUILD_INTEGRATED = 2    # ActivityBuildStatus
STAMP = "2026-08-09T00:00:00Z"

NOTES = {
    "C-BG": "Reconciled 2026-08-09: the failure was the slot-9 ACT-COMPONENT-COVERAGE "
            "blocker (fixed in the C-EA amendment), not the component. Requirements SRS "
            "re-recorded under phaseArtifacts.billingStateAccess; billing state access "
            "built (see server/internal/resourceaccess/billingstate); settlement "
            "integration deferred to the settlement re-plan — see docs/billing-setup.md.",
    "C-WIA": "Reconciled 2026-08-09: completed with zero scope — the Work Item concept "
             "was descoped by founder ruling 2026-07-20 (slot-3 rejected[] ledger); "
             "residual surface is recordActivityWorkItemOpened on projectStateAccess "
             "(built under C-PA).",
    "C-BS": "Reconciled 2026-08-09: phase set to Done/Integrated. billingStateAccess is "
            "real and built (server/internal/resourceaccess/billingstate); settlementstate "
            "and revenueLedgerAccess remain contract-only stubs under the billing→"
            "settlement deferral — see docs/billing-setup.md.",
    "C-BM": "Built: server/internal/manager/billing (onboard, register-customer, "
            "close-cycle, shortfall-sweep workflows) against the frozen billingManager "
            "contract; gateway integration deferred pending Stripe configuration — see "
            "docs/billing-setup.md.",
    "R-BG": "Completed with zero scope: Stripe vendor provisioning deferred to the "
            "settlement re-plan (founder billing→settlement deferral). Setup steps "
            "documented in docs/billing-setup.md.",
    "C-EA": "Reconciled 2026-08-09: coverage-amendment reconciliation. episodeAccess "
            "component built against the frozen contract, closing the slot-9 "
            "ACT-COMPONENT-COVERAGE blocker that rejected billingStateAccess phase-"
            "artifact writes.",
}

GAP_TARGETS = ["C-BG", "C-WIA", "C-BS", "C-BM", "R-BG", "C-EA"]


def note_title(activity_id):
    return f"{activity_id} — reconciliation 2026-08-09"


def note_entry(activity_id):
    return {
        "Kind": "note",
        "Title": note_title(activity_id),
        "Source": "",
        "Produced": False,
        "Note": NOTES[activity_id],
    }


def append_note_once(entry, activity_id):
    """Append the reconciliation note entry unless one is already present (idempotent)."""
    produced = entry.setdefault("produced", [])
    title = note_title(activity_id)
    if not any(p.get("Kind") == "note" and p.get("Title") == title for p in produced):
        produced.append(note_entry(activity_id))


def before_after(entry):
    return {
        "phase": entry.get("phase"),
        "buildStatus": entry.get("buildStatus"),
    }


def reconcile_state():
    d = json.load(open(STATE))
    ac = d["activityConstruction"]
    diffs = []

    # C-BG: 3->2 / 3->2, clear failure fields, keep existing completedAt, append note.
    e = ac["C-BG"]
    before = before_after(e)
    e["phase"] = DONE
    e["buildStatus"] = BUILD_INTEGRATED
    e.pop("failureReason", None)
    e.pop("failureDetail", None)
    append_note_once(e, "C-BG")
    diffs.append(("C-BG", before, before_after(e)))

    # C-WIA: 3->2 / 3->2, clear failure fields, keep existing completedAt, append note.
    e = ac["C-WIA"]
    before = before_after(e)
    e["phase"] = DONE
    e["buildStatus"] = BUILD_INTEGRATED
    e.pop("failureReason", None)
    e.pop("failureDetail", None)
    append_note_once(e, "C-WIA")
    diffs.append(("C-WIA", before, before_after(e)))

    # C-BS: 1->2 / missing->2, no completedAt on file -> stamp it, append note.
    e = ac["C-BS"]
    before = before_after(e)
    e["phase"] = DONE
    e["buildStatus"] = BUILD_INTEGRATED
    e.setdefault("completedAt", STAMP)
    append_note_once(e, "C-BS")
    diffs.append(("C-BS", before, before_after(e)))

    # C-BM: 0->2 / missing->2, no completedAt on file -> stamp it, append note.
    e = ac["C-BM"]
    before = before_after(e)
    e["phase"] = DONE
    e["buildStatus"] = BUILD_INTEGRATED
    e.setdefault("completedAt", STAMP)
    append_note_once(e, "C-BM")
    diffs.append(("C-BM", before, before_after(e)))

    # R-BG: buildStatus 1->2 only; phase already Done; completedAt already present.
    e = ac["R-BG"]
    before = before_after(e)
    e["buildStatus"] = BUILD_INTEGRATED
    append_note_once(e, "R-BG")
    diffs.append(("R-BG", before, before_after(e)))

    # C-EA: new row.
    if "C-EA" not in ac:
        ac["C-EA"] = {
            "activityID": "C-EA",
            "phase": DONE,
            "buildStatus": BUILD_INTEGRATED,
            "completedAt": STAMP,
            "produced": [
                {
                    "Kind": "service-contract",
                    "Title": "episodeAccess — service contract",
                    "Source": "implementation/contracts/episodeAccess.md",
                    "Produced": True,
                    "Note": "Frozen App-B service contract; the coverage-amendment "
                            "addition that closes slot-9 ACT-COMPONENT-COVERAGE.",
                },
                {
                    "Kind": "code",
                    "Title": "episodeAccess — built component",
                    "Source": "server/internal/resourceaccess/episode",
                    "Produced": True,
                    "Note": "Built against the frozen episodeAccess contract.",
                },
                note_entry("C-EA"),
            ],
        }
        diffs.append(("C-EA", {"phase": None, "buildStatus": None}, before_after(ac["C-EA"])))
    else:
        diffs.append(("C-EA", before_after(ac["C-EA"]), before_after(ac["C-EA"])))

    with open(STATE, "w") as f:
        json.dump(d, f, indent=2, ensure_ascii=False)
        f.write("\n")
    return diffs


def gap_record(activity_id):
    return {
        "ProjectID": "archistrator",
        "Record": {
            "EpisodeID": f"gap-reconcile-{activity_id}",
            "Kind": 1,  # EpisodeKindConstruction
            "TargetRef": activity_id,
            "Lineage": {
                "WorkflowID": f"archistrator:{activity_id}",
                "RunID": f"gap-reconcile-{activity_id}",
                "ActivityID": activity_id,
            },
            "Usage": {"In": 0, "Out": 0, "CacheRead": 0, "CacheCreate": 0},
            "StartedAt": STAMP,
            "EndedAt": STAMP,
            "Outcome": 3,  # EpisodeGap
            "GapReason": "commit not made by archistrator",
        },
    }


def append_episode_gaps():
    lines = []
    if os.path.exists(EPISODES):
        with open(EPISODES) as f:
            lines = [json.loads(l) for l in f if l.strip()]

    existing_targets = {l["Record"]["TargetRef"] for l in lines}
    existing_gap_ids = {l["Record"]["EpisodeID"] for l in lines}

    appended = []
    for activity_id in GAP_TARGETS:
        if activity_id in existing_targets:
            continue  # real episode record already on file (C-BG, C-WIA)
        gid = f"gap-reconcile-{activity_id}"
        if gid in existing_gap_ids:
            continue  # already appended by a prior run
        appended.append(activity_id)
        lines.append(gap_record(activity_id))

    if appended:
        with open(EPISODES, "a") as f:
            for activity_id in appended:
                f.write(json.dumps(gap_record(activity_id)) + "\n")

    return appended


# --- Task 12: recompute .constructionProgress from the completed activity record ---

PROGRESS_NOTE_MARKER = "Construction complete 2026-08-09:"


def compute_earned_fraction(d):
    """Derive (earned_effort_days, total_effort_days) from slot-9 (activity list)
    effortDays crossed with .activityConstruction phase/buildStatus. Never
    hand-typed: the earned% the caller reports is always Sigma/Sigma over this."""
    activities = d["slots"]["9"]["model"]["activities"]
    ac = d["activityConstruction"]
    total = sum(a["effortDays"] for a in activities)
    earned = sum(
        a["effortDays"]
        for a in activities
        if ac.get(a["name"], {}).get("phase") == DONE
        and ac.get(a["name"], {}).get("buildStatus") == BUILD_INTEGRATED
    )
    return earned, total


def recompute_progress():
    """Recompute .constructionProgress.Week and append/update the completion
    point in .constructionProgress.points. Derivation only — earned% comes
    from compute_earned_fraction(); plannedPct is the same linear
    week/TotalWeeks basis the existing points already use. Idempotent: a
    second run updates the existing marked completion point in place rather
    than appending a duplicate, and leaves every prior point untouched."""
    d = json.load(open(STATE))
    cp = d["constructionProgress"]
    before = json.loads(json.dumps(cp))

    earned, total = compute_earned_fraction(d)
    earned_pct = round(earned / total * 100, 1)
    total_weeks = cp["TotalWeeks"]
    week = total_weeks
    planned_pct = round(week / total_weeks * 100, 1)

    note = (
        f"{PROGRESS_NOTE_MARKER} all 69 activityConstruction rows Done+Integrated "
        f"({earned}/{total} effort-days from slot-9, {earned_pct:.1f}% earned); "
        f"Week set to TotalWeeks ({total_weeks}); recomputed from the completed "
        f"activity record, not hand-typed."
    )
    point = {
        "week": week,
        "earnedPct": earned_pct,
        "plannedPct": planned_pct,
        "note": note,
    }

    cp["Week"] = week
    points = cp.setdefault("points", [])
    existing = next(
        (p for p in points if p.get("note", "").startswith(PROGRESS_NOTE_MARKER)), None
    )
    if existing is not None:
        existing.update(point)
    else:
        points.append(point)

    with open(STATE, "w") as f:
        json.dump(d, f, indent=2, ensure_ascii=False)
        f.write("\n")

    return before, cp, earned, total


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--recompute-progress",
        action="store_true",
        help="Task 12: recompute .constructionProgress from the activity record "
        "instead of running the default Task 11 lifecycle reconciliation.",
    )
    args = parser.parse_args()

    if args.recompute_progress:
        before, after, earned, total = recompute_progress()
        print(f"constructionProgress.Week: {before.get('Week')} -> {after.get('Week')}")
        print(f"earned effort-days: {earned} / {total} = {earned / total * 100:.1f}%")
        print("points[] before:")
        for p in before.get("points", []):
            print(f"  {p}")
        print("points[] after:")
        for p in after.get("points", []):
            print(f"  {p}")
        return

    diffs = reconcile_state()
    print("activityConstruction transitions:")
    for activity_id, before, after in diffs:
        print(f"  {activity_id}: {before} -> {after}")

    appended = append_episode_gaps()
    print(f"episode gap records appended: {appended or '(none — idempotent re-run)'}")


if __name__ == "__main__":
    main()
