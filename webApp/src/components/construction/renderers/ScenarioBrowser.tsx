import { type ReactNode, useState } from 'react';
import Box from '@mui/material/Box';
import Chip from '@mui/material/Chip';
import Typography from '@mui/material/Typography';
import type { TestScenarioView } from '../../../api/types';
import type { Tokens } from '../../../theme/themes';
import { SequenceFlow, type SequenceMode } from '../primitives/SequenceFlow';

/**
 * Scenario selector + single-scenario view — mirrors the service-contract
 * dynamic-view selector (chips pick one; only that one renders, no long scroll).
 * Shows the selected scenario's "what/why" summary and its sequence diagram.
 */
export function ScenarioBrowser({
  scenarios,
  mode,
  t,
  statusChip,
}: {
  scenarios: TestScenarioView[];
  mode: SequenceMode;
  t: Tokens;
  statusChip?: (s: TestScenarioView) => ReactNode;
}): ReactNode {
  const [selectedId, setSelectedId] = useState<string>('');
  const activeId = scenarios.some((s) => s.id === selectedId) ? selectedId : (scenarios[0]?.id ?? '');
  const active = scenarios.find((s) => s.id === activeId);

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5, minWidth: 0 }}>
      {/* selector chips — one per use-case scenario */}
      <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.75, alignItems: 'center' }}>
        <Typography sx={{ fontFamily: t.mono, fontSize: 10, letterSpacing: '0.08em', color: t.muted, mr: 0.5 }}>
          {`USE CASES · ${String(scenarios.length)}`}
        </Typography>
        {scenarios.map((s) => (
          <Chip
            key={s.id}
            label={s.useCase}
            size="small"
            sx={{
              fontFamily: t.mono, fontSize: 10, fontWeight: 700, cursor: 'pointer',
              bgcolor: s.id === activeId ? t.accent : t.paperAlt,
              color: s.id === activeId ? t.accentText : t.ink,
              border: `1.5px solid ${s.id === activeId ? t.accent : t.line}`,
            }}
            onClick={() => { setSelectedId(s.id); }}
          />
        ))}
      </Box>

      {active !== undefined ? (
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.75 }}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
            <Chip
              label={active.useCase}
              size="small"
              sx={{ height: 18, fontSize: 9, bgcolor: t.chatArchitectBg, color: t.chatArchitectFg }}
            />
            <Typography sx={{ fontFamily: t.body, fontWeight: 700, fontSize: 14, color: t.ink }}>
              {active.title}
            </Typography>
            <Box sx={{ flexGrow: 1 }} />
            {statusChip?.(active)}
          </Box>
          {active.description !== undefined && active.description.length > 0 ? (
            <Box sx={{ borderLeft: `3px solid ${t.line}`, pl: 1.25, py: 0.25 }}>
              <Typography sx={{ fontFamily: t.mono, fontSize: 9, letterSpacing: '0.08em', color: t.muted }}>
                WHAT THIS PROVES
              </Typography>
              <Typography sx={{ fontFamily: t.body, fontSize: 12.5, color: t.ink, lineHeight: 1.5 }}>
                {active.description}
              </Typography>
            </Box>
          ) : null}
          <SequenceFlow mode={mode} steps={active.steps ?? []} />
        </Box>
      ) : null}
    </Box>
  );
}
