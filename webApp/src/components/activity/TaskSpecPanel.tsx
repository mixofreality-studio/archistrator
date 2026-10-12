/**
 * THE TASK SPEC PANEL — what an agentic task is, before and after it runs: who
 * does it (role), with what (command, model, estimated and actual cost), told
 * what (the prompt, the charter, the skills), allowed to call what (tools),
 * allowed to write where (edit scope), and judged by what (checks, with each
 * one's verdict once a run exists). Agent task spec §5.
 *
 * Every value is the server's computed `TaskSpecView` (projectstate.TaskSpecFor)
 * — the same resolution the job seats — so nothing here is joined in from a
 * client-side table. A task that never ran has a spec and no run: every check
 * reads "not run" (R35), and nothing reads as an error. A human review has no
 * command: it shows its role (when it has one) and its exit criterion, and the
 * agent sections are left out rather than drawn empty.
 *
 * Loading and a failed read are one muted line each inside the frame; neither
 * holds back the episode timeline below.
 *
 * Pure and props-only: the container owns the spec read and the log reads.
 */
import { type ReactNode, useEffect, useId, useState } from 'react';
import Box from '@mui/material/Box';
import ButtonBase from '@mui/material/ButtonBase';
import Chip from '@mui/material/Chip';
import Collapse from '@mui/material/Collapse';
import Skeleton from '@mui/material/Skeleton';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';

import { RoleAvatar } from '../RoleAvatar';
import { CommentableList } from '../comments/CommentableList';
import { taskSpecCheckAnchor } from '../comments/CommentContext';
import type { AsyncArtifactData } from '../construction/artifactRenderers';
import { ScenarioDetail, StatusChip } from '../construction/renderers/ScenarioRow.tsx';
import {
  ROLE_LABEL,
  SPEC_CHANGED,
  SPEC_LOADING,
  SPEC_NO_CHECKS,
  SPEC_NO_OUTPUT,
  SPEC_NOT_RUN,
  SPEC_OUTPUT_LOADING,
  SPEC_STATE_ONLY,
  specOutputFailed,
  specReadFailed,
} from './activityCopy.ts';
import {
  checkCounts,
  checkRows,
  countsLine,
  formatDuration,
  formatTokens,
  formatUsd,
  groupByKind,
  KIND_LABEL,
  modelSourceTip,
  scenarioViewOf,
  sourceLabel,
  specSections,
  type CheckRowState,
} from './taskSpecPanelLogic.ts';
import type { TaskSpecView } from '../../contracts/types';
import type { Tokens } from '../../utilities/theme/themes';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';

export interface TaskSpecPanelProps {
  spec: AsyncArtifactData<TaskSpecView>;
  /** The selected revision's episode cost, when an episode was captured. */
  episodeCostUsd?: number | undefined;
  activityId: string;
  /** The run's check logs by `outputRef`, fetched by the container. */
  outputs: Record<string, AsyncArtifactData<string>>;
  /** The file endpoint's URL for one file of the run (video, trace, log). */
  fileUrl: (path: string) => string;
  /** Asks the container for one log: called when a row expands over it (R48). */
  onWantOutput: (ref: string) => void;
}

/** What the panel needs beside the spec to show a run's evidence: the bodies carry it through. */
export type TaskSpecCheckEvidence = Pick<
  TaskSpecPanelProps,
  'activityId' | 'outputs' | 'fileUrl' | 'onWantOutput'
>;

function Label({ children, t }: { children: ReactNode; t: Tokens }): ReactNode {
  return (
    <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, letterSpacing: '0.14em', color: t.muted }}>
      {children}
    </Typography>
  );
}

function Fact({ label, children }: { label: string; children: ReactNode }): ReactNode {
  const t = useTokens();
  return (
    <Box sx={{ minWidth: 0 }}>
      <Label t={t}>{label}</Label>
      {typeof children === 'string' ? (
        <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 13, color: t.ink }}>
          {children}
        </Typography>
      ) : (
        children
      )}
    </Box>
  );
}

/** The small mono chip TestRunPanel draws a guard with. */
function MonoChip({
  label,
  t,
  onClick,
  pressed,
}: {
  label: string;
  t: Tokens;
  onClick?: () => void;
  /** A clickable chip DISCLOSES a list, so it states `aria-expanded`. */
  pressed?: boolean;
}): ReactNode {
  return (
    <Chip
      aria-expanded={onClick !== undefined ? pressed === true : undefined}
      label={label}
      size="small"
      sx={{
        height: 18,
        fontFamily: t.mono,
        fontSize: 9,
        bgcolor: pressed === true ? t.paper : t.paperAlt,
        color: pressed === true ? t.ink : t.muted,
        border: `1px solid ${pressed === true ? t.accent : t.line}`,
      }}
      {...(onClick !== undefined ? { onClick } : {})}
    />
  );
}

/** GatePanel's disclosure: a real button over a Collapse, closed by default. */
function Disclosure({
  title,
  children,
  t,
}: {
  title: string;
  children: ReactNode;
  t: Tokens;
}): ReactNode {
  const [open, setOpen] = useState(false);
  const region = useId();
  return (
    <Box sx={{ border: `1px solid ${t.line}`, borderRadius: 1, overflow: 'hidden' }}>
      <Box
        aria-controls={region}
        aria-expanded={open}
        component="button"
        sx={{
          display: 'flex',
          alignItems: 'center',
          gap: 1,
          px: 1.25,
          py: 0.75,
          width: '100%',
          cursor: 'pointer',
          textAlign: 'left',
          font: 'inherit',
          color: 'inherit',
          border: 'none',
          bgcolor: t.paper,
          borderBottom: open ? `1px solid ${t.line}` : 'none',
        }}
        type="button"
        onClick={() => {
          setOpen((v) => !v);
        }}
      >
        <Typography
          sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11, letterSpacing: '0.1em' }}
        >
          {title}
        </Typography>
        <Box sx={{ flexGrow: 1 }} />
        <ExpandMoreIcon
          sx={{ fontSize: 18, transform: open ? 'rotate(180deg)' : 'none', transition: '120ms' }}
        />
      </Box>
      <Collapse unmountOnExit id={region} in={open}>
        <Box sx={{ p: 1.25, display: 'flex', flexDirection: 'column', gap: 1 }}>{children}</Box>
      </Collapse>
    </Box>
  );
}

function Pre({ children, t }: { children: string; t: Tokens }): ReactNode {
  return (
    <Box
      component="pre"
      sx={{
        m: 0,
        p: 1.25,
        maxHeight: 480,
        overflow: 'auto',
        whiteSpace: 'pre-wrap',
        wordBreak: 'break-word',
        fontFamily: t.mono,
        fontSize: 11.5,
        lineHeight: 1.45,
        color: t.ink,
        bgcolor: t.paper,
        border: `1px solid ${t.line}`,
        borderRadius: 1,
      }}
    >
      {children}
    </Box>
  );
}

function Section({ label, children }: { label: ReactNode; children: ReactNode }): ReactNode {
  const t = useTokens();
  return (
    <Box
      sx={{
        display: 'flex',
        flexDirection: 'column',
        gap: 0.75,
        pt: 1.25,
        borderTop: `1px solid ${t.line}`,
        minWidth: 0,
      }}
    >
      {typeof label === 'string' ? <Label t={t}>{label}</Label> : label}
      {children}
    </Box>
  );
}

function Frame({ children }: { children: ReactNode }): ReactNode {
  const t = useTokens();
  return (
    <Box
      data-testid={UI_IDENTIFIERS.Activity.TASK_FACTS}
      sx={{
        display: 'flex',
        flexDirection: 'column',
        gap: 1.25,
        px: 2,
        py: 1.5,
        bgcolor: t.paperAlt,
        border: `1.5px solid ${t.line}`,
        borderRadius: t.radius / 8 + 0.5,
        minWidth: 0,
      }}
    >
      {children}
    </Box>
  );
}

function CheckLog({
  row,
  outputs,
  onWantOutput,
  t,
}: {
  row: CheckRowState;
  outputs: Record<string, AsyncArtifactData<string>>;
  onWantOutput: (ref: string) => void;
  t: Tokens;
}): ReactNode {
  const ref = row.result?.outputRef ?? '';
  // Mounted only under an expanded row: the log is fetched now, not before.
  useEffect(() => {
    if (ref.length > 0) onWantOutput(ref);
  }, [ref, onWantOutput]);
  if (ref.length === 0) {
    return (
      <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}>
        {SPEC_NO_OUTPUT}
      </Typography>
    );
  }
  const stream = outputs[ref];
  if (stream === undefined || stream.isLoading) {
    return (
      <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}>
        {SPEC_OUTPUT_LOADING}
      </Typography>
    );
  }
  if (stream.error !== null) {
    return (
      <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.dangerFg }}>
        {specOutputFailed(ref, stream.error.message)}
      </Typography>
    );
  }
  const text = stream.data ?? '';
  return <Pre t={t}>{text.length > 0 ? text : `(${ref} is empty)`}</Pre>;
}

function CheckRow({
  row,
  open,
  onToggle,
  outputs,
  fileUrl,
  onWantOutput,
  t,
}: {
  row: CheckRowState;
  open: boolean;
  onToggle: () => void;
  outputs: Record<string, AsyncArtifactData<string>>;
  fileUrl: (path: string) => string;
  onWantOutput: (ref: string) => void;
  t: Tokens;
}): ReactNode {
  const { check, result } = row;
  const line = (
    <>
      <StatusChip
        label={row.state === 'not-run' ? SPEC_NOT_RUN : row.state}
        status={row.state}
        t={t}
      />
      <Typography sx={{ fontFamily: t.body, fontSize: 12.5, color: t.ink, flexShrink: 0 }}>
        {check.title}
      </Typography>
      <Typography
        sx={{
          fontFamily: t.mono,
          fontSize: 10.5,
          color: t.muted,
          minWidth: 0,
          flexGrow: 1,
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
        }}
        title={sourceLabel(check)}
      >
        {sourceLabel(check)}
      </Typography>
      {result !== undefined ? (
        <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted, flexShrink: 0 }}>
          {formatDuration(result.durationMs)}
        </Typography>
      ) : null}
    </>
  );
  const rowSx = {
    display: 'flex',
    width: '100%',
    alignItems: 'center',
    gap: 1,
    px: 0.75,
    py: 0.5,
    textAlign: 'left',
    borderRadius: 1,
    minWidth: 0,
  } as const;
  if (result === undefined) {
    return (
      <Box data-check-id={check.id} data-check-state={row.state} sx={rowSx}>
        {line}
      </Box>
    );
  }
  return (
    <Box data-check-id={check.id} data-check-state={row.state} sx={{ minWidth: 0 }}>
      <ButtonBase
        aria-expanded={open}
        sx={{
          ...rowSx,
          bgcolor: open ? t.paper : 'transparent',
          borderLeft: `3px solid ${open ? t.accent : 'transparent'}`,
        }}
        onClick={onToggle}
      >
        {line}
      </ButtonBase>
      {open ? (
        <Box sx={{ pl: 1.5, pr: 0.5, py: 0.75 }}>
          {check.kind === 'scenario' ? (
            <ScenarioDetail
              fileUrl={fileUrl}
              outputs={outputs}
              scenario={scenarioViewOf(check, result)}
              t={t}
              onWantOutput={onWantOutput}
            />
          ) : (
            <CheckLog outputs={outputs} row={row} t={t} onWantOutput={onWantOutput} />
          )}
        </Box>
      ) : null}
    </Box>
  );
}

function Checks({
  spec,
  activityId,
  outputs,
  fileUrl,
  onWantOutput,
  t,
}: {
  spec: TaskSpecView;
  activityId: string;
  outputs: Record<string, AsyncArtifactData<string>>;
  fileUrl: (path: string) => string;
  onWantOutput: (ref: string) => void;
  t: Tokens;
}): ReactNode {
  const [openId, setOpenId] = useState<string>('');
  const rows = checkRows(spec);
  const counts = checkCounts(rows);
  const header = (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
      <Label t={t}>CHECKS</Label>
      {rows.length > 0 ? (
        <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}>
          {countsLine(counts)}
        </Typography>
      ) : null}
      {spec.specChanged && spec.checkRun !== undefined ? (
        <Chip
          label={SPEC_CHANGED}
          size="small"
          sx={{
            height: 18,
            fontFamily: t.mono,
            fontSize: 9,
            bgcolor: t.awaitingBg,
            color: t.awaitingFg,
            border: `1px solid ${t.awaitingFg}`,
          }}
        />
      ) : null}
    </Box>
  );
  return (
    <Section label={header}>
      {rows.length === 0 ? (
        <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}>
          {SPEC_NO_CHECKS}
        </Typography>
      ) : (
        groupByKind(rows).map((g) => (
          <Box key={g.kind} sx={{ display: 'flex', flexDirection: 'column', gap: 0.25 }}>
            <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted, mt: 0.5 }}>
              {KIND_LABEL[g.kind]}
            </Typography>
            <CommentableList
              ariaLabel={`${KIND_LABEL[g.kind]} checks`}
              gap={0.25}
              getAnchor={(r) => ({
                kind: 'node',
                label: `check:${r.check.id}`,
                source: `Task ${spec.task} · ${r.check.title}`,
                jsonPath: taskSpecCheckAnchor(activityId, spec.task, r.check.id),
              })}
              getKey={(r) => r.check.id}
              getLabel={(r) => `check ${r.check.title} ${r.state}`}
              items={g.rows}
              renderItem={(r) => (
                <CheckRow
                  fileUrl={fileUrl}
                  open={openId === r.check.id}
                  outputs={outputs}
                  row={r}
                  t={t}
                  onToggle={() => {
                    setOpenId((cur) => (cur === r.check.id ? '' : r.check.id));
                  }}
                  onWantOutput={onWantOutput}
                />
              )}
            />
          </Box>
        ))
      )}
    </Section>
  );
}

function EditScope({ spec, t }: { spec: TaskSpecView; t: Tokens }): ReactNode {
  const [openComp, setOpenComp] = useState<string>('');
  const scope = spec.editScope;
  if (scope.stateOnly) {
    return (
      <Section label="MAY EDIT">
        <Typography sx={{ fontSize: 12.5, color: t.ink }}>{SPEC_STATE_ONLY}</Typography>
      </Section>
    );
  }
  const shown = scope.components.find((c) => c.id === openComp);
  return (
    <Section label="MAY EDIT">
      <Box sx={{ display: 'flex', gap: 0.5, flexWrap: 'wrap', alignItems: 'center' }}>
        {scope.components.map((c) => (
          <MonoChip
            key={c.id}
            label={c.id}
            pressed={openComp === c.id}
            t={t}
            onClick={() => {
              setOpenComp((cur) => (cur === c.id ? '' : c.id));
            }}
          />
        ))}
      </Box>
      {shown !== undefined || scope.extra.length > 0 ? (
        <Box component="ul" sx={{ m: 0, pl: 2.5, fontFamily: t.mono, fontSize: 11, color: t.ink }}>
          {[...(shown?.paths ?? []), ...scope.extra].map((p) => (
            <li key={p}>{p}</li>
          ))}
        </Box>
      ) : null}
    </Section>
  );
}

function SpecBody({
  spec,
  episodeCostUsd,
  activityId,
  outputs,
  fileUrl,
  onWantOutput,
}: TaskSpecPanelProps & { spec: { data: TaskSpecView } }): ReactNode {
  const t = useTokens();
  const s = spec.data;
  const { identity, model, prompt, tools } = s;
  const show = specSections(s);
  return (
    <>
      <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 3, flexWrap: 'wrap' }}>
        {show.role ? (
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <RoleAvatar seed={identity.workerClass} size={26} />
            <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 13, color: t.ink }}>
              {ROLE_LABEL[identity.workerClass] ?? identity.workerClass}
            </Typography>
          </Box>
        ) : null}
        {show.command ? <Fact label="COMMAND">{`/${identity.command}`}</Fact> : null}
        {show.model ? (
          <Fact label="MODEL">
            <Tooltip title={modelSourceTip(model.source)}>
              <Chip
                data-model-source={model.source}
                label={model.id}
                size="small"
                sx={{
                  height: 20,
                  fontFamily: t.mono,
                  fontWeight: 700,
                  fontSize: 11.5,
                  bgcolor: t.paper,
                  color: t.ink,
                  border: `1px solid ${t.line}`,
                }}
              />
            </Tooltip>
          </Fact>
        ) : null}
        {show.cost ? (
          <Fact label="COST">
            {`est ${formatUsd(model.estimate.usd)}${
              episodeCostUsd !== undefined ? ` · actual ${formatUsd(episodeCostUsd)}` : ''
            }`}
          </Fact>
        ) : null}
        {show.exitCriterion ? (
          <Box sx={{ minWidth: 240, flexBasis: 320, flexGrow: 1 }}>
            <Label t={t}>EXIT CRITERION</Label>
            <Typography sx={{ fontSize: 13.5, color: t.ink, lineHeight: 1.45 }}>
              {identity.exitCriterion}
            </Typography>
          </Box>
        ) : null}
      </Box>

      {show.prompt ? (
        <Disclosure t={t} title={`PROMPT · ~${formatTokens(prompt.tokens)} tokens`}>
          <Pre t={t}>{prompt.text}</Pre>
          {prompt.charter.length > 0 ? (
            <Disclosure t={t} title={`CHARTER · ${identity.agent}`}>
              <Pre t={t}>{prompt.charter}</Pre>
            </Disclosure>
          ) : null}
          {prompt.skills.length > 0 ? (
            <Disclosure t={t} title={`SKILLS · ${String(prompt.skills.length)}`}>
              <Box sx={{ display: 'flex', gap: 0.5, flexWrap: 'wrap' }}>
                {prompt.skills.map((k) => (
                  <MonoChip key={k} label={k} t={t} />
                ))}
              </Box>
            </Disclosure>
          ) : null}
        </Disclosure>
      ) : null}

      {show.tools ? (
        <Section label="TOOLS">
          {(
            [
              ['State tools', tools.mcp],
              ['Built-ins', tools.builtin],
            ] as const
          )
            .filter(([, list]) => list.length > 0)
            .map(([name, list]) => (
              <Box
                key={name}
                sx={{ display: 'flex', gap: 0.5, flexWrap: 'wrap', alignItems: 'center' }}
              >
                <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted, mr: 0.5 }}>
                  {name}
                </Typography>
                {list.map((x) => (
                  <MonoChip key={x} label={x} t={t} />
                ))}
              </Box>
            ))}
        </Section>
      ) : null}

      {show.editScope ? <EditScope spec={s} t={t} /> : null}

      <Checks
        activityId={activityId}
        fileUrl={fileUrl}
        outputs={outputs}
        spec={s}
        t={t}
        onWantOutput={onWantOutput}
      />
    </>
  );
}

export function TaskSpecPanel(props: TaskSpecPanelProps): ReactNode {
  const t = useTokens();
  const { spec } = props;
  if (spec.data !== undefined) {
    return (
      <Frame>
        <SpecBody {...props} spec={{ ...spec, data: spec.data }} />
      </Frame>
    );
  }
  if (spec.error !== null) {
    return (
      <Frame>
        <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}>
          {specReadFailed(spec.error.message)}
        </Typography>
      </Frame>
    );
  }
  return (
    <Frame>
      <Skeleton aria-label={SPEC_LOADING} height={20} variant="text" width="60%" />
    </Frame>
  );
}
