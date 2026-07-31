/**
 * The System-artifact viewer: a segmented control above the diagram that switches
 * between three lenses on the same architecture —
 *
 *   Static          → ArchitectureFlow (the full layered C4 component graph)
 *   Dynamic         → DynamicViewFlow  (one call chain per use case, via a picker)
 *   Component focus → PerspectiveFlow  (one component + its inbound/outbound edges)
 *
 * The Dynamic lens is a walkthrough-DRIVEN trace, not just a chain: the owning
 * use case's WALKTHROUGH leads (left, 40% — the same focus card, Next / branch
 * buttons, Back / Restart, breadcrumb and you-are-here map as the use-cases
 * screen), and the call chain follows (right, 60%) in fragment mode, lighting
 * every call the current step realizes and muting everything else. The reader
 * makes the decisions; the architecture answers. Narrow containers stack
 * WALKTHROUGH-first (founder QA round 2, 2026-07-31 — reversing the earlier
 * chain-first ruling). When the view links no use case with a diagram, the chain
 * renders full-width and pages itself exactly as before.
 *
 * Dynamic / perspective each surface a MUI Select picker (dynamic views by title;
 * components grouped by layer). Defaults: first dynamic view; first Manager (else
 * first component) for the perspective. All three reuse the shared flow chrome and
 * preserve comment anchoring through C4Node.
 */
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { useRouter, type RegisteredRouter } from '@tanstack/react-router';
import Box from '@mui/material/Box';
import ToggleButton from '@mui/material/ToggleButton';
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup';
import Select from '@mui/material/Select';
import MenuItem from '@mui/material/MenuItem';
import ListSubheader from '@mui/material/ListSubheader';
import FormControl from '@mui/material/FormControl';
import Typography from '@mui/material/Typography';
import {
  listDynamicViews,
  toC4View,
  toCoreUseCasesView,
  toDynamicView,
  dynamicViewUseCaseId,
} from '../../contracts/adapters';
import type {
  ArtifactModelEnvelope,
  Finding,
  ServiceContract,
  ServiceContracts,
  System,
} from '../../contracts/types';
import { realizationByNode } from '../../contracts/realization';
import { useStructureFindings } from './StructureFindingsContext';
import { statusBySeqFromFindings } from './callStatus';
import { visitedSeqsForPath } from './callTrail';
import { findingsForStep } from './useCaseFindings';
import { resolveContractComponentId } from '../../contracts/contractComponentId';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';
import { ArchitectureFlow } from './ArchitectureFlow';
import { DynamicViewFlow } from './DynamicViewFlow';
import { resolveDecider } from './deciderResolution';
import { PerspectiveFlow } from './PerspectiveFlow';
import { UseCaseWalkthrough } from '../usecase/UseCaseWalkthrough';
import { walkthroughPathTo, walkthroughRoots } from '../usecase/walkthroughRoots';
import { ServiceContractView } from '../construction/ServiceContractView';
import { type Layer, LAYER_ORDER, LAYER_LABEL } from './flowLayout';
import { useComments, dynamicEdgeAnchor, CommentProvider } from '../comments/CommentContext';
import { resolveDeepLinkView } from './architectureDeepLink';

type ViewMode = 'static' | 'dynamic' | 'perspective';

/**
 * Module-level memory of the last-picked lens + selections. The design experience
 * can remount this view (a background refetch / HMR flips a render branch), which
 * would otherwise snap it back to Static and lose the picker. Persisting the choice
 * here — outside the component instance — keeps the view put across remounts. Only
 * one ArchitectureView is on screen at a time; stale ids self-heal via the guards
 * below (they fall back to defaults when the id isn't in the current model).
 */
const viewMemory: {
  mode: ViewMode;
  dynamicKey: string;
  /** The 1-based step last consumed off a ?view=&step= deep link, mirrored like
   *  `dynamicKey` — 0 means "no step deep-linked" (the walk opens on its first
   *  step, same as always). There is no picker for this value (unlike
   *  dynamicKey's Select), so it is only ever written by deep-link consumption. */
  dynamicStep: number;
  componentId: string;
  /** The history location key at which a ?view= deep link was last consumed —
   *  lets a fresh NAVIGATION win over memory while a background-refetch remount
   *  of the same location yields to it (see architectureDeepLink.ts). */
  consumedLocationKey: string;
} = {
  mode: 'static',
  dynamicKey: '',
  dynamicStep: 0,
  componentId: '',
  consumedLocationKey: '',
};

export function ArchitectureView({
  envelope,
  height = 600,
  serviceContracts,
  useCasesEnvelope,
}: {
  envelope: ArtifactModelEnvelope | undefined;
  height?: number;
  /** Established component contracts, keyed by component name. When a focused
   *  component has one, Component-focus drills into its interface + diagrams. */
  serviceContracts?: ServiceContracts;
  /** The committed coreUseCases envelope, when the caller has it: a dynamic view
   *  with a blank title then labels itself by its linked use case's name instead
   *  of rendering a blank picker option (F-QA2-51; see adapters.dynamicViewLabel). */
  useCasesEnvelope?: ArtifactModelEnvelope | undefined;
}): ReactNode {
  const t = useTokens();
  const { setAnchor, enabled } = useComments();
  // Design-Health structure findings for the diagram overlays, delivered via
  // context by whichever orchestrator owns the fetch (StructureFindingsContext);
  // [] when no provider is mounted or the health read hasn't resolved — the
  // Static/Component-focus lenses then render overlay-free.
  const structureFindings = useStructureFindings();
  const c4 = useMemo(() => toC4View(envelope), [envelope]);
  const dynamicViews = useMemo(
    () => listDynamicViews(envelope, useCasesEnvelope),
    [envelope, useCasesEnvelope]
  );

  // Map each established contract to its C4 component id, so a focused component
  // can surface its contract (interfaces + diagrams) once it's been designed.
  const contractByComponentId = useMemo(() => {
    const map = new Map<string, ServiceContract>();
    for (const contract of Object.values(serviceContracts ?? {})) {
      const id = resolveContractComponentId(contract.component, c4.components);
      if (id !== undefined) map.set(id, contract);
    }
    return map;
  }, [serviceContracts, c4.components]);

  const firstManager = c4.components.find((c) => c.layer === 'manager');
  const defaultComponentId = firstManager?.id ?? c4.components[0]?.id ?? '';
  const defaultDynamicKey = dynamicViews[0]?.key ?? '';

  // ?view= deep link (the use-case → call-chain jump from the carousel): read
  // through the same probe idiom as StepLink so the router-less MCP shell keeps
  // working — outside a RouterProvider both reads yield '' and module memory
  // rules as before. Non-reactive on purpose: the param matters at mount /
  // navigation time; afterwards the picker owns the selection.
  const router = useRouter({ warn: false }) as RegisteredRouter | undefined;
  const location = router?.state.location;
  const rawViewParam = (location?.search as { view?: unknown } | undefined)?.view;
  const viewParam = typeof rawViewParam === 'string' ? rawViewParam : '';
  const rawStepParam = (location?.search as { step?: unknown } | undefined)?.step;
  const stepParam =
    typeof rawStepParam === 'number' || typeof rawStepParam === 'string'
      ? String(rawStepParam)
      : '';
  const locationKey = location?.state.key ?? location?.state.__TSR_key ?? '';
  const deepLink = resolveDeepLinkView({
    viewParam,
    stepParam,
    locationKey,
    consumedLocationKey: viewMemory.consumedLocationKey,
    availableKeys: dynamicViews.map((v) => v.key),
  });

  // Initialise from module memory (survives remounts) and mirror every change back
  // into it, so a remount restores the last lens + selection instead of Static —
  // unless an unconsumed ?view= deep link targets a real dynamic view: the
  // explicit param wins on mount (and is consumed in the effect below).
  const [storedMode, setStoredMode] = useState<ViewMode>(
    deepLink.apply ? 'dynamic' : viewMemory.mode
  );
  const [storedDynamicKey, setStoredDynamicKey] = useState(
    deepLink.apply ? deepLink.key : viewMemory.dynamicKey || defaultDynamicKey
  );
  const [storedComponentId, setStoredComponentId] = useState(
    viewMemory.componentId || defaultComponentId
  );
  // The step consumed off a ?view=&step= deep link (1-based; 0 = none), mirrored
  // into module memory the same way dynamicKey is — see viewMemory.dynamicStep.
  const [storedDynamicStep, setStoredDynamicStep] = useState(
    deepLink.apply && deepLink.step !== undefined ? deepLink.step : viewMemory.dynamicStep
  );
  const mode = storedMode;
  const dynamicKey = storedDynamicKey;
  const componentId = storedComponentId;
  const consumedStep = storedDynamicStep;
  const setMode = (m: ViewMode): void => {
    viewMemory.mode = m;
    setStoredMode(m);
  };
  const setDynamicKey = (k: string): void => {
    viewMemory.dynamicKey = k;
    setStoredDynamicKey(k);
  };
  const setComponentId = (id: string): void => {
    viewMemory.componentId = id;
    setStoredComponentId(id);
  };
  const setDynamicStep = (s: number): void => {
    viewMemory.dynamicStep = s;
    setStoredDynamicStep(s);
  };

  // Consume the deep link (at most once per mount): mirror it into module memory
  // and record the location key it was consumed at, so a background-refetch
  // remount of the SAME location never snaps a reader who has since changed lens
  // back to the deep-linked view — while a new navigation (new key) re-applies.
  // Runs every render (no dep array) so a model that resolves after first paint
  // still honors the param; the resolve guard + ref keep it idempotent.
  const consumedThisMount = useRef(false);
  useEffect(() => {
    if (!deepLink.apply || consumedThisMount.current) return;
    consumedThisMount.current = true;
    viewMemory.consumedLocationKey = locationKey;
    setMode('dynamic');
    setDynamicKey(deepLink.key);
    setDynamicStep(deepLink.step ?? 0);
  });

  const activeDynamicKey = dynamicViews.some((v) => v.key === dynamicKey)
    ? dynamicKey
    : defaultDynamicKey;
  const activeComponentId = c4.components.some((c) => c.id === componentId)
    ? componentId
    : defaultComponentId;
  const focusedContract = contractByComponentId.get(activeComponentId);
  // The use-cases envelope carries the linked use case's activity graph (which
  // orders the steps and names them) and its actors (the person participants), so
  // it is passed through whenever the caller has it.
  const dynamicModel = useMemo(
    () => toDynamicView(envelope, activeDynamicKey, useCasesEnvelope),
    [envelope, activeDynamicKey, useCasesEnvelope]
  );

  // Per-call CC tint: red where the owning step carries a Design-Health finding,
  // green where the step is realized and clean. With NO findings loaded there is
  // nothing to report — an all-green chain would falsely claim it had been
  // checked — so no map is passed and the step-through keeps its neutral look.
  const dynamicStatusBySeq = useMemo(
    () =>
      structureFindings.length === 0
        ? undefined
        : statusBySeqFromFindings(dynamicModel, structureFindings, activeDynamicKey),
    [dynamicModel, structureFindings, activeDynamicKey]
  );

  // ── The walkthrough-driven trace ───────────────────────────────────────────
  // The use case this view realizes, plus its position in the committed slot
  // order (the walkthrough's comment anchors are keyed by that index). Undefined
  // — and the chain then renders full-width and self-paged, exactly as before —
  // when the view links no use case (a synthetic view), the use cases aren't
  // loaded, or the linked use case owns no activity diagram (e.g. a variation,
  // which shares its parent's; the carousel does not substitute it either).
  const tracedUseCase = useMemo(() => {
    const useCaseId = dynamicViewUseCaseId(envelope, activeDynamicKey);
    if (useCaseId === undefined) return undefined;
    const { useCases } = toCoreUseCasesView(useCasesEnvelope);
    const index = useCases.findIndex((u) => u.id === useCaseId);
    const uc = useCases[index];
    if (uc === undefined || uc.nodes.length === 0) return undefined;
    return { uc, index };
  }, [envelope, useCasesEnvelope, activeDynamicKey]);

  // The walkthrough's per-step data, built exactly as the use-cases carousel
  // builds it (realization badges + per-step Design-Health findings) so the two
  // surfaces say the same thing about the same step.
  const systemModel =
    envelope?.kind === 'system' ? (envelope.model as System | undefined) : undefined;
  const realization = useMemo(
    () => realizationByNode(systemModel, tracedUseCase?.uc.id ?? ''),
    [systemModel, tracedUseCase]
  );
  const stepFindings = useCallback(
    (nodeId: string): Finding[] => findingsForStep(structureFindings, activeDynamicKey, nodeId),
    [structureFindings, activeDynamicKey]
  );
  const firstSeqOfNode = useCallback(
    (nodeId: string): number | undefined =>
      dynamicModel.edges.find((e) => e.stepNodeId === nodeId)?.seq,
    [dynamicModel]
  );

  // A `?view=&step=` deep link lands on ONE call; the walkthrough steps by
  // activity node, so the seq resolves to its owning step and then to the route
  // a reader would have walked to reach it (BFS, deterministic). No such route
  // (or no deep link) → the walkthrough opens at its own natural beginning.
  const seedPath = useMemo(() => {
    if (tracedUseCase === undefined || consumedStep <= 0) return undefined;
    const stepNodeId = dynamicModel.edges.find((e) => e.seq === consumedStep)?.stepNodeId;
    if (stepNodeId === undefined || stepNodeId.length === 0) return undefined;
    return walkthroughPathTo(tracedUseCase.uc.nodes, tracedUseCase.uc.edges, stepNodeId);
  }, [tracedUseCase, dynamicModel, consumedStep]);

  // Where the chain looks BEFORE the walkthrough's first publish: the same node
  // the walkthrough itself will open on (its seed, else its single root — a
  // multi-root diagram opens on the entry chooser, which focuses nothing).
  // Computing it here rather than waiting for the mount effect keeps the first
  // painted frame correct instead of flashing "no realization".
  const initialWalkNodeId = useMemo(() => {
    if (tracedUseCase === undefined) return '';
    if (seedPath !== undefined) return seedPath[seedPath.length - 1] ?? '';
    const roots = walkthroughRoots(tracedUseCase.uc.nodes, tracedUseCase.uc.edges);
    return roots.length === 1 ? (roots[0] ?? '') : '';
  }, [tracedUseCase, seedPath]);

  // The walkthrough owns the position and publishes it here (the activity node
  // it stands on, tagged with the view it belongs to). Tagging matters: the view
  // key changes a render before the walkthrough remounts and re-publishes, and
  // node ids repeat across use cases — an untagged position would light the
  // WRONG fragment for a frame. A mismatched tag falls back to the opening node.
  const [walkPos, setWalkPos] = useState<{ key: string; nodeId: string }>({ key: '', nodeId: '' });
  const handleCurrentNodeChange = useCallback(
    (nodeId: string): void => {
      setWalkPos({ key: activeDynamicKey, nodeId });
    },
    [activeDynamicKey]
  );
  const focusStepNodeId = walkPos.key === activeDynamicKey ? walkPos.nodeId : initialWalkNodeId;

  // THE VISITED TRAIL (founder QA round 4). The walkthrough also publishes the
  // whole ROUTE it has walked; every call authored on a node the reader has
  // already LEFT stays lit at a mid tint, so the chain accretes instead of
  // re-lighting one lonely fragment per step. Tagged with the view key for the
  // same reason the position is — a key change lands a render before the
  // walkthrough remounts, and a stale route would light another use case's
  // calls. Before the first publish the deep link's seed route stands in, so the
  // opening frame already shows the trail the reader "arrived through".
  const [walkPath, setWalkPath] = useState<{ key: string; path: readonly string[] }>({
    key: '',
    path: [],
  });
  const handlePathChange = useCallback(
    (nodeIds: string[]): void => {
      setWalkPath({ key: activeDynamicKey, path: nodeIds });
    },
    [activeDynamicKey]
  );
  const visitedSeqs = useMemo(
    () =>
      visitedSeqsForPath(
        dynamicModel.edges,
        walkPath.key === activeDynamicKey ? walkPath.path : (seedPath ?? [])
      ),
    [dynamicModel, walkPath, activeDynamicKey, seedPath]
  );
  // The current node itself (undefined off the entry chooser or when no use
  // case is traced) — source for both its ActivityNodeKind and, for a
  // decision/switch node, its swim-lane (change 3 below).
  const focusStepNode = tracedUseCase?.uc.nodes.find((n) => n.id === focusStepNodeId);
  // DynamicViewFlow needs the kind only to tell a real realization gap (an
  // action/timeEvent/acceptEvent node authoring no calls) apart from a
  // by-design control-flow step (merge/fork/join/start/end/…) when the
  // current fragment authors no calls at all (founder QA round 3). Blank on
  // the multi-root entry chooser and when no use case is traced — the caption
  // helper treats both an unknown kind and the blank id conservatively.
  const focusStepKind = focusStepNode?.kind;
  // CHANGE 3 (founder QA round 3 addendum — "if it's a decision shouldn't the
  // person or engine responsible for making that decision be highlighted?"):
  // a decision/switch node with no realized step highlights its DECIDER
  // instead of muting the whole diagram — the actor whose swim-lane role
  // matches the node's lane, else the use case's entry Manager. Every other
  // call-less kind (and a decision/switch node that DOES author calls)
  // ignores this prop entirely — DynamicViewFlow only consults it when the
  // fragment it's driving is actually empty.
  const focusDecider = useMemo(() => {
    if (focusStepNode === undefined) return undefined;
    if (focusStepNode.kind !== 'decision' && focusStepNode.kind !== 'switch') return undefined;
    return resolveDecider(
      focusStepNode.lane,
      tracedUseCase?.uc.actors ?? [],
      dynamicModel.participants,
      dynamicModel.edges
    );
  }, [focusStepNode, tracedUseCase, dynamicModel]);

  // Container-aware split: MUI viewport breakpoints can't see that this view can
  // sit inside a narrow design-experience column, where a 40/60 split would
  // squeeze both diagrams into unreadable strips. Measure our OWN width and
  // stack the two panes when the row can't seat them (the UseCaseWalkthrough
  // idiom). Observed on the always-mounted root so the lens switch never leaves
  // the observer unattached.
  const rootRef = useRef<HTMLDivElement>(null);
  const [sideBySide, setSideBySide] = useState(true);
  useEffect(() => {
    const el = rootRef.current;
    if (el === null) return undefined;
    const ro = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (entry !== undefined) setSideBySide(entry.contentRect.width >= 1100);
    });
    ro.observe(el);
    return (): void => {
      ro.disconnect();
    };
  }, []);

  // Components grouped by layer for the perspective picker.
  const grouped = useMemo(() => {
    return LAYER_ORDER.map((layer): { layer: Layer; items: typeof c4.components } => ({
      layer,
      items: c4.components.filter((c) => c.layer === layer),
    })).filter((g) => g.items.length > 0);
  }, [c4]);

  // The call chain itself, built once so the two-up trace layout and the
  // full-width fallback render the SAME element (only its frame differs). With a
  // walkthrough beside it the chain runs in FRAGMENT mode (it follows); without
  // one it keeps its own Prev/Next step-through.
  const dynamicFlow =
    mode === 'dynamic' ? (
      <DynamicViewFlow
        dv={dynamicModel}
        height={height}
        initialStep={consumedStep - 1}
        resetKey={`${activeDynamicKey}#${String(consumedStep)}`}
        {...(tracedUseCase !== undefined ? { focusStepNodeId, visitedSeqs } : {})}
        {...(focusStepKind !== undefined ? { focusStepKind } : {})}
        {...(focusDecider !== undefined ? { focusDecider } : {})}
        {...(dynamicStatusBySeq !== undefined ? { statusBySeq: dynamicStatusBySeq } : {})}
        onCommentStep={
          enabled
            ? (edge): void => {
                const nameOf = new Map(dynamicModel.participants.map((c) => [c.id, c.name]));
                const from = nameOf.get(edge.from) ?? edge.from;
                const to = nameOf.get(edge.to) ?? edge.to;
                setAnchor({
                  kind: 'node',
                  label: `${String(edge.seq)}. ${edge.label} (${from} → ${to})`,
                  source: `${dynamicModel.title} · step`,
                  jsonPath: dynamicEdgeAnchor(activeDynamicKey, edge.seq),
                });
              }
            : undefined
        }
      />
    ) : null;

  return (
    <Box ref={rootRef}>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, mb: 1.5, flexWrap: 'wrap' }}>
        <ToggleButtonGroup
          exclusive
          color="primary"
          data-testid={UI_IDENTIFIERS.Architecture.VIEW_SWITCH}
          size="small"
          value={mode}
          onChange={(_e, next: ViewMode | null) => {
            if (next !== null) setMode(next);
          }}
        >
          <ToggleButton sx={{ fontFamily: t.mono }} value={UI_IDENTIFIERS.Architecture.VIEW_STATIC}>
            Static
          </ToggleButton>
          <ToggleButton
            disabled={dynamicViews.length === 0}
            sx={{ fontFamily: t.mono }}
            value={UI_IDENTIFIERS.Architecture.VIEW_DYNAMIC}
          >
            Dynamic
          </ToggleButton>
          <ToggleButton
            disabled={c4.components.length === 0}
            sx={{ fontFamily: t.mono }}
            value={UI_IDENTIFIERS.Architecture.VIEW_PERSPECTIVE}
          >
            Component focus
          </ToggleButton>
        </ToggleButtonGroup>

        {mode === 'dynamic' && dynamicViews.length > 0 && (
          <FormControl size="small" sx={{ minWidth: 240 }}>
            <Select
              aria-label="Dynamic view"
              data-testid={UI_IDENTIFIERS.Architecture.DYNAMIC_PICKER}
              sx={{ fontFamily: t.mono, fontSize: 13 }}
              value={activeDynamicKey}
              onChange={(e) => {
                setDynamicKey(e.target.value);
                // A manually picked view is a fresh choice, not a continuation of
                // wherever a stale deep-linked step left off — clear it so the
                // newly selected view opens on its own first step.
                setDynamicStep(0);
              }}
            >
              {dynamicViews.map((v) => (
                <MenuItem key={v.key} sx={{ fontFamily: t.mono, fontSize: 13 }} value={v.key}>
                  {v.title}
                </MenuItem>
              ))}
            </Select>
          </FormControl>
        )}

        {mode === 'perspective' && c4.components.length > 0 && (
          <FormControl size="small" sx={{ minWidth: 240 }}>
            <Select
              aria-label="Component focus"
              data-testid={UI_IDENTIFIERS.Architecture.PERSPECTIVE_PICKER}
              sx={{ fontFamily: t.mono, fontSize: 13 }}
              value={activeComponentId}
              onChange={(e) => {
                setComponentId(e.target.value);
              }}
            >
              {grouped.flatMap((g) => [
                <ListSubheader
                  key={`h-${g.layer}`}
                  sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}
                >
                  {LAYER_LABEL[g.layer]}
                </ListSubheader>,
                ...g.items.map((c) => (
                  <MenuItem key={c.id} sx={{ fontFamily: t.mono, fontSize: 13 }} value={c.id}>
                    {c.name}
                  </MenuItem>
                )),
              ])}
            </Select>
          </FormControl>
        )}
      </Box>

      {mode === 'static' && (
        <ArchitectureFlow envelope={envelope} findings={structureFindings} height={height} />
      )}
      {mode === 'dynamic' &&
        (tracedUseCase !== undefined ? (
          <Box
            sx={{
              display: 'flex',
              flexDirection: sideBySide ? 'row' : 'column',
              alignItems: 'stretch',
              gap: 2,
            }}
          >
            {/* The WALKTHROUGH leads — in both directions (founder QA round 2).
                It owns the controls the reader drives (Next, the branch buttons
                that make the decisions, Back / Restart), so ordering it first
                keeps DOM order == visual order == tab order at every width and
                the narrow layout stacks the activity on TOP (no `order`
                overrides, no focus-order mismatch).

                Comment-inert: this pane belongs to the CORE USE CASES artifact,
                and its node anchors ($.decisions[i]…) address that model — arming
                one while reviewing the System artifact would file the comment
                against the wrong slot. Feedback on a step belongs on the use-case
                screen; here the walkthrough is a control, not a review surface.

                Remounted (key) whenever the view or the deep-linked step changes,
                so a fresh `?step=` seeds a fresh route rather than leaving the
                reader wherever the previous walk stood. */}
            <Box
              aria-label={`Walkthrough of ${tracedUseCase.uc.name}`}
              data-testid={UI_IDENTIFIERS.Architecture.DYNAMIC_ACTIVITY_TRACE}
              role="group"
              sx={{ flex: sideBySide ? '0 0 40%' : '1 1 auto', minWidth: 0 }}
            >
              <PaneLabel>{`Activity — ${tracedUseCase.uc.name}`}</PaneLabel>
              <CommentProvider enabled={false}>
                <UseCaseWalkthrough
                  hideCallChainLink
                  callChainKey={activeDynamicKey}
                  firstSeqOfNode={firstSeqOfNode}
                  height={height}
                  key={`${activeDynamicKey}#${String(consumedStep)}`}
                  realization={realization}
                  stepFindings={stepFindings}
                  uc={tracedUseCase.uc}
                  useCaseIndex={tracedUseCase.index}
                  {...(seedPath !== undefined ? { initialPath: seedPath } : {})}
                  onCurrentNodeChange={handleCurrentNodeChange}
                  onPathChange={handlePathChange}
                />
              </CommentProvider>
            </Box>
            {/* The driven pane: the call chain, lighting the fragment the
                walkthrough's current step realizes. Its caption rail is the live
                region that reports where the chain now stands. */}
            <Box sx={{ flex: sideBySide ? '1 1 60%' : '1 1 auto', minWidth: 0 }}>
              <PaneLabel>Call chain</PaneLabel>
              {dynamicFlow}
            </Box>
          </Box>
        ) : (
          dynamicFlow
        ))}
      {mode === 'perspective' && (
        <>
          <PerspectiveFlow
            componentId={activeComponentId}
            findings={structureFindings}
            height={height}
            view={c4}
            onFocusComponent={setComponentId}
          />
          {/* Once the component's service contract has been established (in
              construction), drill into its interface + diagrams right here. */}
          {focusedContract !== undefined && (
            <Box sx={{ mt: 2, pt: 2, borderTop: `1.5px solid ${t.line}` }}>
              <Typography
                sx={{
                  fontFamily: t.mono,
                  fontWeight: 700,
                  fontSize: 11,
                  letterSpacing: '0.1em',
                  textTransform: 'uppercase',
                  color: t.muted,
                  mb: 1.5,
                }}
              >
                Established service contract
              </Typography>
              <ServiceContractView contract={focusedContract} systemEnvelope={envelope} />
            </Box>
          )}
        </>
      )}
    </Box>
  );
}

/**
 * The small overline naming one pane of the dynamic lens' two-up trace layout
 * ("ACTIVITY — <use case>" / "CALL CHAIN"). Each canvas already carries its own
 * frame (ActivityFlow / FlowCanvas draw the border), so the pane adds a label
 * rather than a second box around a box.
 */
function PaneLabel({ children }: { children: ReactNode }): ReactNode {
  const t = useTokens();
  return (
    <Typography
      sx={{
        fontFamily: t.mono,
        fontWeight: 700,
        fontSize: 10,
        letterSpacing: '0.1em',
        textTransform: 'uppercase',
        color: t.muted,
        mb: 0.75,
      }}
    >
      {children}
    </Typography>
  );
}
