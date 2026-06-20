/**
 * The "start a new project" dialog. NAME-AS-IDENTITY (C-PM-Δ, 2026-06-15): the
 * project name IS the GitHub repository name the user has already created — aiarch
 * ADOPTS that pre-existing repo (permissive: resumes if it already carries an
 * .aiarch/ marker). There is NO token/secret field — aiarch does no secret
 * management; the user provisions CLAUDE_CODE_OAUTH_TOKEN by installing the Claude
 * Code GitHub App on their repo. The dialog therefore carries a prerequisites panel
 * spelling out the one-time onboarding the user must complete before creating.
 *
 * Wired to the real useCreateProject mutation. Research input is captured later in
 * System Design, so this stays a one-field gate (the mock's customer/research
 * fields are dropped against the typed CreateProject contract, which takes only the
 * repo name).
 */
import { useState, type ReactNode } from 'react';
import Button from '@mui/material/Button';
import Dialog from '@mui/material/Dialog';
import DialogTitle from '@mui/material/DialogTitle';
import DialogContent from '@mui/material/DialogContent';
import DialogActions from '@mui/material/DialogActions';
import TextField from '@mui/material/TextField';
import Typography from '@mui/material/Typography';
import Box from '@mui/material/Box';
import AddIcon from '@mui/icons-material/Add';
import GitHubIcon from '@mui/icons-material/GitHub';
import { useNavigate } from '@tanstack/react-router';
import { useCreateProject } from '../hooks/useCreateProject';
import { useTokens } from '../theme/ThemeContext';
import { UI_IDENTIFIERS } from '../constants/UIIdentifiers';
import { ErrorAlert } from './shared/ErrorAlert';

export function CreateProjectDialog({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}): ReactNode {
  const t = useTokens();
  const navigate = useNavigate();
  const createProject = useCreateProject();
  const [name, setName] = useState('');

  const reset = (): void => {
    setName('');
    createProject.reset();
  };

  const close = (): void => {
    reset();
    onClose();
  };

  const submit = (): void => {
    const trimmed = name.trim();
    if (trimmed.length === 0 || createProject.isPending) return;
    createProject.mutate(trimmed, {
      onSuccess: (projectId) => {
        reset();
        onClose();
        void navigate({ to: '/project/$projectId/home', params: { projectId } });
      },
    });
  };

  return (
    <Dialog
      open={open}
      slotProps={{
        paper: {
          // data-testid is a valid DOM attribute on the Paper slot; the union
          // typing of DialogPaperSlotProps doesn't surface it, so we widen here.
          ...({ 'data-testid': UI_IDENTIFIERS.ProjectsLanding.CREATE_PROJECT_DIALOG } as Record<
            string,
            string
          >),
          sx: { minWidth: 460, border: `1.5px solid ${t.line}` },
        },
      }}
      onClose={close}
    >
      <DialogTitle sx={{ fontFamily: t.display, fontWeight: 700 }}>Start a new project</DialogTitle>
      <DialogContent
        sx={{ display: 'flex', flexDirection: 'column', gap: 2, pt: '8px !important' }}
      >
        <Typography sx={{ color: t.muted, fontSize: 13.5 }}>
          A project walks The Method from System Design through operation. The project name{' '}
          <strong>is your GitHub repository name</strong> — aiarch adopts the repo you already
          created and drives The Method against it.
        </Typography>

        <PrereqPanel t={t} />

        <TextField
          // Focusing the sole field of a freshly-opened modal is the expected
          // dialog UX (focus lands inside the trap), not a page-load focus steal.
          // eslint-disable-next-line jsx-a11y/no-autofocus
          autoFocus
          fullWidth
          helperText="Must exactly match the GitHub repository you created above."
          label="GitHub repository name"
          placeholder="e.g. loop-events-app"
          size="small"
          slotProps={{
            htmlInput: { 'data-testid': UI_IDENTIFIERS.ProjectsLanding.NEW_PROJECT_NAME_INPUT },
          }}
          value={name}
          onChange={(e) => {
            setName(e.target.value);
          }}
          onKeyDown={(e) => {
            if (e.key === 'Enter') submit();
          }}
        />
        <ErrorAlert error={createProject.error} />
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button
          data-testid={UI_IDENTIFIERS.ProjectsLanding.CREATE_PROJECT_CANCEL}
          sx={{ color: t.muted }}
          onClick={close}
        >
          Cancel
        </Button>
        <Button
          data-testid={UI_IDENTIFIERS.ProjectsLanding.CREATE_PROJECT_SUBMIT}
          disabled={name.trim().length === 0 || createProject.isPending}
          startIcon={<AddIcon />}
          variant="contained"
          onClick={submit}
        >
          Adopt &amp; open
        </Button>
      </DialogActions>
    </Dialog>
  );
}

/**
 * The one-time onboarding prerequisites the user must complete before adopting a
 * repo as an aiarch project. aiarch is NOT in secret management: the design token
 * is provisioned by the user installing the Claude Code GitHub App on their repo
 * (an OAuth-flow Actions secret, never carried through aiarch).
 */
function PrereqPanel({ t }: { t: ReturnType<typeof useTokens> }): ReactNode {
  const items: { label: string; detail: string }[] = [
    {
      label: 'Create an (empty) GitHub repository',
      detail: 'Its name becomes this project’s identity.',
    },
    {
      label: 'Install the aiarch GitHub App on the repo',
      detail: 'Grants contents:write + administration + metadata so aiarch can adopt and drive it.',
    },
    {
      label: 'Install the Claude Code GitHub App (run /install-github-app)',
      detail: 'Provisions CLAUDE_CODE_OAUTH_TOKEN — the design token aiarch never sees or stores.',
    },
  ];
  return (
    <Box
      data-testid={UI_IDENTIFIERS.ProjectsLanding.CREATE_PROJECT_PREREQS}
      sx={{
        border: `1.5px solid ${t.line}`,
        borderRadius: t.radius / 8 + 0.5,
        bgcolor: t.paperAlt,
        px: 2,
        py: 1.5,
      }}
    >
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, mb: 1 }}>
        <GitHubIcon sx={{ fontSize: 16, color: t.ink }} />
        <Typography sx={{ fontFamily: t.mono, fontSize: 11.5, letterSpacing: '0.12em', color: t.muted }}>
          BEFORE YOU ADOPT
        </Typography>
      </Box>
      <Box component="ol" sx={{ m: 0, pl: 2.25, display: 'flex', flexDirection: 'column', gap: 0.75 }}>
        {items.map((it) => (
          <Box component="li" key={it.label} sx={{ color: t.ink, fontSize: 13 }}>
            <Typography component="span" sx={{ fontSize: 13, fontWeight: 600, color: t.ink }}>
              {it.label}
            </Typography>
            <Typography sx={{ fontSize: 12.5, color: t.muted, lineHeight: 1.45 }}>
              {it.detail}
            </Typography>
          </Box>
        ))}
      </Box>
    </Box>
  );
}
