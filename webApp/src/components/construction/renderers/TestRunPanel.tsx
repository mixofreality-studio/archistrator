import { type ReactNode, useState } from 'react';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import ButtonBase from '@mui/material/ButtonBase';
import Chip from '@mui/material/Chip';
import Paper from '@mui/material/Paper';
import Tab from '@mui/material/Tab';
import Tabs from '@mui/material/Tabs';
import Typography from '@mui/material/Typography';
import OpenInNewIcon from '@mui/icons-material/OpenInNew';
import { UI_IDENTIFIERS } from '../../../utilities/constants/UIIdentifiers';
import type { TestRunPanelView, TestRunScenarioView } from '../../../contracts/types';
import type { Tokens } from '../../../utilities/theme/themes';
import { CommentableList } from '../../comments/CommentableList';
import { testRunScenarioAnchor } from '../../comments/CommentContext';
import type { ArtifactRendererProps, AsyncArtifactData } from '../artifactRenderers';
import { StatTile } from '../primitives/StatTile';
import {
  goTestOutputFor,
  isNoRecordedRun,
  summarize,
  tabsFor,
  venueRunUrl,
  type ScenarioTab,
} from './testRunPanelLogic.ts';

/** The self-hosted Playwright trace viewer (copied into public/ by postinstall). */
const TRACE_VIEWER = '/trace-viewer/index.html';

/** The trace viewer runs in its own tab and fetches the trace itself, so it is
 *  handed an ABSOLUTE url: the file endpoint's path resolved against this origin. */
function traceViewerUrl(fileUrl: string): string {
  const absolute = new URL(fileUrl, window.location.origin).toString();
  return `${TRACE_VIEWER}?trace=${encodeURIComponent(absolute)}`;
}

function statusChip(status: string, t: Tokens): ReactNode {
  const good = status === 'pass';
  const bad = status === 'fail';
  return (
    <Chip
      data-scenario-status={status}
      label={status}
      size="small"
      sx={{
        height: 18,
        fontSize: 8.5,
        fontFamily: t.mono,
        bgcolor: good ? t.committedBg : bad ? t.paperAlt : t.awaitingBg,
        color: good ? t.committedFg : bad ? t.dangerFg : t.awaitingFg,
        border: `1px solid ${good ? t.committedDot : bad ? t.dangerFg : t.line}`,
      }}
    />
  );
}

function OutputTab({
  scenario,
  stream,
  t,
}: {
  scenario: TestRunScenarioView;
  stream: AsyncArtifactData<string> | undefined;
  t: Tokens;
}): ReactNode {
  if (scenario.outputRef.length === 0) {
    return (
      <Typography
        data-testid={UI_IDENTIFIERS.Construction.TEST_RUN_OUTPUT}
        sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}
      >
        No output stream recorded for this scenario.
      </Typography>
    );
  }
  if (stream === undefined || stream.isLoading) {
    return (
      <Typography
        data-testid={UI_IDENTIFIERS.Construction.TEST_RUN_OUTPUT}
        sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}
      >
        Loading output…
      </Typography>
    );
  }
  if (stream.error !== null) {
    return (
      <Typography
        data-testid={UI_IDENTIFIERS.Construction.TEST_RUN_OUTPUT}
        sx={{ fontFamily: t.mono, fontSize: 12, color: t.dangerFg }}
      >
        {`Could not read ${scenario.outputRef}: ${stream.error.message}`}
      </Typography>
    );
  }
  const text = goTestOutputFor(stream.data ?? '', scenario.id);
  return (
    <Box
      component="pre"
      data-testid={UI_IDENTIFIERS.Construction.TEST_RUN_OUTPUT}
      sx={{
        m: 0,
        p: 1.25,
        maxHeight: 360,
        overflow: 'auto',
        whiteSpace: 'pre-wrap',
        wordBreak: 'break-word',
        fontFamily: t.mono,
        fontSize: 11.5,
        lineHeight: 1.45,
        color: t.ink,
        bgcolor: t.paperAlt,
        border: `1px solid ${t.line}`,
        borderRadius: 1,
      }}
    >
      {text.length > 0 ? text : `(the stream has no output for ${scenario.id})`}
    </Box>
  );
}

function ScenarioDetail({
  scenario,
  panel,
  outputs,
  t,
}: {
  scenario: TestRunScenarioView;
  panel: TestRunPanelView;
  outputs: Record<string, AsyncArtifactData<string>>;
  t: Tokens;
}): ReactNode {
  const tabs = tabsFor(scenario);
  const [tab, setTab] = useState<ScenarioTab>('Output');
  const active: ScenarioTab = tabs.includes(tab) ? tab : 'Output';
  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1, minWidth: 0 }}>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
        <Tabs
          aria-label={`${scenario.id} detail`}
          sx={{ minHeight: 32 }}
          value={active}
          onChange={(_e, v: ScenarioTab) => {
            setTab(v);
          }}
        >
          {tabs.map((name) => (
            <Tab
              key={name}
              label={name}
              sx={{ minHeight: 32, py: 0.5, fontFamily: t.mono, fontSize: 11 }}
              value={name}
            />
          ))}
        </Tabs>
        <Box sx={{ flexGrow: 1 }} />
        {scenario.traceRef.length > 0 ? (
          <Button
            component="a"
            data-testid={UI_IDENTIFIERS.Construction.TEST_RUN_OPEN_TRACE}
            endIcon={<OpenInNewIcon sx={{ fontSize: 14 }} />}
            href={traceViewerUrl(panel.fileUrl(scenario.traceRef))}
            rel="noreferrer"
            size="small"
            sx={{ py: 0.25, fontFamily: t.mono, fontSize: 11, textTransform: 'none' }}
            target="_blank"
          >
            Open trace
          </Button>
        ) : null}
      </Box>
      {active === 'Video' ? (
        // Only ever mounted over a videoRef (tabsFor): a `<video>` with an
        // empty src is a broken player (Review Focus 4).
        <Box
          controls
          component="video"
          data-testid={UI_IDENTIFIERS.Construction.TEST_RUN_VIDEO}
          preload="metadata"
          src={panel.fileUrl(scenario.videoRef)}
          sx={{
            width: '100%',
            maxHeight: 420,
            bgcolor: '#000',
            borderRadius: 1,
            border: `1px solid ${t.line}`,
          }}
        />
      ) : (
        <OutputTab scenario={scenario} stream={outputs[scenario.outputRef]} t={t} />
      )}
    </Box>
  );
}

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
 */
export function TestRunPanel({ vm, project, t, testing }: ArtifactRendererProps): ReactNode {
  const [selectedId, setSelectedId] = useState<string>('');
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
  const panel = q.data;
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
          {`run ${panel.run.id} · ${panel.artifact} · revision ${panel.run.revision}`}
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
              {panel.artifact}
            </Typography>
          )}
        </Box>
      </Box>

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
          jsonPath: testRunScenarioAnchor(panel.run.id, s.id),
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
                gap: 1,
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
              {statusChip(s.status, t)}
              <Typography sx={{ fontFamily: t.mono, fontSize: 11.5, color: t.ink, flexShrink: 0 }}>
                {s.id}
              </Typography>
              <Typography
                sx={{ fontFamily: t.body, fontSize: 12.5, color: t.ink, minWidth: 0, flexGrow: 1 }}
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
          key={selected.id}
          outputs={outputs}
          panel={panel}
          scenario={selected}
          t={t}
        />
      ) : null}
    </Box>
  );
}
