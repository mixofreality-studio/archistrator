/**
 * ContractCodeFlow — the Interface tab of ServiceContractView: the contract as a
 * C4 level-4 (code) diagram, drawn as UML classes, never as signature strings.
 * It reads LEFT TO RIGHT, in the shared code-diagram language
 * (codeDiagram/codeDiagramModel.ts) the test plan's stimuli use too:
 *
 *   «parameters» projectID…  ─┐   ┌ «interface» BillingStateAccess ┐   ┌─▶ «result» Version
 *   «request» ReviewFeedback ─┼──▶│ ▸ RecordCharge   (selected)    │───┤
 *                              │   │   ReadBalance                  │   └─▶ «error» Error
 *
 * The middle column is the «interface» class, one row per operation (its name,
 * what it takes, what it returns). The selected operation's row is highlighted;
 * the classes it TAKES stack on the left with edges into that row, the classes
 * it RETURNS and FAILS WITH stack on the right with edges out of it — both
 * columns sitting level with the row. Selecting a row (click, Enter or Space)
 * switches the operation; selecting it again hides its classes. The first
 * operation opens selected, so the diagram shows a whole call at first sight.
 *
 * Fallback when op.inputs/op.outputs are empty: the signature's type names are
 * drawn as classes with a muted "(fields not detailed in this contract)".
 */
import { useCallback, useMemo, useState, type ReactNode } from 'react';
import type { Edge, Node } from '@xyflow/react';
import Box from '@mui/material/Box';

import type { ContractOp } from '../../contracts/types';
import type { Tokens } from '../../utilities/theme/themes';
import { useComments, contractOpAnchor } from '../comments/CommentContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';
import {
  columnSegments,
  opShape,
  parseSignature,
  readableType,
  resolveStructs,
  sharedErrorOf,
  type ColumnSegment,
} from './contractCode.ts';
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
  type ClassBoxSpec,
} from './codeDiagram/codeDiagramModel.ts';
import type { InterfaceRow } from './codeDiagram/CodeDiagramNodes';
import { CodeDiagramCanvas, DiagramLegend } from './codeDiagram/CodeDiagramCanvas';

/** "a, b and c" — a reader's list. */
function listOf(items: readonly string[]): string {
  if (items.length <= 1) return items.join('');
  return `${items.slice(0, -1).join(', ')} and ${items[items.length - 1] ?? ''}`;
}

/** One column segment as a class box (a run of plain params, or one struct). */
function segmentBox(
  seg: ColumnSegment,
  i: number,
  side: 'left' | 'right',
  paramNameOf: (type: string) => string | undefined
): ClassBoxSpec {
  const id = `${side}-${String(i)}`;
  if (seg.kind === 'params') {
    return {
      id,
      role: side === 'left' ? 'input' : 'output',
      stereotype: side === 'left' ? '«parameters» plain values passed in' : '«result»',
      title: side === 'left' ? 'Parameters' : 'Plain value returned',
      rows: seg.params.map((p) => ({
        key: p.name,
        name: p.name,
        value: readableType(p.type),
        valueTip: p.type,
        note: p.note,
      })),
      testId: UI_IDENTIFIERS.ServiceContract.STRUCT_CARD,
      rowTestId: UI_IDENTIFIERS.ServiceContract.PARAM_ROW,
      dataStruct: seg.params.map((p) => p.name).join(','),
    };
  }
  const s = seg.struct;
  const passedAs = side === 'left' ? paramNameOf(s.name) : undefined;
  return {
    id,
    role: seg.isError ? 'error' : side === 'left' ? 'input' : 'output',
    stereotype: seg.isError
      ? '«error» what a failure returns'
      : side === 'left'
        ? `«request»${passedAs !== undefined ? ` passed as ${passedAs}` : ' data passed in'}`
        : '«response» data returned',
    title: readableType(s.name),
    titleTip: s.name,
    rows: s.fields.map((f) => ({
      key: f.name,
      name: f.name,
      value: readableType(f.type),
      valueTip: f.type,
      note: f.note,
    })),
    emptyText:
      s.fields.length > 0 || seg.isError
        ? undefined
        : s._fallback === true
          ? '(fields not detailed in this contract)'
          : '(no fields)',
    testId: UI_IDENTIFIERS.ServiceContract.STRUCT_CARD,
    nameTestId: UI_IDENTIFIERS.ServiceContract.STRUCT_NAME,
    dataStruct: s.name,
  };
}

export function ContractCodeFlow({
  component,
  title,
  ops,
  t,
}: {
  /** The contract's component id (the comment anchors key on it). */
  component: string;
  /** The interface's display name (its type name); the id when absent. */
  title?: string | undefined;
  ops: ContractOp[];
  t: Tokens;
}): ReactNode {
  // The first operation opens selected: the diagram shows a whole call at first sight.
  const [activeOp, setActiveOp] = useState<string | null>(ops[0]?.signature ?? null);
  // The selected row's top inside the interface: the side columns sit level with it.
  const [rowTop, setRowTop] = useState(0);
  const { setAnchor } = useComments();
  const sharedError = useMemo(() => sharedErrorOf(ops), [ops]);
  const name = title ?? component;

  const commentOp = useCallback(
    (sig: string): void => {
      setAnchor({
        kind: 'node',
        label: opShape(sig).name,
        source: `${component} · contract op`,
        jsonPath: contractOpAnchor(component, sig),
      });
    },
    [component, setAnchor]
  );

  const { nodes, edges } = useMemo((): { nodes: Node[]; edges: Edge[] } => {
    const rows: InterfaceRow[] = ops.map((op, i) => {
      const shape = opShape(op.signature);
      const takes = shape.params.map((p) => p.name);
      const returns = shape.returns.map(readableType);
      const active = op.signature === activeOp;
      return {
        id: `${op.signature}-${String(i)}`,
        name: shape.name,
        detail: `${takes.length > 0 ? `takes ${listOf(takes)}` : 'takes nothing'} · ${
          returns.length > 0 ? `returns ${listOf(returns)}` : 'returns nothing'
        }`,
        note: [op.stereotype, op.note ?? ''].filter((x) => x.length > 0).join(' — '),
        active,
        interactive: {
          ariaLabel: `${shape.name}: ${active ? 'hide' : 'show'} what it takes and returns`,
          dataOp: op.signature,
          onToggle: (): void => {
            setActiveOp((cur) => (cur === op.signature ? null : op.signature));
          },
          onComment: (): void => {
            commentOp(op.signature);
          },
          commentLabel: `Comment on ${shape.name}`,
          commentTestId: UI_IDENTIFIERS.Comments.listItemComment(op.signature),
        },
      };
    });

    const ns: Node[] = [
      {
        id: 'iface',
        type: 'iface',
        position: { x: DIAGRAM_MID_X, y: 0 },
        data: {
          stereotype: '«interface» the operations other components may call',
          title: name,
          subtitle:
            sharedError !== undefined
              ? `Every operation can fail with ${readableType(sharedError)}`
              : undefined,
          subtitleTip: sharedError,
          width: DIAGRAM_MID_W,
          rows,
          testId: UI_IDENTIFIERS.ServiceContract.CODE_INTERFACE_NODE,
          onActiveRowTop: setRowTop,
        },
        draggable: false,
        selectable: false,
      },
    ];
    const es: Edge[] = [];

    const op = activeOp !== null ? ops.find((o) => o.signature === activeOp) : undefined;
    if (op !== undefined) {
      const shape = opShape(op.signature);
      const paramNameOf = (type: string): string | undefined =>
        shape.params.find((p) => p.type.replace(/^\*/, '').replace(/^\[\]/, '') === type)?.name;
      const { inputNames, outputNames } = parseSignature(op.signature);
      const left = columnSegments(resolveStructs(op.inputs, inputNames)).map((seg, i) =>
        segmentBox(seg, i, 'left', paramNameOf)
      );
      const right = columnSegments(resolveStructs(op.outputs, outputNames)).map((seg, i) =>
        segmentBox(seg, i, 'right', paramNameOf)
      );
      // Level with the selected row, less a little so the first box's edge runs flat.
      const y = Math.max(0, rowTop - 8);
      if (left.length > 0) {
        ns.push({
          id: 'left',
          type: 'stack',
          position: { x: 0, y },
          data: { side: 'left', width: DIAGRAM_LEFT_W, boxes: left },
          draggable: false,
          selectable: false,
        });
        for (const b of left) {
          es.push(
            diagramEdge(t, b.role, {
              source: 'left',
              sourceHandle: boxHandle.side(b.id),
              target: 'iface',
              targetHandle: IFACE_IN,
            })
          );
        }
      }
      if (right.length > 0) {
        ns.push({
          id: 'right',
          type: 'stack',
          position: { x: DIAGRAM_RIGHT_X, y },
          data: { side: 'right', width: DIAGRAM_RIGHT_W, boxes: right },
          draggable: false,
          selectable: false,
        });
        for (const b of right) {
          es.push(
            diagramEdge(t, b.role, {
              source: 'iface',
              sourceHandle: IFACE_OUT,
              target: 'right',
              targetHandle: boxHandle.side(b.id),
            })
          );
        }
      }
    }
    return { nodes: ns, edges: es };
  }, [ops, activeOp, rowTop, name, sharedError, t, commentOp]);

  const activeShape = activeOp !== null ? opShape(activeOp) : undefined;
  const count = `${String(ops.length)} operation${ops.length !== 1 ? 's' : ''}`;
  const summary =
    activeShape !== undefined
      ? `${name} offers ${count}: ${ops.map((o) => opShape(o.signature).name).join(', ')}. ` +
        `Selected: ${activeShape.name}, which takes ${
          activeShape.params.map((p) => `${p.name} (${readableType(p.type)})`).join(', ') ||
          'nothing'
        } and returns ${activeShape.returns.map(readableType).join(', ') || 'nothing'}${
          activeShape.errors.length > 0
            ? `, failing with ${activeShape.errors.map(readableType).join(', ')}`
            : ''
        }.`
      : `${name} offers ${count}. Select one to show what it takes and returns.`;

  return (
    <CodeDiagramCanvas
      caption={
        <>
          <Box component="span" sx={{ fontWeight: 700, color: t.ink }}>
            {`C4 code level · ${count}. `}
          </Box>
          {activeShape !== undefined ? (
            <>
              Showing <b>{activeShape.name}</b>: what it takes (left) and what it returns or fails
              with (right). Select another operation to switch, or it again to hide.{' '}
            </>
          ) : (
            <>Select an operation to show the classes it takes and returns. </>
          )}
          <DiagramLegend roles={['input', 'output', 'error']} />
        </>
      }
      captionTestId={UI_IDENTIFIERS.ServiceContract.CODE_CANVAS_CAPTION}
      dep={`${activeOp ?? ''}:${String(rowTop)}`}
      edges={edges}
      frameTestId={UI_IDENTIFIERS.ServiceContract.CODE_CANVAS_FRAME}
      label={`Class diagram of the ${name} interface`}
      nodes={nodes}
      summary={summary}
    />
  );
}
