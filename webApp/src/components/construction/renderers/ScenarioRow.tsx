import { type ReactNode, useState } from 'react';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Chip from '@mui/material/Chip';
import Tab from '@mui/material/Tab';
import Tabs from '@mui/material/Tabs';
import Typography from '@mui/material/Typography';
import OpenInNewIcon from '@mui/icons-material/OpenInNew';
import { UI_IDENTIFIERS } from '../../../utilities/constants/UIIdentifiers';
import type { TestRunScenarioView } from '../../../contracts/types';
import type { Tokens } from '../../../utilities/theme/themes';
import type { AsyncArtifactData } from '../artifactRenderers';
import { goTestOutputFor, statusTone, tabsFor, type ScenarioTab } from './testRunPanelLogic.ts';

/** The self-hosted Playwright trace viewer (copied into public/ by postinstall). */
const TRACE_VIEWER = '/trace-viewer/index.html';

/** The trace viewer runs in its own tab and fetches the trace itself, so it is
 *  handed an ABSOLUTE url: the file endpoint's path resolved against this origin. */
function traceViewerUrl(fileUrl: string): string {
  const absolute = new URL(fileUrl, window.location.origin).toString();
  return `${TRACE_VIEWER}?trace=${encodeURIComponent(absolute)}`;
}

export function StatusChip({ status, t }: { status: string; t: Tokens }): ReactNode {
  const tone = statusTone(status);
  const good = tone === 'good';
  const bad = tone === 'bad';
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

export function OutputTab({
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

export function ScenarioDetail({
  scenario,
  fileUrl,
  outputs,
  t,
}: {
  scenario: TestRunScenarioView;
  fileUrl: (path: string) => string;
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
            href={traceViewerUrl(fileUrl(scenario.traceRef))}
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
          src={fileUrl(scenario.videoRef)}
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
