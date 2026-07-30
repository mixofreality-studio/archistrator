/**
 * Pure (JSX-free, xyflow-free) join of Design-Health structure findings onto the
 * architecture diagram, kept in a leaf module so it is unit-testable under
 * `node --test` (findingOverlays.test.ts — the architectureCues pattern).
 *
 * The server's live-tier rules (server/internal/utility/designhealth) locate each
 * finding via Location.Section, whose grammar this join trusts DEFENSIVELY —
 * only the verified DH shapes attach, everything else stays in the Design Health
 * list and never reaches the diagram:
 *
 *   DH-GRAPH edge rules      "From→To"        → the matching relationship edge
 *   DH-GRAPH-UTIL-REACHABLE  "utility <id>"   → the component node
 *   DH-GRAPH-MANAGER-EMPTY   "manager <id>"   → the component node
 *   DH-COMP-*                "component <id>" → the component node
 *
 * An edge finding whose relationship no longer exists in the rendered model
 * (drift: health evaluated against an older head-state) degrades to its surviving
 * endpoint node; a finding resolving to nothing is silently dropped.
 */
import type { Finding, Severity } from '../../contracts/types';

/** Findings joined onto the diagram: per-edge, per-node, plus the attached total. */
export interface StructureOverlays {
  /** Findings anchored to a relationship, keyed by edgeOverlayKey(from, to). */
  edges: Map<string, Finding[]>;
  /** Findings anchored to a component node, keyed by component id. */
  nodes: Map<string, Finding[]>;
  /** Number of findings the diagram renders (each attached finding counted once). */
  attachedCount: number;
}

export const EMPTY_STRUCTURE_OVERLAYS: StructureOverlays = {
  edges: new Map(),
  nodes: new Map(),
  attachedCount: 0,
};

/** The Go rules render an edge section as `From + "→" + To` (U+2192, no spaces). */
const EDGE_ARROW = '→';

/** Section prefixes of the node-anchored DH rules (the id is the remainder). */
const NODE_SECTION_PREFIXES = ['utility ', 'manager ', 'component '] as const;

/** The overlay key for a relationship — the exact Go section rendering. */
export function edgeOverlayKey(from: string, to: string): string {
  return `${from}${EDGE_ARROW}${to}`;
}

function push(map: Map<string, Finding[]>, key: string, f: Finding): void {
  const list = map.get(key);
  if (list === undefined) map.set(key, [f]);
  else list.push(f);
}

/**
 * Joins the Design-Health findings onto a rendered architecture model (its
 * component ids + relationship from/to pairs). Pure and total: unknown rule
 * families, missing locations, and unresolvable references are ignored rather
 * than thrown on — the diagram degrades to "no overlay", the finding still
 * shows in the Design Health step.
 */
export function computeStructureOverlays(
  findings: readonly Finding[],
  components: readonly { id: string }[],
  relationships: readonly { from: string; to: string }[]
): StructureOverlays {
  const componentIds = new Set(components.map((c) => c.id));
  const edgePairs = new Set(relationships.map((r) => edgeOverlayKey(r.from, r.to)));

  const edges = new Map<string, Finding[]>();
  const nodes = new Map<string, Finding[]>();
  let attachedCount = 0;

  for (const f of findings) {
    const section = f.location?.section ?? '';
    // Only the live-tier DH rules carry the location grammar this join trusts.
    if (section === '' || !f.ruleId.startsWith('DH-')) continue;

    // Node-anchored shapes first: "utility <id>" / "manager <id>" / "component <id>".
    const prefix = NODE_SECTION_PREFIXES.find((p) => section.startsWith(p));
    if (prefix !== undefined) {
      const id = section.slice(prefix.length);
      if (componentIds.has(id)) {
        push(nodes, id, f);
        attachedCount += 1;
      }
      continue;
    }

    // Edge-anchored DH-GRAPH shape: "From→To".
    if (f.ruleId.startsWith('DH-GRAPH-') && section.includes(EDGE_ARROW)) {
      const parts = section.split(EDGE_ARROW);
      const [from, to] = parts;
      if (parts.length !== 2 || from === undefined || from === '' || to === undefined || to === '')
        continue;
      if (edgePairs.has(section)) {
        push(edges, section, f);
        attachedCount += 1;
      } else if (componentIds.has(from)) {
        // Drift: the edge is gone from the rendered model — anchor on the caller.
        push(nodes, from, f);
        attachedCount += 1;
      } else if (componentIds.has(to)) {
        push(nodes, to, f);
        attachedCount += 1;
      }
    }
  }

  return { edges, nodes, attachedCount };
}

const SEVERITY_RANK: Record<Severity, number> = { info: 0, warning: 1, error: 2 };

/** The loudest severity in a finding list ('info' for an empty list). */
export function maxSeverity(findings: readonly Finding[]): Severity {
  let max: Severity = 'info';
  for (const f of findings) {
    if (SEVERITY_RANK[f.severity] > SEVERITY_RANK[max]) max = f.severity;
  }
  return max;
}

/** One "ruleId — message" line per finding, for tooltips and aria descriptions. */
export function findingLines(findings: readonly Finding[]): string[] {
  return findings.map((f) => `${f.ruleId} — ${f.message}`);
}

/** Copy for the legend count chip linking to the Design Health step. */
export function structureFindingsChipLabel(count: number): string {
  return `${String(count)} structure finding${count === 1 ? '' : 's'}`;
}
