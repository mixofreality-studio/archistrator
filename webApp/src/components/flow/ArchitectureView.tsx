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
import { listDynamicViews, toC4View } from '../../api/adapters';
import type { ArtifactModelEnvelope, ServiceContract, ServiceContracts } from '../../api/types';
import { resolveContractComponentId } from '../../api/contractComponentId';
import { useTokens } from '../../theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../constants/UIIdentifiers';
import { ArchitectureFlow } from './ArchitectureFlow';
import { DynamicViewFlow } from './DynamicViewFlow';
import { PerspectiveFlow } from './PerspectiveFlow';
import { ServiceContractView } from '../construction/ServiceContractView';
import { type Layer, LAYER_ORDER, LAYER_LABEL } from './flowLayout';

type ViewMode = 'static' | 'dynamic' | 'perspective';

export function ArchitectureView({
  envelope,
  height = 600,
  serviceContracts,
}: {
  envelope: ArtifactModelEnvelope | undefined;
  height?: number;
  /** Established component contracts, keyed by component name. When a focused
   *  component has one, Component-focus drills into its interface + diagrams. */
  serviceContracts?: ServiceContracts;
}): ReactNode {
  const t = useTokens();
  const c4 = useMemo(() => toC4View(envelope), [envelope]);
  const dynamicViews = useMemo(() => listDynamicViews(envelope), [envelope]);

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

  const [mode, setMode] = useState<ViewMode>('static');
  const [dynamicKey, setDynamicKey] = useState(defaultDynamicKey);
  const [componentId, setComponentId] = useState(defaultComponentId);

  const activeDynamicKey = dynamicViews.some((v) => v.key === dynamicKey)
    ? dynamicKey
    : defaultDynamicKey;
  const activeComponentId = c4.components.some((c) => c.id === componentId)
    ? componentId
    : defaultComponentId;
  const focusedContract = contractByComponentId.get(activeComponentId);

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
        <DynamicViewFlow envelope={envelope} height={height} viewKey={activeDynamicKey} />
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
