/**
 * One stimulus of a test plan, drawn as a code-level diagram — the same
 * left-to-right class boxes, edge colours and legend as the contract's
 * Interface tab (codeDiagram/). The boxes are built by stimulusDiagramModel.ts;
 * this places them: the caller and its inputs on the left with edges INTO the
 * invoked operation's row, the target's «interface» in the middle, the result,
 * expected outputs and states on the right with edges OUT of that row, and each
 * proof below the state it proves on a dotted edge.
 *
 * Read-only: nothing in it is interactive, so it adds no tab stops; the frame is
 * a named group with a text summary.
 */
import { useMemo, type ReactNode } from 'react';
import type { Edge, Node } from '@xyflow/react';

import type { Tokens } from '../../../utilities/theme/themes';
import {
  boxHandle,
  diagramEdge,
  DIAGRAM_LEFT_W,
  DIAGRAM_MID_W,
  DIAGRAM_MID_X,
  DIAGRAM_RIGHT_W,
  DIAGRAM_RIGHT_X,
  IFACE_IN,
  IFACE_OUT,
  type DiagramRole,
} from '../codeDiagram/codeDiagramModel.ts';
import { CodeDiagramCanvas, DiagramLegend } from '../codeDiagram/CodeDiagramCanvas';
import type { StimulusRow } from './scenarioPresentation';
import { stimulusDiagram } from './stimulusDiagramModel.ts';

export function StimulusDiagram({ row, t }: { row: StimulusRow; t: Tokens }): ReactNode {
  const d = useMemo(() => stimulusDiagram(row), [row]);

  const { nodes, edges, roles } = useMemo((): {
    nodes: Node[];
    edges: Edge[];
    roles: DiagramRole[];
  } => {
    const ns: Node[] = [
      {
        id: 'left',
        type: 'stack',
        position: { x: 0, y: 0 },
        data: { side: 'left', width: DIAGRAM_LEFT_W, boxes: d.left },
        draggable: false,
        selectable: false,
      },
      {
        id: 'iface',
        type: 'iface',
        position: { x: DIAGRAM_MID_X, y: 0 },
        data: {
          stereotype: d.iface.stereotype,
          title: d.iface.title,
          width: DIAGRAM_MID_W,
          rows: [{ id: 'op', name: d.iface.op, detail: d.iface.detail, active: true }],
        },
        draggable: false,
        selectable: false,
      },
      {
        id: 'right',
        type: 'stack',
        position: { x: DIAGRAM_RIGHT_X, y: 0 },
        data: { side: 'right', width: DIAGRAM_RIGHT_W, boxes: d.right },
        draggable: false,
        selectable: false,
      },
    ];
    const es: Edge[] = [];
    const used = new Set<DiagramRole>();
    for (const b of d.left) {
      used.add(b.role);
      es.push(
        diagramEdge(t, b.role, {
          source: 'left',
          sourceHandle: boxHandle.side(b.id),
          target: 'iface',
          targetHandle: IFACE_IN,
        })
      );
    }
    d.right.forEach((b, i) => {
      used.add(b.role);
      if (b.role === 'proof') {
        // A proof hangs off the state right above it, on a dotted edge.
        const above = d.right[i - 1];
        if (above !== undefined) {
          es.push(
            diagramEdge(t, 'proof', {
              source: 'right',
              sourceHandle: boxHandle.proofOut(above.id),
              target: 'right',
              targetHandle: boxHandle.proofIn(b.id),
            })
          );
        }
        return;
      }
      es.push(
        diagramEdge(t, b.role, {
          source: 'iface',
          sourceHandle: IFACE_OUT,
          target: 'right',
          targetHandle: boxHandle.side(b.id),
        })
      );
    });
    const order: DiagramRole[] = ['caller', 'input', 'output', 'error', 'proof'];
    return { nodes: ns, edges: es, roles: order.filter((r) => used.has(r)) };
  }, [d, t]);

  return (
    <CodeDiagramCanvas
      caption={<DiagramLegend roles={roles} />}
      dep={`${String(row.seq)}:${String(d.left.length)}:${String(d.right.length)}`}
      edges={edges}
      label={`Stimulus ${String(row.seq)}: ${row.caller.name} calls ${row.target.name} ${d.iface.op}`}
      nodes={nodes}
      summary={d.summary}
    />
  );
}
