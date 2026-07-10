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
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';
import ToggleButton from '@mui/material/ToggleButton';
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup';
import { useParams } from '@tanstack/react-router';
import {
  listDeploymentProfiles,
  toMissionView,
  toOperationalDecisionsView,
} from '../contracts/adapters';
import type { ArtifactModelEnvelope, DeploymentProfile } from '../contracts/types';
import { useProject } from '../hooks/useProject';
import { useTokens } from '../utilities/theme/ThemeContext';
import { CommentableList } from './comments/CommentableList';
import { operationalDecisionAnchor } from './comments/CommentContext';
import { DeploymentFlow } from './flow/DeploymentFlow';
import { UI_IDENTIFIERS } from '../utilities/constants/UIIdentifiers';

const PROFILE_LABEL: Record<DeploymentProfile, string> = {
  cloud: 'Cloud',
  local: 'Local',
  test: 'Test',
};

/** Truncation length for the inline objective statement (full text lives in the tooltip). */
const OBJECTIVE_CLAMP = 90;

/**
 * "justifies objective 2 — <statement>" for a decision. The bare number is
 * meaningless on its own, so the joined objective statement is shown inline
 * (truncated) with the full text on hover. When the mission join can't resolve the
 * number (mission not committed / out of range) it degrades to just the number.
 */
function ObjectiveJustification({
  number,
  statement,
  t,
}: {
  number: number;
  statement: string | undefined;
  t: ReturnType<typeof useTokens>;
}): ReactNode {
  const label = (
    <Typography sx={{ color: t.muted, fontFamily: t.mono, fontSize: 11, mt: 0.25 }}>
      justifies objective {number}
      {statement !== undefined && statement.length > 0 ? (
        <>
          {' — '}
          <Box component="span" sx={{ fontStyle: 'italic', color: t.ink, opacity: 0.85 }}>
            {statement.length > OBJECTIVE_CLAMP
              ? `${statement.slice(0, OBJECTIVE_CLAMP).trimEnd()}…`
              : statement}
          </Box>
        </>
      ) : null}
    </Typography>
  );
  if (statement === undefined || statement.length <= OBJECTIVE_CLAMP) return label;
  return (
    <Tooltip arrow placement="top-start" title={statement}>
      {label}
    </Tooltip>
  );
}

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

  // The committed Mission, joined so each decision can name the business objective
  // it justifies (a bare "objective 2" is meaningless without the statement).
  const objectiveByNumber = useMemo(() => {
    const missionEnvelope = project?.slots.find((s) => s.kind === 'mission')?.model;
    const m = new Map<number, string>();
    for (const o of toMissionView(missionEnvelope).objectives ?? []) m.set(o.number, o.statement);
    return m;
  }, [project]);

  const profiles = useMemo(() => listDeploymentProfiles(envelope), [envelope]);
  const [profile, setProfile] = useState<DeploymentProfile | undefined>(undefined);
  const activeProfile =
    profile !== undefined && profiles.some((p) => p.profile === profile)
      ? profile
      : profiles[0]?.profile;

  const decisions = toOperationalDecisionsView(envelope);

  return (
    <Box>
      {decisions.length === 0 ? (
        <Typography sx={{ fontFamily: t.mono, fontSize: 12.5, color: t.muted }}>
          No operational decisions drafted yet.
        </Typography>
      ) : (
        <CommentableList
          ariaLabel="Operational decisions"
          getAnchor={(d, i) => ({
            kind: 'node',
            label: d.topic,
            source: `Operational Concepts · ${d.topic}`,
            jsonPath: operationalDecisionAnchor(i),
          })}
          getKey={(d, i) => `${d.topic}-${String(i)}`}
          getLabel={(d) => `decision: ${d.topic}`}
          items={decisions}
          renderItem={(d) => (
            <Box>
              <Typography
                component="span"
                sx={{ fontFamily: t.mono, fontSize: 12.5, fontWeight: 700, color: t.ink }}
              >
                {d.topic}
              </Typography>
              <Typography
                sx={{
                  color: t.ink,
                  fontFamily: t.body,
                  fontSize: '0.92rem',
                  lineHeight: 1.55,
                  mt: 0.25,
                }}
              >
                {d.decision}
              </Typography>
              <ObjectiveJustification
                number={d.justifyingObjective}
                statement={objectiveByNumber.get(d.justifyingObjective)}
                t={t}
              />
            </Box>
          )}
        />
      )}

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
