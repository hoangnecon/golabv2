/**
 * Design tokens live here and in src/index.css (CSS variables). The theme is
 * REPLACED rather than extended: raw palette colors, chromatic tokens, extra
 * font sizes, radii, and shadows do not exist. Every color token resolves to a
 * pure neutral gray triple (R = G = B). Enforced by scripts/check-design.mjs.
 */
const token = (name) => `rgb(var(--c-${name}) / <alpha-value>)`;

/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    colors: {
      transparent: 'transparent',
      current: 'currentColor',
      app: token('app'),
      canvas: token('canvas'),
      surface: token('surface'),
      raised: token('raised'),
      hover: token('hover'),
      line: token('line'),
      'line-strong': token('line-strong'),
      fg: token('fg'),
      muted: token('muted'),
      subtle: token('subtle'),
      scrim: 'rgb(0 0 0 / 0.6)',
    },
    fontSize: {
      caption: ['0.75rem', { lineHeight: '1rem' }],
      body: ['0.875rem', { lineHeight: '1.25rem' }],
      title: ['1rem', { lineHeight: '1.5rem' }],
      display: ['1.5rem', { lineHeight: '2rem', letterSpacing: '-0.015em' }],
    },
    borderRadius: {
      none: '0',
      DEFAULT: '0.5rem',
      lg: '0.75rem',
      xl: '1rem',
      full: '9999px',
    },
    boxShadow: {
      none: 'none',
      overlay: '0 16px 40px rgb(0 0 0 / 0.32), 0 0 0 1px rgb(var(--c-line-strong))',
    },
    fontFamily: {
      sans: [
        '"IBM Plex Sans"',
        'ui-sans-serif',
        'system-ui',
        '-apple-system',
        'Segoe UI',
        'sans-serif',
      ],
      mono: [
        '"JetBrains Mono"',
        'ui-monospace',
        'SFMono-Regular',
        'Menlo',
        'Consolas',
        'monospace',
      ],
    },
    extend: {},
  },
  plugins: [],
};
