#!/usr/bin/env python3
"""Finish-construction Task 11: lifecycle reconciliation hand-commit.

Brings the six outstanding activityConstruction rows (C-BG, C-WIA, C-BS, C-BM,
R-BG) to phase Done(2) / buildStatus Integrated(2), adds the new C-EA row for
the coverage-amendment component, and appends episode-ledger "gap" records
(per the founder's mid-run instruction) for the transitioned activities that
have no real episode record on file — commit-not-made-by-archistrator gaps.

Idempotent: re-running is a no-op once the state already reflects the target
shape (both for the project.json edits and the episode gap-record appends).

Run from the archistrator repo root. Edits .aiarch/state/project.json and
.aiarch/traces/episodes.jsonl in place. Pattern: scripts/align-construction-phase.py.
"""
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


def main():
    diffs = reconcile_state()
    print("activityConstruction transitions:")
    for activity_id, before, after in diffs:
        print(f"  {activity_id}: {before} -> {after}")

    appended = append_episode_gaps()
    print(f"episode gap records appended: {appended or '(none — idempotent re-run)'}")


if __name__ == "__main__":
    main()
