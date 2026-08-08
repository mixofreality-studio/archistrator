/**
 * Pure edge logic for the deployment view: joining the AUTHORED and DERIVED
 * relationship sets the server serves, and collapsing the parallel strands
 * between one pair of elements into a single readable line.
 *
 * A deployment view carries two kinds of edge. The application edges — the SPA
 * calling the server, the server reading its stores — are DERIVED server-side
 * from the committed System model, so they cannot drift from the architecture
 * and nobody authors them. The rest — the person, the browser, the edge gateway,
 * the identity provider — have endpoints that are not System components at all
 * and are authored on the environment. The view draws their union.
 *
 * The collapse matters because derivation is per-RELATIONSHIP: WebClient calls
 * five separate Managers, all of which ship in one container, so five edges land
 * between the same two boxes. Drawn literally that is five identical lines with
 * five stacked labels. Collapsed it is one line that says how many calls it
 * carries, with the full list available on hover.
 *
 * Kept as a leaf module (no runtime imports — types are erased) so it is directly
 * unit-testable under `node --test`, the same convention as deploymentOpsLogic /
 * glossaryLogic; adapters.ts re-exports it.
 */
import type { DeploymentRelationship } from './types';

/** One drawn edge between two deployment elements. */
export interface DeploymentEdgeView {
  /** Stable id: the ordered element pair. */
  id: string;
  from: string;
  to: string;
  /** The line's caption — the single relationship's label, or an "N calls" summary. */
  label: string;
  /** The `[bracketed]` transport line under the label; empty when unstated. */
  technology: string;
  /** Every underlying relationship label, for the hover title. */
  details: string[];
  /** True when no authored relationship contributed — the picture's "this came
   *  from the architecture, not from someone's hand" cue. */
  derived: boolean;
}

/**
 * Joins the authored and derived sets into the drawn edge list, collapsing the
 * strands that share an ordered (from, to) pair.
 *
 * Authored edges are merged FIRST so a pair carrying both an authored and a
 * derived strand keeps the authored label — the authored one names the
 * deployment-level interaction ("Makes API calls to"), while a derived label is
 * the component-level operation list, which is the wrong altitude for this view.
 */
export function toDeploymentEdges(
  authored: readonly DeploymentRelationship[],
  derived: readonly DeploymentRelationship[]
): DeploymentEdgeView[] {
  const byPair = new Map<string, DeploymentEdgeView>();

  const add = (rel: DeploymentRelationship, isDerived: boolean): void => {
    if (rel.from.length === 0 || rel.to.length === 0) return;
    const id = `${rel.from}->${rel.to}`;
    const existing = byPair.get(id);
    if (existing === undefined) {
      byPair.set(id, {
        id,
        from: rel.from,
        to: rel.to,
        label: rel.label,
        technology: rel.technology,
        details: rel.label.length > 0 ? [rel.label] : [],
        derived: isDerived,
      });
      return;
    }
    if (rel.label.length > 0) existing.details.push(rel.label);
    if (existing.technology.length === 0) existing.technology = rel.technology;
    if (!isDerived) existing.derived = false;
  };

  for (const rel of authored) add(rel, false);
  for (const rel of derived) add(rel, true);

  return [...byPair.values()].map((edge) =>
    edge.details.length > 1 ? { ...edge, label: `${String(edge.details.length)} calls` } : edge
  );
}

/**
 * The element keys any edge touches — what the view uses to dim the elements
 * nothing reaches. The server-side gate (DEP-EDGE-ISOLATED) rejects those
 * outright, so on a healthy project this set covers everything drawn; the view
 * still computes it so a project mid-draft shows WHICH box is stranded rather
 * than silently looking fine.
 */
export function connectedElementKeys(edges: readonly DeploymentEdgeView[]): Set<string> {
  const keys = new Set<string>();
  for (const edge of edges) {
    keys.add(edge.from);
    keys.add(edge.to);
  }
  return keys;
}
