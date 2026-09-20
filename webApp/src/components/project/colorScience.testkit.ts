/**
 * The colour science the palette rules are MEASURED in (designer palette ruling,
 * fix I): CIELAB and its polar LCh, CIEDE2000 colour difference, and the WCAG
 * contrast ratio. Zero imports, so node:test pins it directly: bandRamp.test.ts holds
 * CIEDE2000 to the Sharma, Wu & Dalal (2005) reference pairs, then measures the real
 * theme tokens with it.
 *
 * TEST KIT, NOT PRODUCTION CODE — hence `.testkit.ts`, and hence its home beside its
 * one consumer rather than in `utilities/theme/`, where it shipped in the production
 * layer with no production importer (final main review I6). Nothing the app renders
 * measures colour at runtime: the themes carry literal tokens. If a screen ever needs
 * this maths, move it back to `utilities/` with the importer that justifies it.
 *
 * Every hex is sRGB `#RRGGBB`. Lab is relative to the D65 white.
 */

/** A CIELAB colour: L* (0-100), a*, b*. */
export type Lab = readonly [number, number, number];
/** A CIE LCh(ab) colour: L*, chroma, hue in degrees [0, 360). */
export type Lch = readonly [number, number, number];

/** D65 reference white (2° observer), Y normalised to 1. */
const WHITE: readonly [number, number, number] = [0.95047, 1, 1.08883];

function channels(hex: string): [number, number, number] {
  const m = /^#([0-9a-fA-F]{6})$/.exec(hex);
  if (m?.[1] === undefined) throw new Error(`colorScience: not a #RRGGBB colour: ${hex}`);
  const n = Number.parseInt(m[1], 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

/** An 8-bit sRGB channel to linear light (IEC 61966-2-1). */
function linear(c: number): number {
  const s = c / 255;
  return s <= 0.04045 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
}

/** WCAG relative luminance of a hex colour. */
export function luminance(hex: string): number {
  const [r, g, b] = channels(hex).map(linear) as [number, number, number];
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

/** The WCAG 2 contrast ratio between two hex colours, 1-21, order-free. */
export function contrast(a: string, b: string): number {
  const la = luminance(a);
  const lb = luminance(b);
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
}

/** A hex colour in CIELAB (D65). */
export function lab(hex: string): Lab {
  const [r, g, b] = channels(hex).map(linear) as [number, number, number];
  const x = 0.4124564 * r + 0.3575761 * g + 0.1804375 * b;
  const y = 0.2126729 * r + 0.7151522 * g + 0.072175 * b;
  const z = 0.0193339 * r + 0.119192 * g + 0.9503041 * b;
  const f = (t: number): number => (t > 216 / 24389 ? Math.cbrt(t) : ((24389 / 27) * t + 16) / 116);
  const fx = f(x / WHITE[0]);
  const fy = f(y / WHITE[1]);
  const fz = f(z / WHITE[2]);
  return [116 * fy - 16, 500 * (fx - fy), 200 * (fy - fz)];
}

/** A hex colour in LCh(ab): the hue is atan2(b*, a*) in degrees, [0, 360). */
export function lch(hex: string): Lch {
  const [l, a, b] = lab(hex);
  const h = (Math.atan2(b, a) * 180) / Math.PI;
  return [l, Math.hypot(a, b), h < 0 ? h + 360 : h];
}

const DEG = Math.PI / 180;

/**
 * CIEDE2000 colour difference between two Lab colours (kL = kC = kH = 1), as in
 * Sharma, Wu & Dalal, "The CIEDE2000 Color-Difference Formula: Implementation
 * Notes, Supplementary Test Data, and Mathematical Observations" (2005).
 */
export function deltaE2000(one: Lab, two: Lab): number {
  const [l1, a1, b1] = one;
  const [l2, a2, b2] = two;
  const cBar = (Math.hypot(a1, b1) + Math.hypot(a2, b2)) / 2;
  const g = 0.5 * (1 - Math.sqrt(cBar ** 7 / (cBar ** 7 + 25 ** 7)));
  const ap1 = (1 + g) * a1;
  const ap2 = (1 + g) * a2;
  const cp1 = Math.hypot(ap1, b1);
  const cp2 = Math.hypot(ap2, b2);
  const hue = (b: number, ap: number): number => {
    if (b === 0 && ap === 0) return 0;
    const h = Math.atan2(b, ap) / DEG;
    return h < 0 ? h + 360 : h;
  };
  const hp1 = hue(b1, ap1);
  const hp2 = hue(b2, ap2);

  const dL = l2 - l1;
  const dC = cp2 - cp1;
  let dh = 0;
  if (cp1 * cp2 !== 0) {
    dh = hp2 - hp1;
    if (dh > 180) dh -= 360;
    else if (dh < -180) dh += 360;
  }
  const dH = 2 * Math.sqrt(cp1 * cp2) * Math.sin((dh / 2) * DEG);

  const lBar = (l1 + l2) / 2;
  const cpBar = (cp1 + cp2) / 2;
  let hBar = hp1 + hp2;
  if (cp1 * cp2 !== 0) {
    if (Math.abs(hp1 - hp2) <= 180) hBar /= 2;
    else hBar = hp1 + hp2 < 360 ? (hBar + 360) / 2 : (hBar - 360) / 2;
  }
  const t =
    1 -
    0.17 * Math.cos((hBar - 30) * DEG) +
    0.24 * Math.cos(2 * hBar * DEG) +
    0.32 * Math.cos((3 * hBar + 6) * DEG) -
    0.2 * Math.cos((4 * hBar - 63) * DEG);
  const dTheta = 30 * Math.exp(-(((hBar - 275) / 25) ** 2));
  const rC = 2 * Math.sqrt(cpBar ** 7 / (cpBar ** 7 + 25 ** 7));
  const sL = 1 + (0.015 * (lBar - 50) ** 2) / Math.sqrt(20 + (lBar - 50) ** 2);
  const sC = 1 + 0.045 * cpBar;
  const sH = 1 + 0.015 * cpBar * t;
  const rT = -Math.sin(2 * dTheta * DEG) * rC;
  return Math.sqrt((dL / sL) ** 2 + (dC / sC) ** 2 + (dH / sH) ** 2 + rT * (dC / sC) * (dH / sH));
}
