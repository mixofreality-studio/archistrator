import { useMemo, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import { useTokens } from '../theme/ThemeContext';
import type { Tokens } from '../theme/themes';

/**
 * A self-contained, deterministic 8-bit *person* — a little retro pixel figure
 * (head / face / hair / shoulders), paired with a role-identifying PROP that
 * signals the Method responsibility of the role. No external image fetch (this
 * is an offline mock): everything is inline SVG.
 *
 * Design rules honoured here:
 *  - The ROLE is conveyed by the PROP, never by the person's looks. The figure's
 *    skin tone / hair / presentation are seeded from the role id (stable, but
 *    varied across the roster) so the team doesn't read as monolithic.
 *  - The prop is an explicit per-role mapping (PROP_FOR), aligned to each
 *    charter in `team.ts` — e.g. Test Engineer carries the breaking-machine
 *    (a wrench/rig) while Software Tester carries a bug-net + defect ticket, so
 *    the prop reinforces the QA ≠ TestEngineer ≠ Tester distinction.
 *  - Theme-aware: skin / hair / clothes / prop tones are derived from the live
 *    theme tokens, so every figure re-skins with the theme (incl. Retro
 *    Terminal, Blueprint, etc.) instead of hardcoding hex.
 *  - Retro charm kept: hard frame, CRT scanlines on hard-shadow themes, a
 *    little power-LED dot, pixelated rendering.
 *
 * Coordinate system: SVG viewBox 0..100. The figure occupies a "portrait"
 * region; the prop is drawn in the lower-right as a held tool.
 */

// xmur3 string hash → seed; mulberry32 → deterministic [0,1) stream.
function seededRandom(str: string): () => number {
  let h = 1779033703 ^ str.length;
  for (let i = 0; i < str.length; i++) {
    h = Math.imul(h ^ str.charCodeAt(i), 3432918353);
    h = (h << 13) | (h >>> 19);
  }
  let a = h >>> 0;
  return () => {
    a |= 0;
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

// ---------------------------------------------------------------------------
// Color helpers — blend theme tokens toward warm/neutral skin & hair tones so
// the figures stay human-readable while still re-skinning per theme.
// ---------------------------------------------------------------------------

function parseColor(c: string): [number, number, number] {
  const s = c.trim();
  if (s.startsWith('#')) {
    const h = s.slice(1);
    const n = h.length === 3 ? h.split('').map((x) => x + x).join('') : h;
    return [parseInt(n.slice(0, 2), 16), parseInt(n.slice(2, 4), 16), parseInt(n.slice(4, 6), 16)];
  }
  const m = /rgba?\(([^)]+)\)/.exec(s);
  if (m) {
    const parts = (m[1] ?? '').split(',').map((p) => parseFloat(p));
    return [parts[0] ?? 0, parts[1] ?? 0, parts[2] ?? 0];
  }
  return [128, 128, 128];
}

function toHex([r, g, b]: [number, number, number]): string {
  const h = (v: number): string =>
    Math.max(0, Math.min(255, Math.round(v))).toString(16).padStart(2, '0');
  return `#${h(r)}${h(g)}${h(b)}`;
}

function mix(a: string, b: string, w: number): string {
  const [ar, ag, ab] = parseColor(a);
  const [br, bg, bb] = parseColor(b);
  return toHex([ar + (br - ar) * w, ag + (bg - ag) * w, ab + (bb - ab) * w]);
}

function lighten(c: string, w: number): string {
  return mix(c, '#ffffff', w);
}
function darken(c: string, w: number): string {
  return mix(c, '#000000', w);
}

// A small set of human skin tones, varied across the roster. We keep these as
// intrinsic hues (so the team reads as diverse people) but nudge them toward
// the theme ink/paper so they sit naturally in every palette.
const SKIN_BASE = ['#F2C9A0', '#E0A878', '#C68642', '#8D5524', '#5C3A21', '#FBD7B5'];
// Hair hues — intrinsic, also varied.
const HAIR_BASE = ['#2B1B0E', '#5A3A1C', '#8A5A2B', '#B0763A', '#7A7A7A', '#D9C27A', '#A33A2A'];

// ---------------------------------------------------------------------------
// Gender-presentation variety.
//
// IMPORTANT DESIGN RULE: presentation is PURELY human diversity — it carries
// NO role meaning. The ROLE is signalled only by the PROP (PROP_FOR). To keep
// the roster balanced (~1/3 masculine, 1/3 feminine, 1/3 androgynous) AND to
// guarantee presentation does NOT correlate with role type, we assign it with
// an explicit per-role map that deliberately SCATTERS presentations across
// role kinds (architect→masc, a designer→fem, a reviewer→andro, developers and
// QA/test roles split across all three, etc.). A blind seeded coin-flip would
// risk an accidental skew or cluster, so we pin the spread here and let the
// SEED drive only the within-presentation detail (exact hair length/style,
// earrings, stubble) so two feminine figures still look like different people.
//
// Roster check (10 canonical roles):
//   masc  : system-architect, junior-developer, qa-engineer
//   fem   : product-manager, ui-designer, test-engineer, software-tester
//   andro : project-manager, senior-developer, ux-reviewer
// No role TYPE maps to a single presentation — designers split fem/andro,
// developers split masc/andro, QA/test split masc/fem.
// ---------------------------------------------------------------------------

type Presentation = 'masc' | 'fem' | 'andro';

const PRESENTATION_FOR: Record<string, Presentation> = {
  'system-architect': 'masc',
  'junior-developer': 'masc',
  'qa-engineer': 'masc',
  'product-manager': 'fem',
  'ui-designer': 'fem',
  'test-engineer': 'fem',
  'software-tester': 'fem',
  'project-manager': 'andro',
  'senior-developer': 'andro',
  'ux-reviewer': 'andro',
};

// Hair-style pools per presentation. Numbers index drawHairBack/Front() below.
//   0 short crop   1 tall/volume   2 side part   3 cap+locks   (masculine-leaning / neutral)
//   4 long down    5 bob           6 ponytail     7 bun
//   10 half-up     11 side braid                              (feminine-leaning)
//   8 tousled      9 undercut                                  (androgynous-leaning)
// fem pool is 6 wide so the four feminine figures all seed to DISTINCT styles
// (no two feminine teammates share a cut) — see the index spread in figureFor.
const HAIR_POOL: Record<Presentation, number[]> = {
  masc: [0, 2, 3],
  fem: [4, 5, 6, 7, 10, 11],
  andro: [8, 9, 5, 2],
};

interface FigurePlan {
  skin: string;
  skinShade: string;
  hair: string;
  hairStyle: number;
  clothes: string;
  hasGlasses: boolean;
  presentation: Presentation;
  hasEarrings: boolean;
  hasStubble: boolean;
  lashes: boolean; // softer/longer lashes cue
  collar: 'crew' | 'vee' | 'band'; // clothing collar shape
}

function figureFor(seed: string, t: Tokens): FigurePlan {
  const rnd = seededRandom(seed + '::figure');
  const skinRaw = SKIN_BASE[Math.floor(rnd() * SKIN_BASE.length)] ?? '#F2C9A0';
  const hairRaw = HAIR_BASE[Math.floor(rnd() * HAIR_BASE.length)] ?? '#2B1B0E';
  // nudge intrinsic tones a touch toward the theme so they sit in the palette,
  // but keep them clearly skin/hair (small blend weight).
  const skin = mix(skinRaw, t.ink, t.mode === 'dark' ? 0.08 : 0.04);
  const hair = mix(hairRaw, t.ink, t.mode === 'dark' ? 0.12 : 0.05);
  const clothes = mix(t.accent2, t.paper, 0.32);

  const presentation = PRESENTATION_FOR[seed] ?? 'andro';
  const pool = HAIR_POOL[presentation];
  const hairStyle = pool[Math.floor(rnd() * pool.length)] ?? 0;

  // Within-presentation human detail — seeded, so figures of the same
  // presentation still differ. Accessory cues are presentation-leaning but not
  // exclusive (an androgynous figure may wear a single stud; a masc figure may
  // have stubble or not).
  const accRoll = rnd();
  const hasEarrings =
    presentation === 'fem' ? accRoll < 0.7 : presentation === 'andro' ? accRoll < 0.35 : false;
  const hasStubble = presentation === 'masc' ? rnd() < 0.5 : false;
  const lashes = presentation === 'fem';
  const collar: FigurePlan['collar'] =
    presentation === 'fem' ? 'vee' : presentation === 'masc' ? 'crew' : 'band';

  return {
    skin,
    skinShade: darken(skin, 0.18),
    hair,
    hairStyle,
    clothes,
    hasGlasses: rnd() < 0.4,
    presentation,
    hasEarrings,
    hasStubble,
    lashes,
    collar,
  };
}

// ---------------------------------------------------------------------------
// Per-role PROP mapping — keyed by role id, aligned to each charter in team.ts.
// Each prop is a tiny inline-SVG vignette drawn in the lower-right "held" zone.
// `accent` is the prop's signature color (defaults to theme accent).
// ---------------------------------------------------------------------------

type PropKind =
  | 'blueprint' // architect — drafting / set-square over a plan
  | 'megaphone' // product manager — voice of the customer + research note
  | 'gantt' // project manager — gantt / network bars + clipboard
  | 'contract' // senior developer — sealed contract scroll
  | 'laptop' // junior developer — laptop with code brackets
  | 'palette' // ui designer — paint palette + stylus
  | 'loupe' // ux reviewer — magnifier over a wireframe
  | 'gauge' // qa engineer — process gauge / shield
  | 'wrench' // test engineer — the breaking-machine (wrench + gear rig)
  | 'bugnet'; // software tester — bug net + red defect ticket

const PROP_FOR: Record<string, PropKind> = {
  'system-architect': 'blueprint',
  'product-manager': 'megaphone',
  'project-manager': 'gantt',
  'senior-developer': 'contract',
  'junior-developer': 'laptop',
  'ui-designer': 'palette',
  'ux-reviewer': 'loupe',
  'qa-engineer': 'gauge',
  'test-engineer': 'wrench',
  'software-tester': 'bugnet',
};

// Tiny pixel-rect helper for crisp 8-bit shapes.
function px(
  key: string,
  x: number,
  y: number,
  w: number,
  h: number,
  fill: string,
  rx = 0,
): ReactNode {
  return <rect fill={fill} height={h + 0.3} key={key} rx={rx} width={w + 0.3} x={x} y={y} />;
}

/**
 * Draw the prop vignette. Returns SVG nodes sized within roughly x∈[52,98],
 * y∈[54,96] — the lower-right "held tool" zone — so it reads next to the figure.
 */
function drawProp(kind: PropKind, t: Tokens): ReactNode[] {
  const ac = t.accent;
  const ac2 = t.accent2;
  const ink = t.ink;
  const paper = lighten(t.paper, t.mode === 'dark' ? 0.06 : 0.1);
  const line = t.mode === 'dark' ? lighten(ink, 0.05) : darken(ink, 0.05);
  const danger = t.dangerFg;
  const n: ReactNode[] = [];

  switch (kind) {
    case 'blueprint': {
      // a drafting sheet with a set-square laid over a plan + ruler ticks
      const px0 = 54;
      const py0 = 56;
      n.push(px('bp-sheet', px0, py0, 38, 34, mix(ac, paper, 0.55)));
      n.push(px('bp-sheet2', px0 + 2, py0 + 2, 34, 30, mix(ac, '#0a2238', 0.55)));
      // plan lines
      n.push(px('bp-l1', px0 + 5, py0 + 7, 26, 1.6, lighten(ac2, 0.2)));
      n.push(px('bp-l2', px0 + 5, py0 + 13, 18, 1.6, lighten(ac2, 0.2)));
      n.push(px('bp-l3', px0 + 5, py0 + 19, 22, 1.6, lighten(ac2, 0.2)));
      // set-square (right triangle) over the plan
      n.push(
        <polygon
          fill="none"
          key="bp-tri"
          points={`${String(px0 + 8)},${String(py0 + 24)} ${String(px0 + 30)},${String(py0 + 24)} ${String(px0 + 30)},${String(py0 + 6)}`}
          stroke={ac}
          strokeWidth={2.4}
        />,
      );
      n.push(px('bp-pen', px0 + 28, py0 + 2, 2.4, 10, ink));
      break;
    }
    case 'megaphone': {
      // research note (book) + a megaphone — voice of the customer
      n.push(px('mp-book', 54, 70, 16, 20, mix(ac2, paper, 0.4)));
      n.push(px('mp-book2', 56, 72, 12, 16, paper));
      n.push(px('mp-bl1', 58, 76, 8, 1.4, line));
      n.push(px('mp-bl2', 58, 80, 8, 1.4, line));
      n.push(px('mp-bl3', 58, 84, 6, 1.4, line));
      // megaphone cone
      n.push(
        <polygon
          fill={mix(ac, paper, 0.15)}
          key="mp-cone"
          points="74,58 92,52 92,72 74,66"
          stroke={ink}
          strokeWidth={1.6}
        />,
      );
      n.push(px('mp-handle', 72, 60, 4, 4, ink));
      // sound waves
      n.push(
        <path d="M93 56 q4 6 0 12" fill="none" key="mp-w1" stroke={ac} strokeWidth={1.8} />,
      );
      break;
    }
    case 'gantt': {
      // a clipboard with gantt / network float bars
      n.push(px('gt-board', 54, 56, 38, 38, mix(ac2, paper, 0.5)));
      n.push(px('gt-clip', 68, 53, 10, 5, darken(ink, 0)));
      n.push(px('gt-paper', 57, 60, 32, 31, paper));
      const bars: [number, number, string][] = [
        [4, 18, ac],
        [10, 12, ac2],
        [6, 22, mix(ac, ink, 0.2)],
        [14, 14, ac2],
      ];
      bars.forEach(([off, w, c], i) => {
        n.push(px(`gt-b${String(i)}`, 60 + off, 64 + i * 6, w, 3.4, c));
      });
      break;
    }
    case 'contract': {
      // a sealed contract scroll — the frozen public contract
      n.push(px('ct-scroll', 58, 56, 26, 36, mix(paper, ac, 0.06)));
      n.push(px('ct-top', 56, 55, 30, 4, mix(ac, paper, 0.2)));
      n.push(px('ct-bot', 56, 90, 30, 4, mix(ac, paper, 0.2)));
      n.push(px('ct-l1', 62, 62, 18, 1.6, line));
      n.push(px('ct-l2', 62, 67, 18, 1.6, line));
      n.push(px('ct-l3', 62, 72, 12, 1.6, line));
      // wax seal
      n.push(<circle cx={71} cy={82} fill={danger} key="ct-seal" r={5} stroke={darken(danger, 0.3)} strokeWidth={1.2} />);
      n.push(px('ct-seal-x', 69, 81, 4, 2, lighten(danger, 0.3)));
      break;
    }
    case 'laptop': {
      // a laptop showing code brackets — builds against the contract
      n.push(px('lp-screen', 54, 58, 36, 26, mix(ink, paper, 0.1)));
      n.push(px('lp-screen2', 56, 60, 32, 22, mix(t.bg, ink, 0.2)));
      // code brackets { }
      n.push(
        <text fill={ac} fontFamily="monospace" fontSize={15} fontWeight="700" key="lp-code" textAnchor="middle" x={72} y={75}>
          {'{ }'}
        </text>,
      );
      // base
      n.push(px('lp-base', 50, 84, 44, 5, mix(ink, paper, 0.2)));
      n.push(px('lp-hinge', 54, 83, 36, 2, ink));
      break;
    }
    case 'palette': {
      // a paint palette + stylus — UI design
      n.push(
        <ellipse cx={70} cy={76} fill={paper} key="pl-base" rx={20} ry={15} stroke={ink} strokeWidth={1.6} />,
      );
      n.push(<ellipse cx={78} cy={80} fill={t.bg} key="pl-hole" rx={4} ry={3} />);
      const dots: [number, number, string][] = [
        [62, 70, ac],
        [70, 67, ac2],
        [78, 70, t.committedDot],
        [64, 79, danger],
        [73, 80, lighten(ac, 0.3)],
      ];
      dots.forEach(([cx, cy, c], i) => {
        n.push(<circle cx={cx} cy={cy} fill={c} key={`pl-d${String(i)}`} r={3} />);
      });
      // stylus
      n.push(px('pl-pen', 82, 54, 3, 20, ink));
      n.push(<polygon fill={ac} key="pl-tip" points="82,74 85,74 83.5,79" />);
      break;
    }
    case 'loupe': {
      // a magnifier over a wireframe + a check — UX review
      n.push(px('lo-wire', 54, 58, 30, 32, mix(ac2, paper, 0.5)));
      n.push(px('lo-wbar', 58, 62, 18, 3, line));
      n.push(px('lo-wbox', 58, 69, 12, 8, line));
      n.push(px('lo-wbox2', 72, 69, 6, 8, line));
      // magnifier
      n.push(<circle cx={78} cy={80} fill={mix(ac, paper, 0.7)} fillOpacity={0.5} key="lo-glass" r={10} stroke={ink} strokeWidth={2.4} />);
      n.push(px('lo-handle', 85, 87, 3, 9, ink, 1));
      // check mark inside
      n.push(<path d="M74 80 l3 3 l5 -6" fill="none" key="lo-check" stroke={t.committedDot} strokeWidth={2.2} />);
      break;
    }
    case 'gauge': {
      // a process gauge on a shield — QA assures the process
      n.push(
        <path
          d="M70 54 L90 60 L90 76 Q90 90 70 96 Q50 90 50 76 L50 60 Z"
          fill={mix(ac2, paper, 0.45)}
          key="ga-shield"
          stroke={ink}
          strokeWidth={1.8}
        />,
      );
      // gauge arc
      n.push(<path d="M60 78 A12 12 0 0 1 80 78" fill="none" key="ga-arc" stroke={line} strokeWidth={2.4} />);
      // needle to "pass" zone
      n.push(<line key="ga-needle" stroke={ac} strokeWidth={2.4} x1={70} x2={78} y1={78} y2={70} />);
      n.push(<circle cx={70} cy={78} fill={ink} key="ga-hub" r={2.4} />);
      n.push(px('ga-tickL', 59, 77, 2.4, 2.4, t.dangerFg));
      n.push(px('ga-tickR', 79, 77, 2.4, 2.4, t.committedDot));
      break;
    }
    case 'wrench': {
      // the breaking-MACHINE: a gear rig + a wrench. A tool that builds the
      // harness to break the system (distinct from the tester who runs it).
      n.push(<circle cx={66} cy={72} fill={mix(ink, paper, 0.18)} key="we-gear" r={13} stroke={ink} strokeWidth={1.6} />);
      // gear teeth
      for (let i = 0; i < 8; i++) {
        const ang = (i / 8) * Math.PI * 2;
        const gx = 66 + Math.cos(ang) * 13;
        const gy = 72 + Math.sin(ang) * 13;
        n.push(px(`we-t${String(i)}`, gx - 1.6, gy - 1.6, 3.2, 3.2, ink));
      }
      n.push(<circle cx={66} cy={72} fill={t.bg} key="we-hub" r={4} stroke={ink} strokeWidth={1.4} />);
      // wrench laid across
      n.push(
        <g key="we-wrench" transform="rotate(40 80 64)">
          <rect fill={ac} height={22} rx={1} width={4} x={78} y={52} />
          <path d="M76 50 a5 5 0 1 0 8 0 l-2 3 a3 3 0 1 1 -4 0 Z" fill={ac} />
        </g>,
      );
      break;
    }
    case 'bugnet': {
      // RUNS the machine: a bug net catching a bug + a red defect ticket.
      // net handle + hoop
      n.push(px('bn-handle', 54, 70, 3, 24, mix(ink, paper, 0.1)));
      n.push(<circle cx={64} cy={64} fill="none" key="bn-hoop" r={11} stroke={ink} strokeWidth={2.2} />);
      n.push(<circle cx={64} cy={64} fill={mix(ac2, paper, 0.6)} fillOpacity={0.4} key="bn-mesh" r={9} />);
      // mesh hatch
      n.push(<line key="bn-m1" stroke={line} strokeWidth={0.8} x1={57} x2={71} y1={60} y2={68} />);
      n.push(<line key="bn-m2" stroke={line} strokeWidth={0.8} x1={57} x2={71} y1={68} y2={60} />);
      // the bug
      n.push(<ellipse cx={64} cy={63} fill={danger} key="bn-bug" rx={4} ry={5} stroke={darken(danger, 0.3)} strokeWidth={0.8} />);
      n.push(<line key="bn-leg1" stroke={ink} strokeWidth={1} x1={60} x2={57} y1={62} y2={60} />);
      n.push(<line key="bn-leg2" stroke={ink} strokeWidth={1} x1={68} x2={71} y1={62} y2={60} />);
      n.push(<line key="bn-leg3" stroke={ink} strokeWidth={1} x1={60} x2={57} y1={65} y2={67} />);
      n.push(<line key="bn-leg4" stroke={ink} strokeWidth={1} x1={68} x2={71} y1={65} y2={67} />);
      // red defect ticket
      n.push(px('bn-ticket', 76, 78, 16, 14, mix(danger, paper, 0.25)));
      n.push(px('bn-tl1', 79, 82, 10, 1.6, darken(danger, 0.2)));
      n.push(px('bn-tl2', 79, 86, 7, 1.6, darken(danger, 0.2)));
      break;
    }
  }
  return n;
}

// ---------------------------------------------------------------------------
// The figure — a little pixel person: shoulders, neck, head, hair, eyes, glasses.
// ---------------------------------------------------------------------------

// Hair is split into a BACK layer (drawn before the head, so long curtains /
// ponytails / buns sit behind the face) and a FRONT layer (drawn over the
// head: the crown, fringe, locks). Styles 0..3 are short/neutral, 4..7 read as
// longer/up-do feminine cuts, 8..9 are androgynous medium cuts. The
// LENGTH/SILHOUETTE is the dominant cue at 72px.

function drawHairBack(f: FigurePlan): ReactNode[] {
  const n: ReactNode[] = [];
  const hair = f.hair;
  const dark = darken(f.hair, 0.2);
  switch (f.hairStyle) {
    case 4: // long hair down past the shoulders — back curtain
      n.push(<path d="M13 40 Q12 22 34 22 Q56 22 55 40 L55 80 Q51 64 51 46 L17 46 Q17 64 13 80 Z" fill={hair} key="fg-hair-back" />);
      break;
    case 5: // bob — chin-length back layer framing the jaw
      n.push(<path d="M14 40 Q14 22 34 22 Q54 22 54 40 L54 60 Q49 54 49 44 L19 44 Q19 54 14 60 Z" fill={hair} key="fg-hair-back" />);
      break;
    case 6: // ponytail — gathered tail trailing behind one side
      n.push(<path d="M48 34 Q62 38 60 56 Q58 72 50 76 Q56 60 50 44 Z" fill={hair} key="fg-tail" />);
      break;
    case 7: // bun — top/back knot
      n.push(<circle cx={34} cy={19} fill={hair} key="fg-bun" r={7.5} stroke={dark} strokeWidth={1} />);
      break;
    case 10: // half-up — shoulder-length back fall, gathered at the crown
      n.push(<path d="M15 40 Q15 23 34 23 Q53 23 53 40 L53 66 Q48 56 48 45 L20 45 Q20 56 15 66 Z" fill={hair} key="fg-hair-back" />);
      break;
    case 11: // side braid — single plaited rope falling over one shoulder
      n.push(<path d="M16 40 Q16 23 34 23 Q52 23 52 40 L52 54 Q47 48 47 44 L21 44 Q21 50 18 54 Z" fill={hair} key="fg-hair-back" />);
      // braid segments down the left side
      n.push(<circle cx={20} cy={58} fill={hair} key="fg-br1" r={3.6} stroke={dark} strokeWidth={0.8} />);
      n.push(<circle cx={20} cy={65} fill={hair} key="fg-br2" r={3.2} stroke={dark} strokeWidth={0.8} />);
      n.push(<circle cx={20} cy={71} fill={hair} key="fg-br3" r={2.6} stroke={dark} strokeWidth={0.8} />);
      break;
    default:
      break;
  }
  return n;
}

function drawHairFront(f: FigurePlan): ReactNode[] {
  const n: ReactNode[] = [];
  const hair = f.hair;
  const dark = darken(f.hair, 0.2);
  switch (f.hairStyle) {
    case 0: // short crop
      n.push(<path d="M18 40 Q18 24 34 24 Q50 24 50 40 L50 33 Q44 30 34 30 Q24 30 24 35 Z" fill={hair} key="fg-hair" />);
      break;
    case 1: // tall / volume
      n.push(<path d="M17 38 Q16 20 34 20 Q52 20 51 38 Q46 28 34 28 Q22 28 17 38 Z" fill={hair} key="fg-hair" />);
      break;
    case 2: // side part
      n.push(<path d="M18 40 Q18 24 34 24 Q52 24 50 40 Q48 28 30 29 Q24 30 22 40 Z" fill={hair} key="fg-hair" />);
      n.push(px('fg-part', 30, 25, 3, 10, dark));
      break;
    case 3: // cap of hair + locks at ears
      n.push(<path d="M18 41 Q18 24 34 24 Q50 24 50 41 L50 36 Q34 31 18 36 Z" fill={hair} key="fg-hair" />);
      n.push(px('fg-lockL', 18, 38, 4, 9, hair));
      n.push(px('fg-lockR', 46, 38, 4, 9, hair));
      break;
    case 4: // long — crown + fringe over the forehead, side strands by the cheeks
      n.push(<path d="M17 41 Q17 23 34 23 Q51 23 51 41 Q48 30 34 30 Q20 30 17 41 Z" fill={hair} key="fg-hair" />);
      n.push(px('fg-sideL', 18, 40, 3.5, 16, hair, 1));
      n.push(px('fg-sideR', 46.5, 40, 3.5, 16, hair, 1));
      break;
    case 5: // bob — crown + side strands to the jaw
      n.push(<path d="M17 41 Q17 24 34 24 Q51 24 51 41 Q47 30 34 30 Q21 30 17 41 Z" fill={hair} key="fg-hair" />);
      n.push(px('fg-sideL', 18, 40, 3.5, 11, hair, 1));
      n.push(px('fg-sideR', 46.5, 40, 3.5, 11, hair, 1));
      break;
    case 6: // ponytail — pulled-back crown + tie
      n.push(<path d="M17 40 Q17 23 34 23 Q51 23 51 40 Q47 29 34 29 Q21 29 17 40 Z" fill={hair} key="fg-hair" />);
      n.push(<circle cx={49} cy={37} fill={dark} key="fg-tie" r={2.4} />);
      break;
    case 7: // bun — sleek pulled-back crown
      n.push(<path d="M17 41 Q17 23 34 23 Q51 23 51 41 Q47 30 34 30 Q21 30 17 41 Z" fill={hair} key="fg-hair" />);
      break;
    case 10: // half-up — crown + short side strands by the cheeks
      n.push(<path d="M17 41 Q17 23 34 23 Q51 23 51 41 Q48 30 34 30 Q20 30 17 41 Z" fill={hair} key="fg-hair" />);
      n.push(px('fg-sideL', 18, 40, 3, 10, hair, 1));
      n.push(px('fg-sideR', 47, 40, 3, 10, hair, 1));
      break;
    case 11: // side braid — crown swept to one side over the forehead
      n.push(<path d="M17 41 Q17 23 34 23 Q51 23 51 41 Q48 29 28 30 Q22 31 17 41 Z" fill={hair} key="fg-hair" />);
      n.push(px('fg-sweep', 24, 26, 18, 3, dark, 1));
      break;
    case 8: // tousled medium (androgynous)
      n.push(<path d="M16 41 Q16 22 34 22 Q52 22 52 41 Q49 30 44 31 Q40 26 34 30 Q28 26 24 31 Q19 30 16 41 Z" fill={hair} key="fg-hair" />);
      break;
    default: // 9 — undercut: volume on top, shaved sides
      n.push(<path d="M22 40 Q21 22 34 22 Q47 22 46 40 Q44 29 34 29 Q24 29 22 40 Z" fill={hair} key="fg-hair" />);
      n.push(px('fg-uc-l', 19, 36, 3, 8, darken(f.skin, 0.12)));
      n.push(px('fg-uc-r', 46, 36, 3, 8, darken(f.skin, 0.12)));
  }
  return n;
}

function drawFigure(f: FigurePlan, t: Tokens): ReactNode[] {
  const n: ReactNode[] = [];
  const ink = t.ink;
  // back hair layer (long curtains / ponytail / bun) — behind the body & head
  n.push(...drawHairBack(f));
  // shoulders / torso (clothes)
  n.push(
    <path
      d="M14 96 Q14 74 34 72 Q54 74 54 96 Z"
      fill={f.clothes}
      key="fg-torso"
      stroke={darken(f.clothes, 0.25)}
      strokeWidth={1.2}
    />,
  );
  // collar — shape varies (crew / V-neck / band) as a quiet presentation cue
  if (f.collar === 'vee') {
    n.push(<path d="M26 73 L34 86 L42 73" fill="none" key="fg-collar" stroke={darken(f.clothes, 0.32)} strokeWidth={1.8} />);
  } else if (f.collar === 'band') {
    n.push(<path d="M26 76 Q34 80 42 76" fill="none" key="fg-collar" stroke={darken(f.clothes, 0.3)} strokeWidth={2} />);
  } else {
    n.push(<path d="M28 74 L34 82 L40 74" fill="none" key="fg-collar" stroke={darken(f.clothes, 0.3)} strokeWidth={1.6} />);
  }
  // neck
  n.push(px('fg-neck', 29, 60, 10, 12, f.skinShade));
  // head
  n.push(<rect fill={f.skin} height={34} key="fg-head" rx={9} width={28} x={20} y={28} />);
  // jaw shade
  n.push(<rect fill={f.skinShade} fillOpacity={0.35} height={12} key="fg-jaw" rx={9} width={28} x={20} y={50} />);
  // ears
  n.push(<rect fill={f.skin} height={8} key="fg-earL" rx={2.5} width={5} x={17} y={42} />);
  n.push(<rect fill={f.skin} height={8} key="fg-earR" rx={2.5} width={5} x={46} y={42} />);

  // front hair layer — crown / fringe / locks over the head
  n.push(...drawHairFront(f));

  // eyes
  n.push(px('fg-eyeL', 27, 44, 4, 4, ink, 1));
  n.push(px('fg-eyeR', 38, 44, 4, 4, ink, 1));
  // lashes — a soft feminine cue (tiny outward flicks at the outer eye corners)
  if (f.lashes) {
    n.push(<path d="M26 44 l-2 -1.4" fill="none" key="fg-lashL" stroke={ink} strokeLinecap="round" strokeWidth={1.2} />);
    n.push(<path d="M43 44 l2 -1.4" fill="none" key="fg-lashR" stroke={ink} strokeLinecap="round" strokeWidth={1.2} />);
  }
  // brows — feminine presentations get a thinner/higher brow, masc a heavier one
  const browH = f.presentation === 'fem' ? 1.2 : f.presentation === 'masc' ? 2.1 : 1.6;
  const browY = f.presentation === 'fem' ? 40.4 : 41;
  n.push(px('fg-browL', 26, browY, 6, browH, darken(f.hair, 0.1)));
  n.push(px('fg-browR', 37, browY, 6, browH, darken(f.hair, 0.1)));
  // stubble — faint shading along the jaw for some masc figures
  if (f.hasStubble) {
    n.push(<rect fill={darken(f.skin, 0.45)} fillOpacity={0.28} height={9} key="fg-stubble" rx={6} width={24} x={22} y={52} />);
  }
  // smile
  n.push(<path d="M29 54 Q34 58 39 54" fill="none" key="fg-mouth" stroke={darken(f.skin, 0.35)} strokeLinecap="round" strokeWidth={1.6} />);

  // earrings — small studs/drops at the earlobes
  if (f.hasEarrings) {
    n.push(<circle cx={19} cy={50} fill={t.accent} key="fg-earrL" r={1.7} stroke={darken(t.accent, 0.25)} strokeWidth={0.5} />);
    n.push(<circle cx={49} cy={50} fill={t.accent} key="fg-earrR" r={1.7} stroke={darken(t.accent, 0.25)} strokeWidth={0.5} />);
  }

  // optional glasses
  if (f.hasGlasses) {
    n.push(<rect fill="none" height={7} key="fg-glL" rx={2} stroke={ink} strokeWidth={1.4} width={8} x={25} y={42} />);
    n.push(<rect fill="none" height={7} key="fg-glR" rx={2} stroke={ink} strokeWidth={1.4} width={8} x={36} y={42} />);
    n.push(px('fg-glBridge', 33, 45, 3, 1.4, ink));
  }
  return n;
}

export function RoleAvatar({
  seed,
  size = 72,
  selected = false,
}: {
  seed: string;
  size?: number;
  selected?: boolean;
}): ReactNode {
  const t = useTokens();
  const figure = useMemo(() => figureFor(seed, t), [seed, t]);
  const propKind = PROP_FOR[seed] ?? 'laptop';
  const figureNodes = useMemo(() => drawFigure(figure, t), [figure, t]);
  const propNodes = useMemo(() => drawProp(propKind, t), [propKind, t]);

  return (
    <Box
      sx={{
        width: size,
        height: size,
        flexShrink: 0,
        position: 'relative',
        border: `1.5px solid ${t.hardShadow ? t.shadowColor : t.line}`,
        borderRadius: `${String(Math.min(t.radius, 6))}px`,
        bgcolor: t.paperAlt,
        boxShadow: selected
          ? t.hardShadow
            ? `3px 3px 0 ${t.shadowColor}`
            : `0 6px 16px ${t.shadowColor}`
          : 'none',
        overflow: 'hidden',
        transition: 'box-shadow 120ms ease, transform 120ms ease',
      }}
    >
      <Box
        aria-hidden
        component="svg"
        sx={{ width: '100%', height: '100%', display: 'block', imageRendering: 'pixelated' }}
        viewBox="0 0 100 100"
      >
        {/* faint "screen" wash */}
        <rect fill={t.bg} height="100" opacity={0.55} width="100" x="0" y="0" />
        {/* subtle vignette panel behind the figure */}
        <rect fill={t.paper} height="88" opacity={0.35} rx={t.radius > 4 ? 6 : 0} width="88" x="6" y="6" />
        {figureNodes}
        {propNodes}
      </Box>

      {/* CRT scanlines on hard-shadow themes */}
      {t.hardShadow ? <Box
          sx={{
            position: 'absolute',
            inset: 0,
            pointerEvents: 'none',
            backgroundImage:
              'repeating-linear-gradient(0deg, rgba(0,0,0,0.10) 0 1px, transparent 1px 3px)',
            mixBlendMode: 'multiply',
          }}
        /> : null}
      {/* power LED */}
      <Box
        sx={{
          position: 'absolute',
          bottom: 4,
          right: 4,
          width: 5,
          height: 5,
          borderRadius: '50%',
          bgcolor: t.accent,
          boxShadow: `0 0 4px ${t.accent}`,
        }}
      />
    </Box>
  );
}
