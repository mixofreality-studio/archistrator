import { type ReactNode, useContext, useId, useMemo, useState } from 'react';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Chip from '@mui/material/Chip';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';
import FormControl from '@mui/material/FormControl';
import Select from '@mui/material/Select';
import MenuItem from '@mui/material/MenuItem';
import { UI_IDENTIFIERS } from '../../../utilities/constants/UIIdentifiers';
import type { ScenarioView } from '../../../contracts/types';
import type { SequencedCall } from '../../../contracts/adapters';
import { toUseCaseView } from '../../../contracts/useCaseViews';
import type { Tokens } from '../../../utilities/theme/themes';
import { DynamicViewFlow } from '../../flow/DynamicViewFlow';
import { layerColors } from '../../flow/flowLayout';
import { ActivityFlow, type ActivityHighlight } from '../../usecase/ActivityFlow';
import {
  CommentsOff,
  useComments,
  testPlanBindingAnchor,
  testPlanStepAnchor,
} from '../../comments/CommentContext';
import { CommentableList } from '../../comments/CommentableList';
import { StructuredValue } from '../primitives/StructuredValue';
import { diagramFits } from '../codeDiagram/codeDiagramModel.ts';
import { useElementWidth } from '../useElementWidth';
import { StimulusDiagram } from './StimulusDiagram';
import { ScenarioLinkContext } from './scenarioLink';
import { activeScenarioId } from './scenarioSelection.ts';
import {
  presentScenario,
  type Expectation,
  type Party,
  type ScenarioContext,
  type ScenarioPresentation,
  type StimulusRow,
} from './scenarioPresentation';

/** A small uppercase section label (the plan view's typographic convention). */
function Eyebrow({ t, children }: { t: Tokens; children: ReactNode }): ReactNode {
  return (
    <Typography
      sx={{ fontFamily: t.mono, fontSize: 10.5, letterSpacing: '0.08em', color: t.muted, mb: 0.25 }}
    >
      {children}
    </Typography>
  );
}

/** What each binding standing means, said once, on hover/focus of its chip. */
const BINDING_TIP: Record<'bound' | 'partial' | 'unbound' | 'skipped', string> = {
  bound:
    'Bound: every stimulus carries concrete inputs, an expected result and a proof for each expected state — the generated test can replay it.',
  partial:
    'Partly bound: some stimuli still lack concrete inputs, an expectation or a proof. The testing gate fails until all are bound.',
  unbound:
    'Unbound: the test plan has not yet given this scenario concrete inputs or expectations.',
  skipped:
    'Skipped: bound as a skip because an upstream component is not integrated yet. A skip is not a pass.',
};

/** The binding's standing, as a chip: bound / n/m bound / unbound / skipped. */
function bindingChip(s: ScenarioView, t: Tokens): ReactNode {
  const b = s.binding;
  let label: string;
  let fg: string;
  let bg: string;
  let tip: string;
  if (b === undefined) {
    label = 'unbound';
    fg = t.dangerFg;
    bg = t.paperAlt;
    tip = BINDING_TIP.unbound;
  } else if (b.skip !== undefined) {
    label = `skipped until ${b.skip.until}`;
    fg = t.awaitingFg;
    bg = t.awaitingBg;
    tip = BINDING_TIP.skipped;
  } else {
    const bound = new Set(b.steps.map((st) => st.seq));
    const all = s.stimuli.every((st) => bound.has(st.seq));
    label = all ? 'bound' : `${String(bound.size)}/${String(s.stimuli.length)} bound`;
    fg = all ? t.committedFg : t.awaitingFg;
    bg = all ? t.committedBg : t.awaitingBg;
    tip = all ? BINDING_TIP.bound : BINDING_TIP.partial;
  }
  return (
    <Tooltip title={tip}>
      <Chip
        data-binding-status={label}
        data-testid={UI_IDENTIFIERS.Construction.SCENARIO_BINDING_CHIP}
        label={label}
        size="small"
        sx={{ height: 22, fontSize: 11, bgcolor: bg, color: fg, border: `1px solid ${t.line}` }}
        tabIndex={0}
      />
    </Tooltip>
  );
}

/** A participant's layer, said in words a first-time reader knows. */
const PARTY_TAG_TIP: Record<string, string> = {
  actor: 'A person who uses the system',
  time: 'A scheduled time event, not a person',
  unresolved: 'Not found in the committed architecture',
  client: 'Client layer — the user-facing app or API',
  manager: 'Manager layer — orchestrates a use case',
  engine: 'Engine layer — encapsulates a business rule',
  resourceAccess: 'ResourceAccess layer — reads and writes a resource',
  resource: 'Resource — a database, store or external system',
  utility: 'Utility — cross-cutting infrastructure',
};

/** A participant's name, tagged with its layer, the tag outlined in that layer's colour. */
function PartyName({ p, t }: { p: Party; t: Tokens }): ReactNode {
  const colors = layerColors(t);
  const color =
    p.kind === 'component' && p.layer !== undefined
      ? colors[p.layer]
      : p.kind === 'unknown'
        ? t.dangerFg
        : colors.person;
  const tag =
    p.kind === 'actor'
      ? 'actor'
      : p.kind === 'timer'
        ? 'time'
        : p.kind === 'unknown'
          ? 'unresolved'
          : (p.layer ?? '');
  return (
    <Box component="span" sx={{ whiteSpace: 'nowrap' }}>
      <Box component="span" sx={{ fontWeight: 700, color: t.ink }}>
        {p.name}
      </Box>
      {tag.length > 0 ? (
        <Box
          component="span"
          sx={{
            ml: 0.5,
            px: 0.5,
            fontFamily: t.body,
            fontSize: 10.5,
            // The tag's TEXT stays ink (AA on every paper); the layer colour is
            // carried by the outline, which only needs the 3:1 of a UI boundary.
            color: t.ink,
            border: `1.5px solid ${color}`,
            borderRadius: 0.5,
            verticalAlign: 'middle',
          }}
          title={PARTY_TAG_TIP[tag]}
        >
          {tag}
        </Box>
      ) : null}
    </Box>
  );
}

function kindColor(kind: StimulusRow['kind'], t: Tokens): string {
  switch (kind) {
    case 'actor input':
      return t.accent;
    case 'component call':
      return t.accent2;
    case 'time event':
      return t.awaitingFg;
    case 'decision':
      return t.muted;
  }
}

const KIND_TIP: Record<StimulusRow['kind'], string> = {
  'actor input': 'A person acts on the system through a client',
  'component call': 'Another component calls this one',
  'time event': 'A schedule fires — no person is involved',
  decision: 'A branch the use case takes at this point',
};

function KindChip({ kind, t }: { kind: StimulusRow['kind']; t: Tokens }): ReactNode {
  const c = kindColor(kind, t);
  return (
    <Tooltip title={KIND_TIP[kind]}>
      <Chip
        label={kind}
        size="small"
        sx={{
          height: 22,
          fontFamily: t.body,
          fontSize: 11,
          fontWeight: 700,
          bgcolor: 'transparent',
          color: t.ink,
          border: `1.5px solid ${c}`,
        }}
        tabIndex={0}
      />
    </Tooltip>
  );
}

/** A small labelled field: the label above, the value below — a form, not a code line. */
function Field({
  label,
  children,
  t,
}: {
  label: string;
  children: ReactNode;
  t: Tokens;
}): ReactNode {
  return (
    <Box sx={{ minWidth: 0 }}>
      <Typography sx={{ fontFamily: t.body, fontSize: 11, fontWeight: 700, color: t.muted }}>
        {label}
      </Typography>
      {children}
    </Box>
  );
}

/**
 * How one expected output/state is proven. A probe is a READ the generated test
 * makes after the stimulus — which component, which operation, with what, and
 * the answer it must get — laid out as labelled fields, never as a call string.
 */
function ProofBlock({ e, t }: { e: Expectation; t: Tokens }): ReactNode {
  const p = e.proof;
  switch (p.kind) {
    case 'probe': {
      const pr = p.probe;
      const ex = pr.expect;
      return (
        <Box
          sx={{
            display: 'flex',
            flexDirection: 'column',
            gap: 0.75,
            pl: 1.25,
            py: 0.5,
            borderLeft: `2px solid ${t.committedDot}`,
            minWidth: 0,
          }}
        >
          <Typography sx={{ fontFamily: t.body, fontSize: 12.5, color: t.ink }}>
            <Box component="span" sx={{ color: t.committedText, fontWeight: 700 }}>
              Proved by reading{' '}
            </Box>
            <Box component="span" sx={{ fontWeight: 700 }}>
              {pr.operation}
            </Box>
            <Box component="span" sx={{ color: t.muted }}>{` on ${pr.component}`}</Box>
          </Typography>
          <Box
            sx={{
              display: 'grid',
              gridTemplateColumns: { xs: 'minmax(0,1fr)', md: 'minmax(0,1fr) minmax(0,1fr)' },
              gap: 1.5,
            }}
          >
            <Field label="Asks with" t={t}>
              {pr.inputs.length === 0 ? (
                <Typography sx={{ fontFamily: t.body, fontSize: 12.5, color: t.muted }}>
                  no inputs
                </Typography>
              ) : (
                <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.5 }}>
                  {pr.inputs.map((a) => (
                    <Box key={a.name} sx={{ minWidth: 0 }}>
                      <Typography
                        sx={{ fontFamily: t.body, fontSize: 12, fontWeight: 600, color: t.ink }}
                      >
                        {a.name}
                      </Typography>
                      <StructuredValue raw={a.value} t={t} />
                    </Box>
                  ))}
                </Box>
              )}
            </Field>
            <Field label={ex.errorExpected ? 'Must fail with' : 'Must answer'} t={t}>
              {ex.errorExpected ? (
                <Typography sx={{ fontFamily: t.body, fontSize: 12.5, color: t.dangerFg }}>
                  {ex.errorCode.length > 0 ? ex.errorCode : 'any error'}
                </Typography>
              ) : ex.result.length > 0 ? (
                <StructuredValue raw={ex.result} t={t} />
              ) : (
                <Typography sx={{ fontFamily: t.body, fontSize: 12.5, color: t.muted }}>
                  any answer without an error
                </Typography>
              )}
            </Field>
          </Box>
        </Box>
      );
    }
    case 'unobservable':
      return (
        <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.muted }}>
          Declared unobservable — nothing the component exposes can read it, so no probe.
        </Typography>
      );
    case 'missing':
      return (
        <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.dangerFg }}>
          Owed: no probe, and not declared unobservable.
        </Typography>
      );
    case 'unbound':
      return (
        <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.dangerFg }}>
          Owed: needs a probe (an operation to read it back and the answer expected) or an
          “unobservable” declaration.
        </Typography>
      );
  }
}

/** The outputs / states / also-proves a stimulus carries, each with its proof. */
function ExpectationList({
  title,
  items,
  shape,
  t,
}: {
  title: string;
  items: Expectation[];
  shape: 'signal' | 'object' | 'other';
  t: Tokens;
}): ReactNode {
  if (items.length === 0) return null;
  return (
    <Box sx={{ minWidth: 0 }}>
      <Eyebrow t={t}>{title}</Eyebrow>
      <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
        {items.map((e) => (
          <Box
            key={e.node}
            sx={{ display: 'flex', flexDirection: 'column', gap: 0.5, minWidth: 0 }}
          >
            <Box sx={{ display: 'flex', alignItems: 'baseline', columnGap: 0.75 }}>
              {/* the node's UML shape in miniature: a send-signal flag, an object box */}
              <Box
                aria-hidden
                component="span"
                sx={{
                  display: 'inline-block',
                  width: 14,
                  height: 10,
                  border: `1.5px solid ${t.ink}`,
                  borderRadius: shape === 'other' ? '3px' : 0,
                  clipPath:
                    shape === 'signal'
                      ? 'polygon(0 0, 70% 0, 100% 50%, 70% 100%, 0 100%)'
                      : undefined,
                  bgcolor: shape === 'signal' ? t.ink : 'transparent',
                  flexShrink: 0,
                }}
              />
              <Typography
                component="span"
                sx={{ fontFamily: t.body, fontSize: 13, fontWeight: 600, color: t.ink }}
              >
                {e.text}
                {e.to.length > 0 ? (
                  <Box component="span" sx={{ color: t.muted, fontWeight: 400 }}>
                    {` → sent to ${e.to}`}
                  </Box>
                ) : null}
              </Typography>
            </Box>
            <Box sx={{ pl: 2.75, minWidth: 0 }}>
              <ProofBlock e={e} t={t} />
            </Box>
          </Box>
        ))}
      </Box>
    </Box>
  );
}

/** What the stimulus must return, as a reader says it. */
function ExpectedResult({ row, t }: { row: StimulusRow; t: Tokens }): ReactNode {
  switch (row.expect.kind) {
    case 'result':
      return <StructuredValue raw={row.expect.text} t={t} />;
    case 'error':
      return (
        <Typography sx={{ fontFamily: t.body, fontSize: 12.5, color: t.dangerFg }}>
          {`fails with ${row.expect.text}`}
        </Typography>
      );
    case 'none':
      return (
        <Typography sx={{ fontFamily: t.body, fontSize: 12.5, color: t.muted }}>
          No result stated — it must return without an error.
        </Typography>
      );
    case 'missing':
      return (
        <Typography sx={{ fontFamily: t.body, fontSize: 12.5, color: t.dangerFg }}>
          Owed: a result or an expected error.
        </Typography>
      );
  }
}

/** One stimulus, read whole: who calls what with which values, and what must follow. */
function StimulusCard({
  row,
  selected,
  onSelect,
  diagram,
  t,
}: {
  row: StimulusRow;
  selected: boolean;
  onSelect: () => void;
  /** Room for the code-level diagram; below it, the labelled text form. */
  diagram: boolean;
  t: Tokens;
}): ReactNode {
  return (
    <Box
      data-selected={selected ? 'true' : 'false'}
      data-seq={row.seq}
      data-testid={UI_IDENTIFIERS.Construction.SCENARIO_STIMULUS}
      sx={{
        display: 'flex',
        flexDirection: 'column',
        gap: 1.25,
        pl: 1.5,
        py: 1,
        borderLeft: `3px solid ${selected ? t.accent : t.line}`,
        cursor: 'pointer',
        minWidth: 0,
      }}
      onClick={onSelect}
    >
      {/* line 1: step number, kind, caller → component, operation */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap', minWidth: 0 }}>
        <Typography sx={{ fontFamily: t.body, fontWeight: 700, fontSize: 13, color: t.muted }}>
          {`Stimulus ${String(row.seq)}`}
        </Typography>
        <KindChip kind={row.kind} t={t} />
        <Typography
          component="span"
          sx={{ fontFamily: t.body, fontSize: 13.5, color: t.ink, wordBreak: 'break-word' }}
        >
          <PartyName p={row.caller} t={t} />
          <Box component="span" sx={{ color: t.muted, mx: 0.75 }}>
            calls
          </Box>
          <PartyName p={row.target} t={t} />
          {row.op.length > 0 ? (
            <>
              <Box component="span" sx={{ color: t.muted, mx: 0.75 }}>
                operation
              </Box>
              <Box component="span" sx={{ fontWeight: 700, color: t.ink }}>
                {row.op}
              </Box>
            </>
          ) : (
            <Box component="span" sx={{ color: t.dangerFg, ml: 0.75 }}>
              — operation not resolved
            </Box>
          )}
        </Typography>
        <Box sx={{ flexGrow: 1 }} />
        {row.hook ? (
          <Tooltip title="Hooked: the generated test hands this step's setup and checks to a hand-written hooks file instead of generating them.">
            <Chip
              label="hand-written setup"
              size="small"
              sx={{ height: 22, fontSize: 11, bgcolor: t.paperAlt, color: t.ink }}
              tabIndex={0}
            />
          </Tooltip>
        ) : null}
      </Box>
      {/* line 2: where on the path, the guard, the alternative callers — and the
          toggle that places this stimulus on the path and call-chain diagrams */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
        <Typography
          component="div"
          sx={{
            fontFamily: t.body,
            fontSize: 12.5,
            color: t.muted,
            lineHeight: 1.45,
            flex: '1 1 320px',
            minWidth: 0,
          }}
        >
          {`At the step “${row.nodeLabel}”`}
          {row.guard.length > 0 ? (
            <Box component="span" sx={{ color: t.ink }}>
              {' · only when '}
              <Box component="span" sx={{ fontWeight: 700 }}>
                {row.guard}
              </Box>
            </Box>
          ) : null}
          {row.via.length > 0 ? (
            <Box component="span">
              {' · also reachable via '}
              {row.via.map((v, i) => (
                <Box component="span" key={v.id}>
                  {i > 0 ? ', ' : ''}
                  <PartyName p={v} t={t} />
                </Box>
              ))}
            </Box>
          ) : null}
        </Typography>
        <Button
          aria-pressed={selected}
          size="small"
          sx={{ textTransform: 'none', fontSize: 12, minHeight: 32, flexShrink: 0 }}
          variant={selected ? 'contained' : 'text'}
          onClick={(e) => {
            e.stopPropagation();
            onSelect();
          }}
        >
          {selected ? 'Shown on the diagrams' : 'Show on the diagrams'}
        </Button>
      </Box>

      {diagram ? (
        // The stimulus as a code-level diagram (the contract's language): who
        // calls, with what, into which operation, and what must come back.
        <StimulusDiagram row={row} t={t} />
      ) : (
        <>
          {/* inputs | expected result */}
          <Box
            sx={{
              display: 'grid',
              gridTemplateColumns: { xs: 'minmax(0,1fr)', md: 'minmax(0,1.2fr) minmax(0,1fr)' },
              gap: 2,
            }}
          >
            <Box sx={{ minWidth: 0 }}>
              <Eyebrow t={t}>INPUTS</Eyebrow>
              {row.inputs.length === 0 ? (
                <Typography sx={{ fontFamily: t.body, fontSize: 12.5, color: t.muted }}>
                  The contract states no inputs for this operation.
                </Typography>
              ) : (
                <Box
                  component="dl"
                  sx={{
                    m: 0,
                    display: 'grid',
                    gridTemplateColumns: 'max-content minmax(0,1fr)',
                    columnGap: 1.5,
                    rowGap: 0.75,
                  }}
                >
                  {row.inputs.map((i) => (
                    <Box key={i.name} sx={{ display: 'contents' }}>
                      <Box component="dt" sx={{ minWidth: 0 }}>
                        <Typography
                          sx={{ fontFamily: t.body, fontSize: 12.5, fontWeight: 700, color: t.ink }}
                        >
                          {i.name}
                        </Typography>
                        {i.type.length > 0 ? (
                          <Typography
                            sx={{ fontFamily: t.body, fontSize: 11, color: t.muted }}
                            title="The type the contract declares for this input"
                          >
                            {i.type.replace(/^\*/, '')}
                          </Typography>
                        ) : null}
                      </Box>
                      <Box component="dd" sx={{ m: 0, minWidth: 0 }}>
                        {i.value === undefined ? (
                          <Typography
                            sx={{ fontFamily: t.body, fontSize: 12.5, color: t.dangerFg }}
                          >
                            not bound yet
                          </Typography>
                        ) : (
                          <StructuredValue raw={i.value} t={t} />
                        )}
                      </Box>
                    </Box>
                  ))}
                </Box>
              )}
            </Box>
            <Box sx={{ minWidth: 0 }}>
              <Eyebrow t={t}>
                {`EXPECTED RESULT${row.resultType.length > 0 ? ` · a ${row.resultType}` : ''}`}
              </Eyebrow>
              <ExpectedResult row={row} t={t} />
            </Box>
          </Box>

          <ExpectationList
            items={row.outputs}
            shape="signal"
            t={t}
            title="EXPECTED OUTPUTS · signals sent to an actor"
          />
          <ExpectationList
            items={row.states}
            shape="object"
            t={t}
            title="EXPECTED STATES · what must be true afterwards"
          />
          <ExpectationList
            items={row.alsoProves}
            shape="other"
            t={t}
            title="ALSO PROVES · states the diagram attaches to other stimuli on this path"
          />
        </>
      )}

      {row.hints.length > 0 ? (
        <Box>
          <Eyebrow t={t}>NOTES ON THE DIAGRAM</Eyebrow>
          {row.hints.map((h) => (
            <Typography
              key={h}
              sx={{ fontFamily: t.body, fontSize: 12.5, color: t.ink, lineHeight: 1.45 }}
            >
              {h}
            </Typography>
          ))}
        </Box>
      ) : null}

      {/* The diagram marks every owed value in its own row; the text form lists them. */}
      {!diagram && row.missing.length > 0 ? (
        <Box
          data-testid={UI_IDENTIFIERS.Construction.SCENARIO_MISSING}
          sx={{ display: 'flex', alignItems: 'center', gap: 0.5, flexWrap: 'wrap' }}
        >
          <Typography sx={{ fontFamily: t.body, fontSize: 12, fontWeight: 700, color: t.dangerFg }}>
            Still owed by the test plan:
          </Typography>
          {row.missing.map((m) => (
            <Chip
              key={m}
              label={m}
              size="small"
              sx={{
                height: 22,
                fontFamily: t.body,
                fontSize: 11,
                bgcolor: 'transparent',
                color: t.dangerFg,
                border: `1px solid ${t.dangerFg}`,
              }}
            />
          ))}
        </Box>
      ) : null}
    </Box>
  );
}

/** The scenario's header: what a reviewer reads first. */
function ScenarioHeaderCard({
  s,
  p,
  t,
  statusChip,
}: {
  s: ScenarioView;
  p: ScenarioPresentation;
  t: Tokens;
  statusChip: ((s: ScenarioView) => ReactNode) | undefined;
}): ReactNode {
  const h = p.header;
  const target = p.stimuli[0]?.target;
  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.SCENARIO_HEADER}
      sx={{ display: 'flex', flexDirection: 'column', gap: 0.75, minWidth: 0 }}
    >
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
        <Typography sx={{ fontFamily: t.body, fontWeight: 700, fontSize: 16, color: t.ink }}>
          {h.useCaseName}
        </Typography>
        {h.pathNumber > 0 ? (
          <Tooltip title="A use case can branch; each scenario follows one path through its activity diagram.">
            <Chip
              label={`path ${String(h.pathNumber)}`}
              size="small"
              sx={{
                height: 22,
                fontSize: 11,
                bgcolor: t.chatArchitectBg,
                color: t.chatArchitectFg,
              }}
              tabIndex={0}
            />
          </Tooltip>
        ) : null}
        <Box sx={{ flexGrow: 1 }} />
        {bindingChip(s, t)}
        {statusChip?.(s)}
      </Box>
      <Box
        component="dl"
        sx={{
          m: 0,
          display: 'grid',
          gridTemplateColumns: 'max-content minmax(0,1fr)',
          columnGap: 1.5,
          rowGap: 0.5,
          alignItems: 'baseline',
        }}
      >
        <Box component="dt">
          <Eyebrow t={t}>TRIGGER</Eyebrow>
        </Box>
        <Typography component="dd" sx={{ m: 0, fontFamily: t.body, fontSize: 13, color: t.ink }}>
          {h.trigger.length > 0
            ? h.trigger
            : 'unknown (the use case is not in the committed model)'}
          {h.entry.length > 0 ? (
            <Box component="span" sx={{ color: t.muted }}>{` — “${h.entry}”`}</Box>
          ) : null}
        </Typography>
        <Box component="dt">
          <Eyebrow t={t}>ACTORS</Eyebrow>
        </Box>
        <Typography component="dd" sx={{ m: 0, fontFamily: t.body, fontSize: 13, color: t.ink }}>
          {h.actors.length > 0 ? h.actors.join(' · ') : '—'}
        </Typography>
        <Box component="dt">
          <Eyebrow t={t}>BRANCHES TAKEN</Eyebrow>
        </Box>
        <Typography component="dd" sx={{ m: 0, fontFamily: t.body, fontSize: 13, color: t.ink }}>
          {h.guards.length === 0
            ? 'none — the path takes no branch'
            : h.guards.map((g, i) => (
                <Box component="span" key={`${g.at}:${g.guard}:${String(i)}`}>
                  {i > 0 ? '  ·  ' : ''}
                  <Box component="span" sx={{ fontWeight: 700 }}>
                    {g.guard.replace(/^\[|\]$/g, '')}
                  </Box>
                  <Box component="span" sx={{ color: t.muted }}>{` at “${g.atLabel}”`}</Box>
                </Box>
              ))}
        </Typography>
        <Box component="dt">
          <Eyebrow t={t}>COVERS</Eyebrow>
        </Box>
        <Typography component="dd" sx={{ m: 0, fontFamily: t.body, fontSize: 13, color: t.ink }}>
          {`${String(h.steps)} steps on the path · ${String(p.stimuli.length)} ${p.stimuli.length === 1 ? 'stimulus' : 'stimuli'} into `}
          {target !== undefined ? <PartyName p={target} t={t} /> : null}
        </Typography>
      </Box>
    </Box>
  );
}

/**
 * Scenario browser for a component's deterministic test plan (spec §2.4, §3,
 * §6.4 plan lens). Pick a projected scenario; it reads whole:
 *
 *   header    the use case's name, trigger, actors and the guards the path takes
 *   stimuli   one row per stimulus into this component (CommentableList, anchored
 *             `…bindings[scenario=<id>].steps[seq=<n>]`): the real caller → op,
 *             the contract's params with the bound values, the expectation, each
 *             expected output / state with the probe that proves it (or
 *             `unobservable`), hints, and what the binding still owes
 *   path      the path on the use case's own activity diagram (every node, the
 *             path lit, guards on the taken edges, UML shapes, note connectors)
 *   chain     the use case's call chain along the path: the real callers in their
 *             committed layers, the stimulus edges marked; selecting a stimulus
 *             lands the step-through on it and rings its node on the path
 *
 * `linked` makes the scenario follow the URL's `sc` deep link (scenarioLink.ts):
 * the plan's own browser takes it, so a component's "reached through" row opens
 * at the scenario it names. A browser inside another pane keeps its own pick.
 */
export function ScenarioBrowser({
  scenarios,
  component,
  context,
  t,
  statusChip,
  linked = false,
}: {
  scenarios: ScenarioView[];
  /** The component the scenarios are projected onto (the anchor's `testPlan[<comp>]`). */
  component: string;
  /** The committed models the scenarios are read against (absent ones degrade). */
  context: ScenarioContext;
  t: Tokens;
  statusChip?: (s: ScenarioView) => ReactNode;
  linked?: boolean;
}): ReactNode {
  const { setAnchor } = useComments();
  const pickerId = useId();
  // The stimuli draw as diagrams only where the three columns fit (codeDiagramModel).
  const [measureStimuli, stimuliWidth] = useElementWidth();
  const link = useContext(ScenarioLinkContext);
  const deepLink = linked ? link : undefined;
  const [selectedId, setSelectedId] = useState<string>('');
  const [selectedSeq, setSelectedSeq] = useState<{ scenario: string; seq: number } | null>(null);
  const activeId = activeScenarioId(scenarios, [deepLink?.scenarioId, selectedId]);
  const active = scenarios.find((s) => s.id === activeId);
  const presentations = useMemo(
    () => new Map(scenarios.map((s) => [s.id, presentScenario(s, component, context)])),
    [scenarios, component, context]
  );
  const p = active !== undefined ? presentations.get(active.id) : undefined;
  const seq = selectedSeq?.scenario === activeId ? selectedSeq.seq : (active?.stimuli[0]?.seq ?? 0);

  const decision =
    p !== undefined && p.header.useCaseIndex >= 0
      ? context.decisions[p.header.useCaseIndex]
      : undefined;
  const ucView = useMemo(
    () => (decision !== undefined ? toUseCaseView(decision) : undefined),
    [decision]
  );
  const highlight: ActivityHighlight | undefined = useMemo(() => {
    const pr = presentations.get(activeId);
    if (pr === undefined) return undefined;
    // the same pick as `seq` below, read off state so the memo keys on it directly
    const at = selectedSeq?.scenario === activeId ? selectedSeq.seq : pr.stimuli[0]?.seq;
    return {
      current: pr.stimuli.find((r) => r.seq === at)?.node ?? '',
      visitedNodes: pr.path.visitedNodes,
      visitedEdges: pr.path.visitedEdges,
    };
  }, [presentations, activeId, selectedSeq]);

  // Arm an anchored comment from the call chain's caption: a stimulus edge anchors
  // its step; any other call on the path anchors the scenario (its context).
  const onCommentStep =
    active !== undefined && p !== undefined
      ? (edge: SequencedCall): void => {
          const stim = p.chain.stimulusByEdgeSeq.get(edge.seq);
          setAnchor(
            stim !== undefined
              ? {
                  kind: 'node',
                  label: `${edge.label} (stimulus ${String(stim)})`,
                  source: `${component} · ${active.id}`,
                  jsonPath: testPlanStepAnchor(component, active.id, stim),
                }
              : {
                  kind: 'node',
                  label: `${edge.label} (call ${String(edge.seq)} on the path)`,
                  source: `${component} · ${active.id}`,
                  jsonPath: testPlanBindingAnchor(component, active.id),
                }
          );
        }
      : undefined;

  return (
    <Box
      data-active-scenario={activeId}
      sx={{ display: 'flex', flexDirection: 'column', gap: 1.5, minWidth: 0 }}
    >
      {/* scenario dropdown selector — mirrors the architecture view picker */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, minWidth: 0 }}>
        <Typography
          component="label"
          htmlFor={pickerId}
          sx={{ fontFamily: t.mono, fontSize: 10.5, letterSpacing: '0.08em', color: t.muted }}
        >
          {`SCENARIO · ${String(scenarios.length)}`}
        </Typography>
        <FormControl size="small" sx={{ flex: '1 1 auto', minWidth: 0, maxWidth: 640 }}>
          <Select
            data-testid={UI_IDENTIFIERS.Construction.SCENARIO_PICKER}
            inputProps={{ 'aria-label': 'Scenario', id: pickerId }}
            sx={{ fontFamily: t.body, fontSize: 13 }}
            value={activeId}
            onChange={(e) => {
              setSelectedId(e.target.value);
              deepLink?.onScenarioChange(e.target.value);
            }}
          >
            {scenarios.map((s) => (
              <MenuItem key={s.id} sx={{ fontFamily: t.body, fontSize: 13 }} value={s.id}>
                {presentations.get(s.id)?.pickerLabel ?? s.title}
              </MenuItem>
            ))}
          </Select>
        </FormControl>
      </Box>

      {active !== undefined && p !== undefined ? (
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5, minWidth: 0 }}>
          {/* The scenario-level anchor (spec §6.4): `…testPlan[<comp>].bindings[scenario=<id>]`,
              the binding as a whole — a skip reason, a missing binding. */}
          <CommentableList
            ariaLabel="Scenario binding"
            getAnchor={(s) => ({
              kind: 'node',
              label: `scenario:${s.id}`,
              source: `${component} · ${s.id}`,
              jsonPath: testPlanBindingAnchor(component, s.id),
            })}
            getKey={(s) => s.id}
            getLabel={() => p.pickerLabel}
            getLabelKind={() => 'scenario binding'}
            items={[active]}
            renderItem={(s) => <ScenarioHeaderCard p={p} s={s} statusChip={statusChip} t={t} />}
          />

          {active.preconditions.hints.length > 0 ||
          active.preconditions.expectedStates.length > 0 ? (
            <Box sx={{ borderLeft: `3px solid ${t.line}`, pl: 1.25, py: 0.25 }}>
              <Eyebrow t={t}>GIVEN · true before the first stimulus</Eyebrow>
              <Typography
                sx={{ fontFamily: t.body, fontSize: 12.5, color: t.ink, lineHeight: 1.5 }}
              >
                {[
                  ...active.preconditions.expectedStates.map((x) => `${x.object} ${x.inState}`),
                  ...active.preconditions.hints,
                ].join(' · ')}
              </Typography>
            </Box>
          ) : null}

          {active.binding?.skip !== undefined ? (
            <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.ink }}>
              <Box component="span" sx={{ fontWeight: 700, color: t.awaitingFg }}>
                Skipped:{' '}
              </Box>
              {active.binding.skip.reason} — until {active.binding.skip.until}. A skipped scenario
              is not a pass; the testing gate counts it.
            </Typography>
          ) : active.binding === undefined ? (
            <Typography
              data-testid={UI_IDENTIFIERS.Construction.SCENARIO_UNBOUND}
              sx={{ fontFamily: t.body, fontSize: 12, color: t.dangerFg }}
            >
              No binding yet: the test plan has not bound this scenario. Everything below is derived
              from the use case and the contracts; each stimulus is a target until it carries
              concrete inputs, an expectation and a proof for every expected state.
            </Typography>
          ) : null}

          <Box ref={measureStimuli} sx={{ minWidth: 0 }}>
            <Eyebrow t={t}>
              {`STIMULI · ${String(p.stimuli.length)} · each is one way the use case reaches this component; show one on the diagrams below`}
            </Eyebrow>
            <CommentableList
              ariaLabel="Scenario stimuli"
              gap={0.75}
              getAnchor={(r) => ({
                kind: 'node',
                label: `${r.caller.name} → ${r.target.name}.${r.op} (stimulus ${String(r.seq)})`,
                source: `${component} · ${active.id}`,
                jsonPath: testPlanStepAnchor(component, active.id, r.seq),
              })}
              getKey={(r) => `${active.id}#${String(r.seq)}`}
              getLabel={(r) => `#${String(r.seq)} ${r.caller.name} → ${r.target.name}.${r.op}`}
              getLabelKind={() => 'stimulus'}
              items={p.stimuli}
              renderItem={(r) => (
                <StimulusCard
                  diagram={diagramFits(stimuliWidth)}
                  row={r}
                  selected={r.seq === seq}
                  t={t}
                  onSelect={() => {
                    setSelectedSeq({ scenario: active.id, seq: r.seq });
                  }}
                />
              )}
            />
          </Box>

          <Box data-testid={UI_IDENTIFIERS.Construction.SCENARIO_PATH} sx={{ minWidth: 0 }}>
            <Eyebrow t={t}>
              {`PATH THROUGH “${p.header.useCaseName}” · highlighted: the steps this scenario takes · ringed: stimulus ${String(seq)}`}
            </Eyebrow>
            {ucView !== undefined && highlight !== undefined ? (
              <CommentsOff>
                <ActivityFlow
                  focusFirst
                  height={600}
                  highlight={highlight}
                  uc={ucView}
                  useCaseIndex={p.header.useCaseIndex}
                />
              </CommentsOff>
            ) : (
              <Typography sx={{ fontFamily: t.body, fontSize: 12, color: t.muted }}>
                {`The use case “${active.useCase}” is not in the committed core use cases, so its activity diagram cannot be drawn. Path: ${active.path.join(' → ')}`}
              </Typography>
            )}
          </Box>

          <Box data-testid={UI_IDENTIFIERS.Construction.SCENARIO_CHAIN} sx={{ minWidth: 0 }}>
            <Eyebrow t={t}>
              {`CALL CHAIN ALONG THE PATH · ${String(p.chain.dv.edges.length)} calls between components · the calls into this component are its stimuli`}
            </Eyebrow>
            <DynamicViewFlow
              detailBySeq={p.chain.detailBySeq}
              dv={p.chain.dv}
              height={460}
              initialStep={p.chain.edgeIndexByStimulus.get(seq) ?? 0}
              resetKey={`${active.id}#${String(seq)}`}
              statusBySeq={p.chain.statusBySeq}
              {...(p.chain.focalComponentId !== undefined
                ? { focalComponentId: p.chain.focalComponentId }
                : {})}
              {...(onCommentStep ? { onCommentStep } : {})}
            />
          </Box>
        </Box>
      ) : null}
    </Box>
  );
}
