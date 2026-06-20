/**
 * Research-input affordance for the first System Design step. startSystemDesign
 * requires a ResearchInput corpus to be present; when the server reports it
 * missing (409 failed_precondition), this panel collects a single research source
 * (title + content) and submits it via setResearchInput. On success the caller
 * retries start. Minimal, deliberately one-source — the corpus can grow later.
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import TextField from '@mui/material/TextField';
import Button from '@mui/material/Button';
import ScienceOutlinedIcon from '@mui/icons-material/ScienceOutlined';
import type { ResearchInput } from '../../api/types';
import { useTokens } from '../../theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../constants/UIIdentifiers';

export function ResearchInputPanel({
  pending,
  onSubmit,
}: {
  pending: boolean;
  onSubmit: (research: ResearchInput) => void;
}): ReactNode {
  const t = useTokens();
  const [title, setTitle] = useState('');
  const [content, setContent] = useState('');
  const ready = title.trim().length > 0 && content.trim().length > 0;

  return (
    <Paper
      data-testid={UI_IDENTIFIERS.DesignExperience.RESEARCH_INPUT}
      sx={{ p: { xs: 2.5, md: 4 }, maxWidth: 760 }}
    >
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
        <ScienceOutlinedIcon sx={{ color: t.accent }} />
        <Typography sx={{ color: t.ink }} variant="h5">
          Add research input to begin
        </Typography>
      </Box>
      <Typography sx={{ color: t.muted, fontSize: 14, lineHeight: 1.6, mb: 3 }}>
        System Design distills the mission from your research corpus. Add at least one source — a
        founder brief, competitor analysis, or customer interview — to start the workflow.
      </Typography>
      <TextField
        fullWidth
        label="Source title"
        slotProps={{ htmlInput: { 'data-testid': UI_IDENTIFIERS.DesignExperience.RESEARCH_INPUT_TITLE } }}
        sx={{ mb: 2 }}
        value={title}
        onChange={(e) => { setTitle(e.target.value); }}
      />
      <TextField
        fullWidth
        multiline
        label="Source content"
        minRows={5}
        slotProps={{ htmlInput: { 'data-testid': UI_IDENTIFIERS.DesignExperience.RESEARCH_INPUT_TEXT } }}
        sx={{ mb: 3 }}
        value={content}
        onChange={(e) => { setContent(e.target.value); }}
      />
      <Button
        color="primary"
        data-testid={UI_IDENTIFIERS.DesignExperience.RESEARCH_INPUT_SUBMIT}
        disabled={!ready || pending}
        variant="contained"
        onClick={() => {
          onSubmit({ sources: [{ title: title.trim(), content: content.trim() }] });
        }}
      >
        Save research &amp; start
      </Button>
    </Paper>
  );
}
