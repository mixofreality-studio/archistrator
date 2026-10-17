import { type ReactNode, useEffect, useState } from 'react';
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
import { LogLines } from '../primitives/LogLines';
import { goTestOutputFor, statusTone, tabsFor, type ScenarioTab } from './testRunPanelLogic.ts';

/** The self-hosted Playwright trace viewer (copied into public/ by postinstall). */
const TRACE_VIEWER = '/trace-viewer/index.html';

/** The trace viewer runs in its own tab and fetches the trace itself, so it is
 *  handed an ABSOLUTE url: the file endpoint's path resolved against this origin. */
function traceViewerUrl(fileUrl: string): string {
  const absolute = new URL(fileUrl, window.location.origin).toString();
  return `${TRACE_VIEWER}?trace=${encodeURIComponent(absolute)}`;
}

/** `label` overrides the text where the status word is not what a reader says
 *  (the task spec panel's `not-run` reads "not run"); the tone is the status'. */
export function StatusChip({
  status,
  t,
  label,
}: {
  status: string;
  t: Tokens;
  label?: string;
}): ReactNode {
  const tone = statusTone(status);
  const good = tone === 'good';
  const bad = tone === 'bad';
  return (
    <Chip
      data-scenario-status={status}
      label={label ?? status}
      size="small"
      sx={{
        height: 22,
        fontSize: 11,
        fontFamily: t.body,
        fontWeight: 700,
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
        sx={{ fontFamily: t.body, fontSize: 12.5, color: t.muted }}
      >
        No output stream recorded for this scenario.
      </Typography>
    );
  }
  if (stream === undefined || stream.isLoading) {
    return (
      <Typography
        data-testid={UI_IDENTIFIERS.Construction.TEST_RUN_OUTPUT}
        sx={{ fontFamily: t.body, fontSize: 12.5, color: t.muted }}
      >
        Loading output…
      </Typography>
    );
  }
  if (stream.error !== null) {
    return (
      <Typography
        data-testid={UI_IDENTIFIERS.Construction.TEST_RUN_OUTPUT}
        sx={{ fontFamily: t.body, fontSize: 12.5, color: t.dangerFg }}
      >
        {`Could not read ${scenario.outputRef}: ${stream.error.message}`}
      </Typography>
    );
  }
  const text = goTestOutputFor(stream.data ?? '', scenario.id);
  if (text.trim().length === 0) {
    return (
      <Typography
        data-testid={UI_IDENTIFIERS.Construction.TEST_RUN_OUTPUT}
        sx={{ fontFamily: t.body, fontSize: 12.5, color: t.muted }}
      >
        The run recorded no output for this scenario.
      </Typography>
    );
  }
  return (
    <LogLines
      label={`Test output of ${scenario.title.length > 0 ? scenario.title : scenario.id}`}
      t={t}
      testId={UI_IDENTIFIERS.Construction.TEST_RUN_OUTPUT}
      text={text}
    />
  );
}

export function ScenarioDetail({
  scenario,
  fileUrl,
  outputs,
  t,
  onWantOutput,
}: {
  scenario: TestRunScenarioView;
  fileUrl: (path: string) => string;
  outputs: Record<string, AsyncArtifactData<string>>;
  t: Tokens;
  /** Asks the container to fetch a stream: called once the Output tab is open
   *  over one, so a stream nobody reads is never downloaded (R48). */
  onWantOutput?: ((ref: string) => void) | undefined;
}): ReactNode {
  const tabs = tabsFor(scenario);
  const [tab, setTab] = useState<ScenarioTab>('Output');
  const active: ScenarioTab = tabs.includes(tab) ? tab : 'Output';
  const ref = scenario.outputRef;
  useEffect(() => {
    if (active === 'Output' && ref.length > 0) onWantOutput?.(ref);
  }, [active, ref, onWantOutput]);
  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1, minWidth: 0 }}>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
        {tabs.length === 1 ? (
          // One view needs no tab strip: a tab bar over a single tab is chrome
          // that promises a choice there is not.
          <Typography sx={{ fontFamily: t.body, fontSize: 12.5, fontWeight: 700, color: t.ink }}>
            Test output
          </Typography>
        ) : (
          <Tabs
            aria-label={`${scenario.title.length > 0 ? scenario.title : scenario.id}: evidence`}
            sx={{ minHeight: 40 }}
            value={active}
            onChange={(_e, v: ScenarioTab) => {
              setTab(v);
            }}
          >
            {tabs.map((name) => (
              <Tab
                key={name}
                label={name}
                sx={{
                  minHeight: 40,
                  py: 0.5,
                  fontFamily: t.body,
                  fontSize: 12.5,
                  textTransform: 'none',
                }}
                value={name}
              />
            ))}
          </Tabs>
        )}
        <Box sx={{ flexGrow: 1 }} />
        {scenario.traceRef.length > 0 ? (
          <Button
            component="a"
            data-testid={UI_IDENTIFIERS.Construction.TEST_RUN_OPEN_TRACE}
            endIcon={<OpenInNewIcon sx={{ fontSize: 14 }} />}
            href={traceViewerUrl(fileUrl(scenario.traceRef))}
            rel="noreferrer"
            size="small"
            sx={{ minHeight: 32, fontSize: 12, textTransform: 'none' }}
            target="_blank"
          >
            Open the step-by-step trace
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
