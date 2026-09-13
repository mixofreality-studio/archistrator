import { createTheme, type Theme } from '@mui/material/styles';

// The scanline texture generator. It used to be declared right here; it moved to
// a zero-import sibling so the construction lens's provenance rail can reuse the
// SAME geometry without pulling MUI into node:test. See textures.ts.
import { scan } from './textures.ts';

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
  /**
   * Text-safe variant of committedDot, used ONLY where the committed-green reads
   * as TEXT on paper (the axis-2 caption + the detail panel's axis-2 label). The
   * dot/border usages keep committedDot; on every theme but retro this equals
   * committedDot (those already clear AA as text). Retro darkens it: the mid-olive
   * committedDot (#6E8A3F) hit only 3.63:1 on the warm paper (#FBF6EA), below the
   * 4.5:1 AA floor for small text — #566E2E clears it at 5.31:1, same olive family.
   */
  committedText: string;
  /**
   * FAILED / ERROR only (designer palette ruling): a failed build, a defect ticket,
   * an error line. Never "critical" — that is criticalFg — and never a float band.
   */
  dangerFg: string;
  awaitingBg: string;
  /** Awaiting a human: the owed gate, a blocked build status, the awaiting borders. */
  awaitingFg: string;
  /**
   * The ONE "critical" colour (designer palette ruling): the float-0 band AND every
   * critical-path mark — a node's stripe, border, ring, header and chip, the
   * critical edges and milestones, the legend and the minimap, the graph lens's
   * critical lane edge. The accent never means critical, and neither does dangerFg.
   * The heaviest colour on the float ramp (bandRamp.test.ts measures it).
   */
  criticalFg: string;
  /** Text on a criticalFg fill (a critical node's header, the CP chip): ≥ 4.5:1. */
  criticalText: string;
  /**
   * Float-band RED (≤5d slack). The float ramp runs criticalFg → bandRed →
   * bandYellow → bandGreen, and each step, measured against paper and paperAlt,
   * is lighter in weight (contrast falls ≥1.08× a step, green ≥3.2:1), later in
   * LCh hue (≥12° a step) and ≥10 CIEDE2000 from its neighbour, and every band is
   * ≥15 CIEDE2000 from every status colour and the accent (bandRamp.test.ts).
   */
  bandRed: string;
  /** Float-band YELLOW (6–25d slack) — an amber, on the same ramp. */
  bandYellow: string;
  /** Float-band GREEN (≥26d slack) — the lightest step, still ≥3.2:1. */
  bandGreen: string;
  chatArchitectBg: string;
  chatArchitectFg: string;
  chatPmBg: string;
  chatPmFg: string;
}

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
    accent: '#A85817',
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
    // Darkened from the #6E8A3F dot (3.63:1 as text on paper #FBF6EA, below AA
    // 4.5:1) to a text-safe olive that clears 5.31:1 while staying in family.
    committedText: '#566E2E',
    dangerFg: '#8A2A18',
    awaitingBg: '#F2D6AE',
    awaitingFg: '#5A2E10',
    // The float ramp (designer palette ruling): criticalFg → bandRed → bandYellow →
    // bandGreen, each lighter against the warm paper than the last. The yellow
    // (#9C7800) still clears WCAG 1.4.11's 3:1 for non-text indicators (the
    // stale-basis chip border/icon + the float-band markers) on paperAlt (#FCF9F1),
    // at 3.82:1; the old #A88A00 hit only ~2.96:1.
    criticalFg: '#A80F45',
    criticalText: '#FFFFFF',
    bandYellow: '#9C7800',
    bandGreen: '#009C68',
    bandRed: '#E61E00',
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
    committedText: '#7BA86E',
    dangerFg: '#f0a59a',
    awaitingBg: 'rgba(200,150,70,0.18)',
    awaitingFg: '#e0b06a',
    criticalFg: '#FC3C68',
    criticalText: '#000000',
    bandYellow: '#8C741C',
    bandGreen: '#007C00',
    bandRed: '#D85800',
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
    committedText: '#9CB36A',
    dangerFg: '#eaa78f',
    awaitingBg: 'rgba(217,138,43,0.2)',
    awaitingFg: '#e7b574',
    criticalFg: '#FC4C70',
    criticalText: '#000000',
    bandYellow: '#887800',
    bandGreen: '#007C4C',
    bandRed: '#E44824',
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
    committedText: '#5FC6E8',
    dangerFg: '#f3a89a',
    awaitingBg: 'rgba(240,180,90,0.18)',
    awaitingFg: '#f0c074',
    criticalFg: '#FC6894',
    criticalText: '#000000',
    bandYellow: '#A88C00',
    bandGreen: '#009C44',
    bandRed: '#F86800',
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
    committedText: '#6EC6E6',
    dangerFg: '#eaa3ad',
    awaitingBg: 'rgba(200,150,70,0.18)',
    awaitingFg: '#e0b06a',
    criticalFg: '#FC5054',
    criticalText: '#000000',
    bandYellow: '#887C00',
    bandGreen: '#008434',
    bandRed: '#C86C10',
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
      // Global :focus-visible ring — a clearly visible outline in every theme,
      // built from the theme's own accent token (never a hardcoded color). Covers
      // native focusable elements (links, custom role="button"/"tab" elements with
      // a real tabIndex) that fall outside MuiButtonBase.
      MuiCssBaseline: {
        styleOverrides: {
          ':focus-visible': {
            outline: 'none',
            boxShadow: `0 0 0 2px ${t.accent}`,
            borderRadius: t.radius,
          },
        },
      },
      MuiButtonBase: {
        styleOverrides: {
          root: {
            '&.Mui-focusVisible': {
              outline: 'none',
              boxShadow: `0 0 0 2px ${t.accent}`,
            },
          },
        },
      },
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
