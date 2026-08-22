/** WCAG 2 contrast helpers for the light and dark themes. */

export function hexToRgb(hex: string): [number, number, number] {
  const raw = hex.replace("#", "").trim();
  if (raw.length !== 6) {
    throw new Error(`contrast: expected 6-digit hex, got ${hex}`);
  }
  const n = Number.parseInt(raw, 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

function channel(c: number): number {
  const s = c / 255;
  return s <= 0.04045 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
}

export function relativeLuminance(hex: string): number {
  const [r, g, b] = hexToRgb(hex);
  return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
}

export function contrastRatio(fg: string, bg: string): number {
  const l1 = relativeLuminance(fg);
  const l2 = relativeLuminance(bg);
  const lighter = Math.max(l1, l2);
  const darker = Math.min(l1, l2);
  return (lighter + 0.05) / (darker + 0.05);
}

export const AA_NORMAL = 4.5;
export const AA_LARGE = 3;

export type ThemeTokens = {
  bg: string;
  text: string;
  muted: string;
  pending: string;
  pendingBg: string;
  memory: string;
  memoryBg: string;
  danger: string;
  dangerBg: string;
  ok: string;
  okBg: string;
  sigBg: string;
  sigFg: string;
};

export const lightTheme: ThemeTokens = {
  bg: "#f4f0e6",
  text: "#1a1610",
  muted: "#5c564c",
  pending: "#8a5800",
  pendingBg: "#f8e4b8",
  memory: "#0a3a8c",
  memoryBg: "#dbe7ff",
  danger: "#b42318",
  dangerBg: "#fde4e1",
  ok: "#17663a",
  okBg: "#d8f3e3",
  sigBg: "#161410",
  sigFg: "#f3ead4",
};

export const darkTheme: ThemeTokens = {
  bg: "#12110e",
  text: "#f3efe6",
  muted: "#b7b0a3",
  pending: "#f5c518",
  pendingBg: "#3a2d0a",
  memory: "#8bb4ff",
  memoryBg: "#1c2d4d",
  danger: "#ff7a70",
  dangerBg: "#3b1614",
  ok: "#5dcaa0",
  okBg: "#123226",
  sigBg: "#161410",
  sigFg: "#f3ead4",
};

export function themePairs(theme: ThemeTokens): Array<[string, string, number]> {
  return [
    [theme.text, theme.bg, AA_NORMAL],
    [theme.muted, theme.bg, AA_NORMAL],
    [theme.pending, theme.pendingBg, AA_NORMAL],
    [theme.memory, theme.memoryBg, AA_NORMAL],
    [theme.danger, theme.dangerBg, AA_NORMAL],
    [theme.ok, theme.okBg, AA_NORMAL],
    [theme.sigFg, theme.sigBg, AA_NORMAL],
  ];
}
