// Branch colors, 12 per theme. Every color keeps a contrast ratio of at least 3
// against the background of its theme.
export const PALETTE: { dark: string[]; light: string[] } = {
  dark: [
    '#5aa9ff', '#ff7a7a', '#5fd38d', '#f2c14e', '#c58bff', '#4fd1c5',
    '#ff9f5a', '#ff7ac6', '#a3d65c', '#8f9bff', '#e0a46c', '#6ec6e8',
  ],
  light: [
    '#1f6fd1', '#d12f2f', '#1c8a4c', '#a86a00', '#7b3fc4', '#0b8579',
    '#c25a00', '#c21f88', '#5a7d0a', '#4a4fd6', '#8a5a28', '#0a6f94',
  ],
};

export const BG = { dark: '#1e1f22', light: '#ffffff' };

function channel(v: number): number {
  const s = v / 255;
  return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
}

function luminance(hex: string): number {
  const n = parseInt(hex.replace('#', ''), 16);
  return 0.2126 * channel((n >> 16) & 255) + 0.7152 * channel((n >> 8) & 255) + 0.0722 * channel(n & 255);
}

// contrast is the WCAG contrast ratio of two #rrggbb colors, from 1 to 21.
export function contrast(a: string, b: string): number {
  const la = luminance(a);
  const lb = luminance(b);
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
}
