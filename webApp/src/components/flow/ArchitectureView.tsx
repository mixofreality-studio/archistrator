/**
 * The System-artifact viewer: a segmented control above the diagram that switches
 * between three lenses on the same architecture —
 *
 *   Static          → ArchitectureFlow (the full layered C4 component graph)
 *   Dynamic         → DynamicViewFlow  (one call chain per use case, via a picker)
 *   Component focus → PerspectiveFlow  (one component + its inbound/outbound edges)
 *
 * Dynamic / perspective each surface a MUI Select picker (dynamic views by title;
 * components grouped by layer). Defaults: first dynamic view; first Manager (else
 * first component) for the perspective. All three reuse the shared flow chrome and
 * preserve comment anchoring through C4Node.
 */
import { useMemo, useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import ToggleButton from '@mui/material/ToggleButton';
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup';
import Select from '@mui/material/Select';
import MenuItem from '@mui/material/MenuItem';
import ListSubheader from '@mui/material/ListSubheader';
import FormControl from '@mui/material/FormControl';
import Typography from '@mui/material/Typography';
import { listDynamicViews, toC4View, toDynamicView } from '../../contracts/adapters';
import type {
  ArtifactModelEnvelope,
  ServiceContract,
  ServiceContracts,
} from '../../contracts/types';
import { resolveContractComponentId } from '../../contracts/contractComponentId';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';
import { ArchitectureFlow } from './ArchitectureFlow';
import { DynamicViewFlow } from './DynamicViewFlow';
import { PerspectiveFlow } from './PerspectiveFlow';
import { ServiceContractView } from '../construction/ServiceContractView';
import { type Layer, LAYER_ORDER, LAYER_LABEL } from './flowLayout';
import { useComments, dynamicEdgeAnchor } from '../comments/CommentContext';

type ViewMode = 'static' | 'dynamic' | 'perspective';

/**
 * Module-level memory of the last-picked lens + selections. The design experience
 * can remount this view (a background refetch / HMR flips a render branch), which
 * would otherwise snap it back to Static and lose the picker. Persisting the choice
 * here — outside the component instance — keeps the view put across remounts. Only
 * one ArchitectureView is on screen at a time; stale ids self-heal via the guards
 * below (they fall back to defaults when the id isn't in the current model).
 */
const viewMemory: { mode: ViewMode; dynamicKey: string; componentId: string } = {
  mode: 'static',
  dynamicKey: '',
  componentId: '',
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

  // Initialise from module memory (survives remounts) and mirror every change back
  // into it, so a remount restores the last lens + selection instead of Static.
  const [storedMode, setStoredMode] = useState<ViewMode>(viewMemory.mode);
  const [storedDynamicKey, setStoredDynamicKey] = useState(
    viewMemory.dynamicKey || defaultDynamicKey
  );
  const [storedComponentId, setStoredComponentId] = useState(
    viewMemory.componentId || defaultComponentId
  );
  const mode = storedMode;
  const dynamicKey = storedDynamicKey;
  const componentId = storedComponentId;
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

  const activeDynamicKey = dynamicViews.some((v) => v.key === dynamicKey)
    ? dynamicKey
    : defaultDynamicKey;
  const activeComponentId = c4.components.some((c) => c.id === componentId)
    ? componentId
    : defaultComponentId;
  const focusedContract = contractByComponentId.get(activeComponentId);
  const dynamicModel = useMemo(
    () => toDynamicView(envelope, activeDynamicKey),
    [envelope, activeDynamicKey]
  );

  // Components grouped by layer for the perspective picker.
  const grouped = useMemo(() => {
    return LAYER_ORDER.map((layer): { layer: Layer; items: typeof c4.components } => ({
      layer,
      items: c4.components.filter((c) => c.layer === layer),
    })).filter((g) => g.items.length > 0);
  }, [c4]);

  return (
    <Box>
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

      {mode === 'static' && <ArchitectureFlow envelope={envelope} height={height} />}
      {mode === 'dynamic' && (
        <DynamicViewFlow
          dv={dynamicModel}
          height={height}
          resetKey={activeDynamicKey}
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
      )}
      {mode === 'perspective' && (
        <>
          <PerspectiveFlow componentId={activeComponentId} height={height} view={c4} />
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
