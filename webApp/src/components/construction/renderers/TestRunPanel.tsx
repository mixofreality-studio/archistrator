import { type ReactNode, useState } from 'react';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import ButtonBase from '@mui/material/ButtonBase';
import Chip from '@mui/material/Chip';
import MenuItem from '@mui/material/MenuItem';
import Paper from '@mui/material/Paper';
import Select from '@mui/material/Select';
import Typography from '@mui/material/Typography';
import OpenInNewIcon from '@mui/icons-material/OpenInNew';
import { UI_IDENTIFIERS } from '../../../utilities/constants/UIIdentifiers';
import { CommentableList } from '../../comments/CommentableList';
import { testRunScenarioAnchor } from '../../comments/CommentContext';
import type { ArtifactRendererProps } from '../artifactRenderers';
import { StatTile } from '../primitives/StatTile';
import { ScenarioDetail, StatusChip } from './ScenarioRow.tsx';
import { artifactLabel, isNoRecordedRun, summarize, venueRunUrl } from './testRunPanelLogic.ts';

/**
 * The testing task's results panel (spec §6.4): the recorded run, every
 * projected scenario joined with its verdict, and the venue's evidence for the
 * one selected — a server scenario's stdout/stderr out of the `go test -json`
 * stream; a UI scenario's video, and its Playwright trace in the self-hosted
 * viewer. A skip is counted apart from a pass, because the gate counts it
 * (§3.1); a projected scenario the run never reported is named, because the
 * gate holds on it (§6.5).
 *
 * Read through `testing.testRun` / `testing.outputs` (the `testRun` project
 * view and the file endpoint), fetched by the container — never from the row.
 *
 * The gate judges EVERY contract the activity's component owns (R36), each its
 * own member of `testRuns`. With more than one, a dropdown chooses which
 * contract's verdicts are on screen (a contract count is dynamic, so a
 * dropdown, never tabs); the primary is shown first.
 */
export function TestRunPanel({ vm, project, t, testing }: ArtifactRendererProps): ReactNode {
  const [selectedId, setSelectedId] = useState<string>('');
  const [contract, setContract] = useState<string>('');
  const q = testing?.testRun;
  if (q === undefined || q.isLoading) {
    return (
      <Typography
        data-testid={UI_IDENTIFIERS.Construction.TEST_RUN_PANEL}
        sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}
      >
        Reading the test run…
      </Typography>
    );
  }
  if (q.error !== null || q.data === undefined) {
    return (
      <Typography
        data-testid={UI_IDENTIFIERS.Construction.TEST_RUN_PANEL}
        sx={{ color: t.muted, fontSize: 12.5, lineHeight: 1.5 }}
      >
        {q.error !== null && !isNoRecordedRun(q.error)
          ? `No test run to show: ${q.error.message}`
          : 'No test run recorded for this activity yet. The venue records one when the integration task runs the generated scenarios.'}
      </Typography>
    );
  }
  const members = q.data.testRuns;
  const panel = members.find((m) => m.run.component === contract) ?? q.data;
  if (panel.expired) {
    return (
      <Paper
        data-testid={UI_IDENTIFIERS.Construction.TEST_RUN_EXPIRED}
        sx={{ p: 3, border: `1px solid ${t.line}` }}
      >
        <Typography sx={{ fontSize: 13.5, color: t.ink, lineHeight: 1.5 }}>
          Artifact expired; re-run integration.
        </Typography>
        <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted, mt: 0.5 }}>
          {`run ${panel.run.id} · ${artifactLabel(panel.artifact)} · revision ${panel.run.revision}`}
        </Typography>
      </Paper>
    );
  }

  const counts = summarize(panel.scenarios);
  const venue = venueRunUrl(project?.gitRows?.[vm.activityId]?.prUrl, panel.run.id);
  const selected =
    panel.scenarios.find((s) => s.id === selectedId) ?? panel.scenarios[0] ?? undefined;
  const outputs = testing?.outputs ?? {};

  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.TEST_RUN_PANEL}
      sx={{ display: 'flex', flexDirection: 'column', gap: 1.5, minWidth: 0 }}
    >
      {/* header: the three tiles, the run's identity, the venue link */}
      <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap', alignItems: 'stretch' }}>
        <StatTile
          label="pass"
          t={t}
          tone={counts.pass > 0 ? 'good' : 'neutral'}
          value={counts.pass}
        />
        <StatTile
          label="fail"
          t={t}
          tone={counts.fail > 0 ? 'bad' : 'neutral'}
          value={counts.fail}
        />
        <StatTile
          label="skip"
          t={t}
          tone={counts.skip > 0 ? 'bad' : 'neutral'}
          value={counts.skip}
        />
        {counts.timeout > 0 ? (
          <StatTile label="timeout" t={t} tone="danger" value={counts.timeout} />
        ) : null}
        <Box
          sx={{ display: 'flex', flexDirection: 'column', justifyContent: 'center', minWidth: 0 }}
        >
          <Typography
            sx={{
              fontFamily: t.mono,
              fontWeight: 700,
              fontSize: 11,
              letterSpacing: '0.06em',
              color: t.ink,
            }}
          >
            {`TEST RUN · ${panel.run.id}`}
          </Typography>
          <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}>
            {`revision ${panel.run.revision} · ${panel.run.component}`}
          </Typography>
          {venue !== undefined ? (
            <Button
              component="a"
              data-testid={UI_IDENTIFIERS.Construction.TEST_RUN_VENUE_LINK}
              endIcon={<OpenInNewIcon sx={{ fontSize: 14 }} />}
              href={venue}
              rel="noreferrer"
              size="small"
              sx={{
                alignSelf: 'flex-start',
                px: 0,
                py: 0.25,
                fontFamily: t.mono,
                fontSize: 11,
                textTransform: 'none',
                color: t.accent,
              }}
              target="_blank"
            >
              Open in venue
            </Button>
          ) : (
            <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}>
              {artifactLabel(panel.artifact)}
            </Typography>
          )}
        </Box>
      </Box>

      {members.length > 1 ? (
        <Select
          inputProps={{ 'aria-label': 'Contract' }}
          renderValue={(c) => `Contract ${c}`}
          size="small"
          sx={{ alignSelf: 'flex-start', fontFamily: t.mono, fontWeight: 700, fontSize: 12 }}
          value={panel.run.component}
          onChange={(e) => {
            setContract(e.target.value);
            setSelectedId('');
          }}
        >
          {members.map((m) => {
            const c = summarize(m.scenarios);
            return (
              <MenuItem
                key={m.run.component}
                sx={{ fontFamily: t.mono, fontSize: 12 }}
                value={m.run.component}
              >
                {`${m.run.component} · ${String(c.pass)} pass · ${String(c.fail)} fail${
                  c.timeout > 0 ? ` · ${String(c.timeout)} timed out` : ''
                }`}
              </MenuItem>
            );
          })}
        </Select>
      ) : null}

      {panel.missing.length > 0 ? (
        <Typography
          data-testid={UI_IDENTIFIERS.Construction.TEST_RUN_MISSING}
          sx={{ fontFamily: t.mono, fontSize: 11, color: t.dangerFg }}
        >
          {`Not run (the gate holds on these): ${panel.missing.join(', ')}`}
        </Typography>
      ) : null}
      {panel.unknown.length > 0 ? (
        <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: t.awaitingFg }}>
          {`Reported but not projected: ${panel.unknown.join(', ')}`}
        </Typography>
      ) : null}

      <CommentableList
        ariaLabel="Test run scenarios"
        gap={0.25}
        getAnchor={(s) => ({
          kind: 'node',
          label: `scenario:${s.id}`,
          source: `Test run ${panel.run.id} · ${s.id}`,
          jsonPath: testRunScenarioAnchor(panel.run.id, panel.run.component, s.id),
        })}
        getKey={(s) => s.id}
        getLabel={(s) => `scenario ${s.id} ${s.status}`}
        items={panel.scenarios}
        renderItem={(s) => {
          const on = selected?.id === s.id;
          return (
            <ButtonBase
              aria-pressed={on}
              sx={{
                display: 'flex',
                width: '100%',
                alignItems: 'center',
                // A long id or title, or several guard chips, wrap the trailing chips
                // and duration onto a second line rather than squeezing the title to a
                // word per line (R60).
                flexWrap: 'wrap',
                columnGap: 1,
                rowGap: 0.25,
                px: 0.75,
                py: 0.5,
                textAlign: 'left',
                borderRadius: 1,
                bgcolor: on ? t.paperAlt : 'transparent',
                borderLeft: `3px solid ${on ? t.accent : 'transparent'}`,
              }}
              onClick={() => {
                setSelectedId(s.id);
              }}
            >
              <StatusChip status={s.status} t={t} />
              <Typography sx={{ fontFamily: t.mono, fontSize: 11.5, color: t.ink, flexShrink: 0 }}>
                {s.id}
              </Typography>
              <Typography
                sx={{
                  fontFamily: t.body,
                  fontSize: 12.5,
                  color: t.ink,
                  // At least 12rem, one line; a title longer than the row is cut with an
                  // ellipsis and reads whole in its tooltip (R60).
                  flex: '1 1 12rem',
                  minWidth: '12rem',
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                  whiteSpace: 'nowrap',
                }}
                title={s.title}
              >
                {s.title}
              </Typography>
              {s.guards.map((g) => (
                <Chip
                  key={`${g.at}:${g.guard}`}
                  label={g.guard}
                  size="small"
                  sx={{
                    height: 18,
                    flexShrink: 0,
                    fontFamily: t.mono,
                    fontSize: 9,
                    bgcolor: t.paperAlt,
                    color: t.muted,
                    border: `1px solid ${t.line}`,
                  }}
                />
              ))}
              <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted, flexShrink: 0 }}>
                {`${String(s.durationMs)} ms`}
              </Typography>
            </ButtonBase>
          );
        }}
      />

      {selected !== undefined ? (
        <ScenarioDetail
          fileUrl={panel.fileUrl}
          key={selected.id}
          outputs={outputs}
          scenario={selected}
          t={t}
          onWantOutput={testing?.wantOutput}
        />
      ) : null}
    </Box>
  );
}
