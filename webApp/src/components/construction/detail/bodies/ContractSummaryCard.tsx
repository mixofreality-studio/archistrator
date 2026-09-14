/**
 * The contract SUMMARY card and its one-line REFERENCE form (designer §2.2).
 *
 * The summary is what a bare activity click shows — stereotype · component ·
 * layer, the volatility sentence, the op count and the first five signatures —
 * with one click to the full view in Detailed Design and one to the focus view.
 * No canvas mounts here, so browsing 29 rows stays cheap. Below 600px it is also
 * what every contract collapses to, the focus view being the only place a
 * diagram draws.
 */
import type { ReactElement } from 'react';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Link from '@mui/material/Link';
import Typography from '@mui/material/Typography';
import OpenInFullRoundedIcon from '@mui/icons-material/OpenInFullRounded';

import type { ServiceContract } from '../../../../contracts/types';
import { useTokens } from '../../../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../../../utilities/constants/UIIdentifiers';
import { ArtifactFrame } from './ArtifactFrame';
import type { ArtifactRole } from './artifactPlacement.ts';

/** How many signatures the summary lists before "+N more". */
const SUMMARY_OPS = 5;

export function ContractSummaryCard({
  contract,
  artifactRole,
  title,
  source,
  onOpenDesign,
  onFocus,
}: {
  contract: ServiceContract;
  artifactRole: ArtifactRole;
  title: string;
  source: string;
  /** Select this activity's Detailed Design phase. Absent when already there. */
  onOpenDesign?: (() => void) | undefined;
  onFocus?: (() => void) | undefined;
}): ReactElement {
  const t = useTokens();
  const ops = contract.ops ?? [];
  const shown = ops.slice(0, SUMMARY_OPS);
  const more = ops.length - shown.length;
  const head = [contract.stereotype, contract.component, contract.layer]
    .filter((s): s is string => s !== undefined && s.length > 0)
    .join(' · ');

  return (
    <ArtifactFrame
      artifactRole={artifactRole}
      kindTestId={UI_IDENTIFIERS.Construction.CONTRACT_SUMMARY}
      source={source}
      title={title}
      onFocus={onFocus}
    >
      <Box
        data-testid={UI_IDENTIFIERS.Construction.CONTRACT_SUMMARY}
        sx={{
          display: 'flex',
          flexDirection: 'column',
          gap: 0.75,
          p: 1.25,
          border: `1px solid ${t.line}`,
          borderRadius: `${String(t.radius)}px`,
          bgcolor: t.paperAlt,
          minWidth: 0,
        }}
      >
        <Typography sx={{ fontFamily: t.mono, fontSize: 11.5, fontWeight: 700, color: t.ink }}>
          {head}
        </Typography>
        {contract.volatility !== undefined && contract.volatility.length > 0 ? (
          <Typography
            sx={{
              fontFamily: t.body,
              fontSize: 12,
              color: t.muted,
              lineHeight: 1.45,
              display: '-webkit-box',
              WebkitLineClamp: 3,
              WebkitBoxOrient: 'vertical',
              overflow: 'hidden',
            }}
          >
            {contract.volatility}
          </Typography>
        ) : null}
        <Typography sx={{ fontFamily: t.mono, fontSize: 10, fontWeight: 700, color: t.muted }}>
          {`${String(ops.length)} ${ops.length === 1 ? 'op' : 'ops'}`}
        </Typography>
        <Box component="ul" sx={{ m: 0, pl: 0, listStyle: 'none', minWidth: 0 }}>
          {shown.map((op, i) => (
            <Typography
              component="li"
              key={`${op.signature}-${String(i)}`}
              sx={{
                fontFamily: t.mono,
                fontSize: 10.5,
                color: t.ink,
                lineHeight: 1.55,
                whiteSpace: 'nowrap',
                overflow: 'hidden',
                textOverflow: 'ellipsis',
              }}
              title={op.signature}
            >
              {op.signature}
            </Typography>
          ))}
          {more > 0 ? (
            <Typography component="li" sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}>
              {`+${String(more)} more`}
            </Typography>
          ) : null}
        </Box>
        <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap', mt: 0.25 }}>
          {onOpenDesign !== undefined ? (
            <Button
              data-testid={UI_IDENTIFIERS.Construction.CONTRACT_SUMMARY_OPEN}
              size="small"
              sx={{ fontFamily: t.mono, fontSize: 11, fontWeight: 700, textTransform: 'none' }}
              variant="outlined"
              onClick={onOpenDesign}
            >
              Open in Detailed Design →
            </Button>
          ) : null}
          {onFocus !== undefined ? (
            <Button
              size="small"
              startIcon={<OpenInFullRoundedIcon sx={{ fontSize: 14 }} />}
              sx={{ fontFamily: t.mono, fontSize: 11, fontWeight: 700, textTransform: 'none' }}
              variant="text"
              onClick={onFocus}
            >
              Focus
            </Button>
          ) : null}
        </Box>
      </Box>
    </ArtifactFrame>
  );
}

/** The collapsed REFERENCE card: "<line> → Open". Never labelled as this task's product. */
export function ContractReferenceLine({
  line,
  source,
  onOpen,
  onFocus,
}: {
  line: string;
  source: string;
  onOpen: () => void;
  onFocus?: (() => void) | undefined;
}): ReactElement {
  const t = useTokens();
  return (
    <ArtifactFrame
      artifactRole="reference"
      kindTestId={UI_IDENTIFIERS.Construction.CONTRACT_REFERENCE}
      source={source}
      title="SERVICE CONTRACT"
      onFocus={onFocus}
    >
      <Typography
        data-testid={UI_IDENTIFIERS.Construction.CONTRACT_REFERENCE}
        sx={{ fontFamily: t.body, fontSize: 12.5, color: t.ink }}
      >
        {`${line} → `}
        <Link
          component="button"
          data-testid={UI_IDENTIFIERS.Construction.CONTRACT_REFERENCE_OPEN}
          sx={{ fontFamily: t.mono, fontSize: 12, fontWeight: 700, color: t.accent2 }}
          underline="hover"
          onClick={onOpen}
        >
          Open
        </Link>
      </Typography>
    </ArtifactFrame>
  );
}
