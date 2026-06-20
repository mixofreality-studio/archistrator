/**
 * The OperationalConcepts artifact viewer: the runtime-interaction decisions list
 * (rendered as prose, exactly as before) followed by a Deployment section that
 * shows the typed deployment topology per profile.
 *
 * A ToggleButtonGroup switches between only the profiles actually present in
 * deployment.environments, feeding DeploymentFlow. Instances are coloured by their
 * System component's Method layer, so the section joins against the committed
 * System artifact pulled from the project head-state (via the route's projectId).
 * When deployment is absent/empty the section renders a subtle note.
 */
import { useMemo, useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import ToggleButton from '@mui/material/ToggleButton';
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup';
import { useParams } from '@tanstack/react-router';
import { listDeploymentProfiles, toMarkdown } from '../api/adapters';
import type { ArtifactModelEnvelope } from '../api/types';
import type { components } from '../api/schema';
import { useProject } from '../hooks/useProject';
import { useTokens } from '../theme/ThemeContext';
import { Prose } from './Prose';
import { DeploymentFlow } from './flow/DeploymentFlow';
import { UI_IDENTIFIERS } from '../constants/UIIdentifiers';

type DeploymentProfile = components['schemas']['DeploymentProfile'];

const PROFILE_LABEL: Record<DeploymentProfile, string> = {
  cloud: 'Cloud',
  local: 'Local',
  test: 'Test',
};

export function OperationalConceptsView({
  envelope,
  height = 520,
}: {
  envelope: ArtifactModelEnvelope | undefined;
  height?: number;
}): ReactNode {
  const t = useTokens();
  const params = useParams({ strict: false });
  const projectId = typeof params.projectId === 'string' ? params.projectId : '';
  const { data: project } = useProject(projectId);

  // The committed System artifact, joined for instance layer/name colouring.
  const systemEnvelope = useMemo(
    () => project?.slots.find((s) => s.kind === 'system')?.model,
    [project]
  );

  const profiles = useMemo(() => listDeploymentProfiles(envelope), [envelope]);
  const [profile, setProfile] = useState<DeploymentProfile | undefined>(undefined);
  const activeProfile =
    profile !== undefined && profiles.some((p) => p.profile === profile)
      ? profile
      : profiles[0]?.profile;

  const markdown = toMarkdown(envelope);

  return (
    <Box>
      <Prose
        artifactKind="operationalConcepts"
        markdown={markdown.length > 0 ? markdown : '_No content yet._'}
        source="Operational Concepts"
      />

      <Box sx={{ mt: 4 }}>
        <Typography
          sx={{
            fontFamily: t.mono,
            fontWeight: 700,
            fontSize: '0.78rem',
            letterSpacing: '0.16em',
            textTransform: 'uppercase',
            color: t.muted,
            mb: 1.5,
          }}
        >
          Deployment
        </Typography>

        {profiles.length === 0 ? (
          <Typography sx={{ fontFamily: t.mono, fontSize: 12.5, color: t.muted }}>
            No deployment topology.
          </Typography>
        ) : (
          <>
            <ToggleButtonGroup
              exclusive
              color="primary"
              data-testid={UI_IDENTIFIERS.Deployment.PROFILE_SWITCH}
              size="small"
              sx={{ mb: 1.5 }}
              value={activeProfile ?? ''}
              onChange={(_e, next: DeploymentProfile | null) => {
                if (next !== null) setProfile(next);
              }}
            >
              {profiles.map((p) => (
                <ToggleButton key={p.profile} sx={{ fontFamily: t.mono }} value={p.profile}>
                  {p.title.length > 0 ? p.title : PROFILE_LABEL[p.profile]}
                </ToggleButton>
              ))}
            </ToggleButtonGroup>

            {activeProfile !== undefined && (
              <DeploymentFlow
                height={height}
                opEnvelope={envelope}
                profile={activeProfile}
                systemEnvelope={systemEnvelope}
              />
            )}
          </>
        )}
      </Box>
    </Box>
  );
}
