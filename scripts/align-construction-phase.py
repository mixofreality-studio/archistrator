#!/usr/bin/env python3
"""Align activityConstruction.Phase with the actual built state.

The seed left Phase=0 (NotStarted) on built components, so the construction pump
can't tell built from unbuilt and re-dispatches existing code. This sets
Phase=Done(2) for every activity whose component is built (or that needs no fresh
construction), leaving NotStarted(0) only for the genuinely-unbuilt C-* build
activities — the app's real construction queue.

Run from the archistrator repo root. Edits .aiarch/state/project.json in place.
"""
import json
import os
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
STATE = os.path.join(ROOT, ".aiarch/state/project.json")
SROOT = os.path.join(ROOT, "server")

DONE = 2          # ActivityConstructionDone
NOT_STARTED = 0   # ActivityConstructionNotStarted
BUILD_INTEGRATED = 2  # ActivityBuildStatus


def pkg_for(comp, layer):
    layer = (layer or "").lower().replace(" ", "")
    base = {"engine": "internal/engine", "manager": "internal/manager",
            "resourceaccess": "internal/resourceaccess", "client": "internal/client"}.get(layer)
    if not base:
        return None
    name = comp
    for suf in ("Engine", "Manager", "Access", "Client", "Gateway"):
        if name.endswith(suf):
            name = name[:-len(suf)]
    alias = {"usageAccess": "usagelog", "sourceControlAccess": "sourcecontrol",
             "durableExecutionAccess": "durableexecution", "constructionPipelineAccess": "constructionpipeline",
             "projectStateAccess": "projectstate", "artifactAccess": "artifact", "webClient": "web",
             "operationEstimationEngine": "operationestimation", "constructionEstimationEngine": "estimation"}
    cands = ([alias[comp]] if comp in alias else []) + [comp.lower(), name.lower()]
    return next((os.path.join(SROOT, base, c) for c in cands
                 if os.path.isdir(os.path.join(SROOT, base, c))), None)


def norm(s):
    return "".join(ch for ch in (s or "").lower() if ch.isalnum())


def main():
    d = json.load(open(STATE))
    sc = d.get("serviceContracts", {})
    ac = d.setdefault("activityConstruction", {})
    acts = d["slots"]["9"]["model"]["activities"]

    unbuilt = {comp for comp, c in sc.items()
               if not pkg_for(comp, c.get("Layer")) and "-" not in comp and comp != "security"}
    compnorm = {norm(c): c for c in sc}

    def comp_of(label):
        n = norm(label.split("(")[0].replace("Build", ""))
        if n in compnorm:
            return compnorm[n]
        return next((c for cn, c in compnorm.items() if cn and cn in n), None)

    targets = []   # NotStarted (the app's queue)
    for a in acts:
        name = a["name"]
        label = a.get("title") or ""
        comp = comp_of(label)
        is_unbuilt_build = name.startswith("C-") and comp in unbuilt
        entry = ac.get(name)
        if is_unbuilt_build:
            targets.append(name)
            if entry is None:
                entry = {"ActivityID": name}
                ac[name] = entry
            entry["Phase"] = NOT_STARTED
            entry["BuildStatus"] = 0
        else:
            if entry is None:
                entry = {"ActivityID": name}
                ac[name] = entry
            entry["Phase"] = DONE
            entry.setdefault("BuildStatus", BUILD_INTEGRATED)

    json.dump(d, open(STATE, "w"), indent=2)
    print(f"aligned {len(acts)} activities: {len(targets)} NotStarted, {len(acts)-len(targets)} Done")
    print("NotStarted (app construction queue):", sorted(targets))


if __name__ == "__main__":
    main()
