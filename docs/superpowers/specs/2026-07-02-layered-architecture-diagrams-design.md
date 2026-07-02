# Layered top-down architecture diagrams — design

> **Amendment (after first review):** utility calls are **never drawn as lines** in
> any view (the bar just exists). Relationship **labels appear only** in the Dynamic
> tab's **step-through** — a Structurizr-style Prev/Next walk with "Step X of N" and
> a caption bar above the diagram showing the current call's text + `from → to`; the
> current edge highlights, its endpoints glow, other edges go quiet. Static hover
> keeps the mute/highlight but shows **no** labels; Component-focus shows **no**
> labels. Also fixed: the diagram view remounting and snapping back to Static — the
> lens + selection now persist in a module-level `viewMemory` singleton in
> `ArchitectureView`. The sections below describe the original (superseded) label
> behaviour; layout/gutter/utility-bar/ordering are unchanged.


Reshape the System-design diagrams (Static / Dynamic / Component-focus) in `webApp`
into the Method's canonical top-down layered style (Righting Software Fig 3-4):
each layer is a horizontal row, top→down, with Utilities as a vertical bar on the
right spanning all rows.

## Problem (grounded in real data)

The Static view already places nodes by layer (row = layer, col = insertion order),
but it reads as a tangle. Cause is **layout, not data** — all 81 edges are genuine
directed `from→to` calls. The mess comes from:

- **13 utility edges** (10 manager→utility, 3 client→utility) running from mid-graph
  down to the bottom Utility row, crossing everything.
- Nodes within a row are **not ordered under their callers**, so same-layer edges
  overlap and their labels collapse into unreadable horizontal bands.

Real layer transitions: manager→resourceAccess 30, client→manager 13,
resourceAccess→resource 12, manager→engine 12, manager→utility 10, client→utility 3,
manager→manager 1.

## Design

**Shared layered layout** (`computeLayout` in `flowLayout.ts`), used by all three views:

- Fixed rows, top→down: Client · Manager · Engine · ResourceAccess · Resource. Only
  present layers get a row (no empty gaps). `y = rowIndex * ROW_H`.
- **Utilities = a boxed vertical bar pinned right** (`utilityFrame` decorative node
  with a "Utilities" header), Utility nodes stacked vertically to span the rows.
- **Within-row ordering by barycenter**: single top-down sweep; each node's x-key is
  the average x of its already-placed callers (any upper row — handles the
  manager→resourceAccess skip). Nodes with no placed caller keep input order at the
  end. Kills most crossings.
- **Left gutter** of `rowLabel` decorative nodes: `Client`, `Business Logic` (centered
  across the Manager+Engine rows), `Resource Access`, `Resource`.

**Edges**

- Directed arrows, real `from→to` only (already true).
- **Labels hidden by default** (`label` omitted); shown only on edges incident to the
  hovered/focused node.
- **Utility edges not drawn in the base view**; revealed only when their component is
  hovered. Utility edges use side handles (source Right → target Left) so they enter
  the bar cleanly.

**Hover-to-focus (Static)**

- `onNodeMouseEnter`/`Leave` set `hoveredId`. Incident set = hovered + direct
  callers + direct callees. Non-incident nodes dim (low opacity); non-incident edges
  hide. Incident edges highlight (accent stroke, arrow) and reveal labels (utility
  edges included). Decorative nodes never dim.

**Component-focus tab** — recompute the shared layout over just {focus + direct
in/out neighbors} and their edges; labels always on. A "pinned" version of the Static
hover state.

**Dynamic tab** — shared layout over participants + sequenced edges; single call
chain, so `1. …` labels stay visible.

## Files

- `flowLayout.ts` — geometry consts, `computeLayout`, `flowEdge` gains handle ids +
  label/variant options; `nodeTypes` moves out (see below).
- `flowDecor.tsx` (new) — `RowLabelNode`, `UtilityFrameNode`.
- `flowShared.tsx` — owns `nodeTypes = { c4, rowLabel, utilityFrame }`; `FlowCanvas`
  gains `onNodeMouseEnter`/`onNodeMouseLeave` passthrough.
- `C4Node.tsx` — add Left target + Right source handles (opacity 0) for utility edges;
  dim styling via `data`.
- `ArchitectureFlow.tsx` — new build + hover state.
- `DynamicViewFlow.tsx`, `PerspectiveFlow.tsx` — adopt shared layout.

## Process

Exploratory ("find what feels right"): implement in reviewable steps, each verified
in the browser at `localhost:5199`, STOP for review — (1) Static relayout + hover,
(2) Dynamic, (3) Component-focus. No new dependency.
