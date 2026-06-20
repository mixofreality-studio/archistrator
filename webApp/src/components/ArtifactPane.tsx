/**
 * The home base main pane: renders one selected artifact slot as a living
 * document. All non-empty slots — prose AND the three diagram kinds (volatilities
 * / coreUseCases / system) — render via the shared ArtifactRenderer dispatcher,
 * so the home base shows the real interactive scatter / C4 / activity views for
 * committed artifacts (no more "open in System Design" hand-off). Empty slots show
 * a "not yet drafted" placeholder.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import LockOutlinedIcon from '@mui/icons-material/LockOutlined';
import type { ArtifactMeta } from '../api/adapters';
import type { ArtifactModelEnvelope, ServiceContracts } from '../api/types';
import { ArtifactRenderer } from './ArtifactRenderer';
import { useTokens } from '../theme/ThemeContext';

export function ArtifactPane({
  artifact,
  envelope,
  serviceContracts,
}: {
  artifact: ArtifactMeta;
  envelope: ArtifactModelEnvelope | undefined;
  /** When present, the Architecture section drills into established contracts. */
  serviceContracts?: ServiceContracts | undefined;
}): ReactNode {
  const t = useTokens();

  if (artifact.stage === 'empty') {
    return (
      <Box sx={{ py: 6, textAlign: 'center', color: t.muted }}>
        <LockOutlinedIcon sx={{ fontSize: 28, opacity: 0.4 }} />
        <Typography sx={{ fontFamily: t.mono, mt: 1 }}>Not yet drafted.</Typography>
        <Typography variant="caption">{artifact.blurb}</Typography>
      </Box>
    );
  }

  return (
    <ArtifactRenderer
      envelope={envelope}
      serviceContracts={serviceContracts}
      title={artifact.title}
    />
  );
}
