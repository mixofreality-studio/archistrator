import { createTheme, type Theme } from '@mui/material/styles';

/**
 * Five swappable design languages for archistrator. Each is a bag of semantic
 * tokens; components read them via useTokens(). buildMuiTheme() turns a bag into
 * an MUI theme so stock components inherit the look too. Ported verbatim from the
 * frozen UX mock (methodpoc/designs/aiarch/ux-mock/src/theme/themes.ts).
 */

export type ThemeKey = 'retro' | 'mor' | 'retroMor' | 'blueprint' | 'blueprintMor';

export interface Tokens {
  key: ThemeKey;
  name: string;
  tag: string;
  mode: 'light' | 'dark';
  bg: string;
  paper: string;
  paperAlt: string;
  ink: string;
  muted: string;
  line: string;
  accent: string;
  accentText: string;
  accent2: string;
  mono: string;
  display: string;
  body: string;
  texture: string;
  textureSize?: string;
  hardShadow: boolean;
  shadowColor: string;
  radius: number;
  committedBg: string;
  committedFg: string;
  committedDot: string;
  /** error / danger tone — e.g. a defect ticket / wax seal in the team avatars. */
  dangerFg: string;
  awaitingBg: string;
  awaitingFg: string;
  /** Float-band YELLOW (6–25d slack) — a real amber, distinct from accent/danger. */
  bandYellow: string;
  /** Float-band GREEN (≥26d slack) — distinct from accent (critical) per theme. */
  bandGreen: string;
  chatArchitectBg: string;
  chatArchitectFg: string;
  chatPmBg: string;
  chatPmFg: string;
}

const scan = (a: number, c = '34,32,27'): string =>
  `repeating-linear-gradient(0deg, rgba(${c},${String(a)}) 0 2px, transparent 2px 4px)`;
const grid = (c: string): string =>
  `linear-gradient(${c} 1px, transparent 1px), linear-gradient(90deg, ${c} 1px, transparent 1px)`;

export const TOKENS: Record<ThemeKey, Tokens> = {
  retro: {
    key: 'retro',
    name: 'Retro Terminal',
    tag: 'warm paper · amber CRT',
    mode: 'light',
    bg: '#EFE7D3',
    paper: '#FBF6EA',
    paperAlt: '#FCF9F1',
    ink: '#22201B',
    muted: '#6E6452',
    line: '#22201B',
    accent: '#C2691C',
    accentText: '#FFF8EC',
    accent2: '#2F6E6A',
    mono: '"Space Mono", ui-monospace, monospace',
    display: '"Space Grotesk", system-ui, sans-serif',
    body: '"Space Grotesk", system-ui, sans-serif',
    texture: scan(0.015),
    hardShadow: true,
    shadowColor: '#22201B',
    radius: 3,
    committedBg: '#D8E4C2',
    committedFg: '#2C3F1B',
    committedDot: '#6E8A3F',
    dangerFg: '#8A2A18',
    awaitingBg: '#F2D6AE',
    awaitingFg: '#5A2E10',
    bandYellow: '#A88A00',
    bandGreen: '#2E7D32',
    chatArchitectBg: '#E7DFF2',
    chatArchitectFg: '#3A2A55',
    chatPmBg: '#C5DEDB',
    chatPmFg: '#1F4744',
  },
  mor: {
    key: 'mor',
    name: 'Mix of Reality',
    tag: 'dark editorial · lavender',
    mode: 'dark',
    bg: '#0a0a0a',
    paper: '#1a1a1a',
    paperAlt: '#141414',
    ink: '#f5f5f5',
    muted: '#a3a3a3',
    line: 'rgba(255,255,255,0.12)',
    accent: '#7B68AE',
    accentText: '#ffffff',
    accent2: '#9584C0',
    mono: '"JetBrains Mono", ui-monospace, monospace',
    display: '"Playfair Display", Georgia, serif',
    body: '"Inter", system-ui, sans-serif',
    texture: 'radial-gradient(1200px 600px at 70% -10%, rgba(123,104,174,0.10), transparent)',
    hardShadow: false,
    shadowColor: 'rgba(0,0,0,0.6)',
    radius: 8,
    committedBg: 'rgba(123,160,110,0.16)',
    committedFg: '#b6d49a',
    committedDot: '#7BA86E',
    dangerFg: '#f0a59a',
    awaitingBg: 'rgba(200,150,70,0.18)',
    awaitingFg: '#e0b06a',
    bandYellow: '#E6C84D',
    bandGreen: '#6FBF73',
    chatArchitectBg: 'rgba(123,104,174,0.2)',
    chatArchitectFg: '#cdbef0',
    chatPmBg: 'rgba(80,150,150,0.16)',
    chatPmFg: '#8fd0ca',
  },
  retroMor: {
    key: 'retroMor',
    name: 'Warm Dusk',
    tag: 'retro × mix of reality',
    mode: 'dark',
    bg: '#181410',
    paper: '#221C16',
    paperAlt: '#1C1712',
    ink: '#F3E9D8',
    muted: '#B7A98F',
    line: 'rgba(243,233,216,0.18)',
    accent: '#D98A2B',
    accentText: '#1a140d',
    accent2: '#9584C0',
    mono: '"Space Mono", ui-monospace, monospace',
    display: '"Playfair Display", Georgia, serif',
    body: '"Inter", system-ui, sans-serif',
    texture: scan(0.05, '243,233,216'),
    hardShadow: true,
    shadowColor: '#D98A2B',
    radius: 4,
    committedBg: 'rgba(140,170,100,0.18)',
    committedFg: '#cfe0a8',
    committedDot: '#9CB36A',
    dangerFg: '#eaa78f',
    awaitingBg: 'rgba(217,138,43,0.2)',
    awaitingFg: '#e7b574',
    bandYellow: '#CFCB55',
    bandGreen: '#7FB562',
    chatArchitectBg: 'rgba(149,132,192,0.22)',
    chatArchitectFg: '#d8cdf0',
    chatPmBg: 'rgba(80,150,150,0.18)',
    chatPmFg: '#9fd6d0',
  },
  blueprint: {
    key: 'blueprint',
    name: 'Blueprint',
    tag: 'architect · drafting grid',
    mode: 'dark',
    bg: '#0F2A45',
    paper: '#143A5A',
    paperAlt: '#102F4C',
    ink: '#E8F1FA',
    muted: '#8FB3CE',
    line: 'rgba(168,216,255,0.28)',
    accent: '#5FC6E8',
    accentText: '#06243B',
    accent2: '#A8D8FF',
    mono: '"JetBrains Mono", ui-monospace, monospace',
    display: '"Space Grotesk", system-ui, sans-serif',
    body: '"Inter", system-ui, sans-serif',
    texture: grid('rgba(168,216,255,0.07)'),
    textureSize: '26px 26px',
    hardShadow: false,
    shadowColor: 'rgba(0,0,0,0.5)',
    radius: 0,
    committedBg: 'rgba(95,198,232,0.16)',
    committedFg: '#a9e3f5',
    committedDot: '#5FC6E8',
    dangerFg: '#f3a89a',
    awaitingBg: 'rgba(240,180,90,0.18)',
    awaitingFg: '#f0c074',
    bandYellow: '#E8C547',
    bandGreen: '#5FD08A',
    chatArchitectBg: 'rgba(168,216,255,0.14)',
    chatArchitectFg: '#cfe6fa',
    chatPmBg: 'rgba(120,220,200,0.14)',
    chatPmFg: '#9fe6d6',
  },
  blueprintMor: {
    key: 'blueprintMor',
    name: 'Drafting Room',
    tag: 'architect × mix of reality',
    mode: 'dark',
    bg: '#0C1A2E',
    paper: '#14233A',
    paperAlt: '#101D30',
    ink: '#ECEAF5',
    muted: '#9AA3B8',
    line: 'rgba(255,255,255,0.12)',
    accent: '#7B68AE',
    accentText: '#ffffff',
    accent2: '#6EC6E6',
    mono: '"JetBrains Mono", ui-monospace, monospace',
    display: '"Playfair Display", Georgia, serif',
    body: '"Inter", system-ui, sans-serif',
    texture: grid('rgba(123,104,174,0.08)'),
    textureSize: '30px 30px',
    hardShadow: false,
    shadowColor: 'rgba(0,0,0,0.55)',
    radius: 6,
    committedBg: 'rgba(110,198,230,0.14)',
    committedFg: '#a8def0',
    committedDot: '#6EC6E6',
    dangerFg: '#eaa3ad',
    awaitingBg: 'rgba(200,150,70,0.18)',
    awaitingFg: '#e0b06a',
    bandYellow: '#E6C84D',
    bandGreen: '#7FCB7A',
    chatArchitectBg: 'rgba(123,104,174,0.22)',
    chatArchitectFg: '#cdbef0',
    chatPmBg: 'rgba(110,198,230,0.16)',
    chatPmFg: '#9fd9ec',
  },
};

export const THEME_ORDER: ThemeKey[] = ['retro', 'mor', 'retroMor', 'blueprint', 'blueprintMor'];

/** A border helper consistent with the theme's line weight + radius. */
export function border(t: Tokens, w = 1.5): string {
  return `${String(w)}px solid ${t.line}`;
}

/** Raised-card effect: hard offset shadow for boxy themes, soft for the rest. */
export function raise(t: Tokens, n = 3): string {
  return t.hardShadow
    ? `${String(n)}px ${String(n)}px 0 ${t.shadowColor}`
    : `0 ${String(n * 2)}px ${String(n * 6)}px ${t.shadowColor}`;
}

export function buildMuiTheme(t: Tokens): Theme {
  return createTheme({
    palette: {
      mode: t.mode,
      primary: { main: t.accent, contrastText: t.accentText },
      secondary: { main: t.accent2 },
      background: { default: t.bg, paper: t.paper },
      text: { primary: t.ink, secondary: t.muted },
      divider: t.line,
    },
    shape: { borderRadius: t.radius },
    typography: {
      fontFamily: t.body,
      h1: { fontFamily: t.display, fontWeight: 800, letterSpacing: '-0.02em' },
      h2: { fontFamily: t.display, fontWeight: 800, letterSpacing: '-0.015em' },
      h3: { fontFamily: t.display, fontWeight: 700, letterSpacing: '-0.01em' },
      h4: { fontFamily: t.display, fontWeight: 700 },
      h5: { fontFamily: t.display, fontWeight: 700 },
      h6: { fontFamily: t.display, fontWeight: 600 },
      button: {
        fontFamily: t.mono,
        fontWeight: 700,
        letterSpacing: '0.04em',
        textTransform: 'none',
      },
      overline: {
        fontFamily: t.mono,
        fontWeight: 700,
        letterSpacing: '0.18em',
        textTransform: 'uppercase',
      },
      subtitle2: { fontFamily: t.mono, fontWeight: 700, letterSpacing: '0.08em' },
      caption: { fontFamily: t.mono, letterSpacing: '0.02em' },
    },
    components: {
      MuiPaper: {
        defaultProps: { elevation: 0 },
        styleOverrides: { root: { backgroundImage: 'none', border: border(t) } },
      },
      MuiButton: {
        defaultProps: { disableElevation: true },
        styleOverrides: {
          root: { borderRadius: t.radius, paddingInline: 16 },
          contained: {
            border: border(t),
            boxShadow: raise(t),
            '&:hover': {
              boxShadow: t.hardShadow ? raise(t, 1) : raise(t, 2),
              transform: t.hardShadow ? 'translate(2px,2px)' : 'none',
            },
            transition: 'all 90ms ease',
          },
          outlined: { borderWidth: 1.5, '&:hover': { borderWidth: 1.5 } },
        },
      },
      MuiChip: {
        styleOverrides: {
          root: {
            fontFamily: t.mono,
            fontWeight: 700,
            letterSpacing: '0.06em',
            borderRadius: t.radius,
            border: border(t),
          },
          outlined: { borderWidth: 1.5 },
        },
      },
      MuiAppBar: {
        defaultProps: { elevation: 0, color: 'transparent' },
        styleOverrides: {
          root: { backgroundColor: t.paper, borderBottom: border(t), backgroundImage: 'none' },
        },
      },
      MuiAlert: {
        styleOverrides: { root: { borderRadius: t.radius, border: border(t), fontFamily: t.body } },
      },
      MuiTooltip: { styleOverrides: { tooltip: { fontFamily: t.mono, borderRadius: t.radius } } },
      MuiMenu: { styleOverrides: { paper: { border: border(t) } } },
    },
  });
}
