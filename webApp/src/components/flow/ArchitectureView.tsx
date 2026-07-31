/**
 * The System-artifact viewer: a segmented control above the diagram that switches
 * between three lenses on the same architecture —
 *
 *   Static          → ArchitectureFlow (the full layered C4 component graph)
 *   Dynamic         → DynamicViewFlow  (one call chain per use case, via a picker)
 *   Component focus → PerspectiveFlow  (one component + its inbound/outbound edges)
 *
 * The Dynamic lens is a TRACE, not just a chain: the owning use case's activity
 * diagram is rendered beside it (ActivityFlow, the same you-are-here map the
 * use-case walkthrough uses), synced to the step that owns the current call — so
 * the reader sees the call chain realize the very steps they just walked on the
 * previous screen. Chain leads (left, 60%), map follows (right, 40%), stacking
 * chain-first on a narrow container. When the view links no use case with a
 * diagram, the chain renders full-width exactly as before.
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
  type SequencedCall,
} from '../../contracts/adapters';
import type {
  ArtifactModelEnvelope,
  ServiceContract,
  ServiceContracts,
} from '../../contracts/types';
import { useStructureFindings } from './StructureFindingsContext';
import { statusBySeqFromFindings } from './callStatus';
import { resolveContractComponentId } from '../../contracts/contractComponentId';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';
import { ArchitectureFlow } from './ArchitectureFlow';
import { DynamicViewFlow } from './DynamicViewFlow';
import { PerspectiveFlow } from './PerspectiveFlow';
import { ActivityFlow } from '../usecase/ActivityFlow';
import { traceHighlight } from './traceHighlight';
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

  // ── The side-by-side activity trace ────────────────────────────────────────
  // The step-through owns the walk's position, so it publishes it here (the call
  // it is standing on, tagged with the view it belongs to) and the activity pane
  // projects that onto the use case's own diagram. Tagging matters: view keys
  // change before the flow re-publishes, and seq numbers repeat across views —
  // an untagged position would light the WRONG step for a frame. A mismatched
  // tag reads as "no position yet" and the diagram renders plain.
  const [stepPos, setStepPos] = useState<{ key: string; call: SequencedCall | undefined }>({
    key: '',
    call: undefined,
  });
  const handleStepChange = useCallback(
    (call: SequencedCall | undefined): void => {
      setStepPos({ key: activeDynamicKey, call });
    },
    [activeDynamicKey]
  );

  // The use case this view realizes, plus its position in the committed slot
  // order (ActivityFlow's comment anchors are keyed by that index). Undefined —
  // and the chain then renders full-width, exactly as before — when the view
  // links no use case (a synthetic view), the use cases aren't loaded, or the
  // linked use case owns no activity diagram (e.g. a variation, which shares its
  // parent's; the carousel does not substitute it either).
  const tracedUseCase = useMemo(() => {
    const useCaseId = dynamicViewUseCaseId(envelope, activeDynamicKey);
    if (useCaseId === undefined) return undefined;
    const { useCases } = toCoreUseCasesView(useCasesEnvelope);
    const index = useCases.findIndex((u) => u.id === useCaseId);
    const uc = useCases[index];
    if (uc === undefined || uc.nodes.length === 0) return undefined;
    return { uc, index };
  }, [envelope, useCasesEnvelope, activeDynamicKey]);

  const activityHighlight = useMemo(() => {
    if (tracedUseCase === undefined) return undefined;
    const seq = stepPos.key === activeDynamicKey ? stepPos.call?.seq : undefined;
    return traceHighlight(dynamicModel.edges, seq, tracedUseCase.uc.edges);
  }, [tracedUseCase, dynamicModel.edges, stepPos, activeDynamicKey]);

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

  // The call-chain step-through itself, built once so the two-up trace layout and
  // the full-width fallback render the SAME element (only its frame differs).
  const dynamicFlow =
    mode === 'dynamic' ? (
      <DynamicViewFlow
        dv={dynamicModel}
        height={height}
        initialStep={consumedStep - 1}
        resetKey={`${activeDynamicKey}#${String(consumedStep)}`}
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
        onStepChange={handleStepChange}
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
            {/* The chain leads — in BOTH directions. It is the artifact under
                review, it owns the controls the reader drives (Prev/Next), and it
                is what this step is about; the map is the companion you glance at.
                Ordering it first keeps DOM order == visual order == tab order at
                every width, so the narrow layout stacks the chain on TOP (no
                `order` overrides, no focus-order mismatch). */}
            <Box sx={{ flex: sideBySide ? '1 1 60%' : '1 1 auto', minWidth: 0 }}>
              <PaneLabel>Call chain</PaneLabel>
              {dynamicFlow}
            </Box>
            {/* Supplementary pane: the use case's OWN activity diagram, walked in
                lock-step with the chain. The step position is announced by the
                step-through's caption live region (StepBar) — this map is the
                visual companion, so it is grouped and named, not a second
                announcer. */}
            <Box
              aria-label={`Activity diagram trace for ${tracedUseCase.uc.name}`}
              data-testid={UI_IDENTIFIERS.Architecture.DYNAMIC_ACTIVITY_TRACE}
              role="group"
              sx={{ flex: sideBySide ? '0 0 40%' : '1 1 auto', minWidth: 0 }}
            >
              <PaneLabel>{`Activity — ${tracedUseCase.uc.name}`}</PaneLabel>
              {/* Comment-inert: this pane belongs to the CORE USE CASES artifact,
                  and its node anchors ($.decisions[i]…) address that model — arming
                  one while reviewing the System artifact would file the comment
                  against the wrong slot. Feedback on a step belongs on the use-case
                  screen; here the diagram is a map, not a review surface. */}
              <CommentProvider enabled={false}>
                <ActivityFlow
                  height={height}
                  uc={tracedUseCase.uc}
                  useCaseIndex={tracedUseCase.index}
                  {...(activityHighlight !== undefined ? { highlight: activityHighlight } : {})}
                />
              </CommentProvider>
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
