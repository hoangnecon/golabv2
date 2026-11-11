#!/usr/bin/env node
// Gate 4: Enforces ../DESIGN.md v2 (100% Pure Neutral Monochrome R=G=B,
// Read-Only Observability UI, Zero Polling, Clean UI Folder Layering).
// Exit code 1 on any violation.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join, relative, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

const UI_DIR = join(dirname(fileURLToPath(import.meta.url)), '..');
const SRC_DIR = join(UI_DIR, 'src');
const TAILWIND_CONFIG = join(UI_DIR, 'tailwind.config.js');

const BANNED_TOKEN_NAMES = new Set([
  'primary',
  'secondary',
  'success',
  'warning',
  'danger',
  'error',
  'info',
  'accent',
  'on-accent',
  'purple',
  'violet',
  'indigo',
  'blue',
  'sky',
  'cyan',
  'teal',
  'emerald',
  'green',
  'lime',
  'yellow',
  'amber',
  'orange',
  'red',
  'rose',
  'pink',
  'fuchsia',
  'sidebar',
]);

const LINE_RULES = [
  {
    name: 'raw palette color (use monochrome tokens: app, canvas, surface, raised, hover, line, line-strong, fg, muted, subtle)',
    pattern:
      /\b(?:bg|text|border|ring|outline|divide|fill|stroke|from|to|via|shadow|placeholder|decoration|accent|caret)-(?:slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose|black|white)\b/,
  },
  {
    name: 'banned chromatic token utility',
    pattern:
      /\b(?:bg|text|border|ring|outline|divide|fill|stroke)-(?:primary|secondary|success|warning|danger|error|info|accent|on-accent|purple|sidebar)\b/,
  },
  {
    name: 'hex or color function literal in TypeScript/TSX source (colors belong only in index.css neutral tokens)',
    pattern: /#[0-9a-fA-F]{3,8}\b|\b(?:rgba?|hsla?|oklch|oklab|lch|lab)\(/,
    tsOnly: true,
  },
  {
    name: 'non-neutral color function in CSS (use rgb() or --c-* neutral triples only)',
    pattern: /\b(?:hsla?|oklch|oklab|lch|lab)\(/,
    cssOnly: true,
  },
  {
    name: 'arbitrary Tailwind value `[...]`',
    pattern: /\b[a-z-]+-\[[^\]]+\]/,
  },
  {
    name: 'font size outside caption|body|title|display',
    pattern: /\btext-(?:xs|sm|base|lg|xl|[2-9]xl)\b/,
  },
  {
    name: 'radius other than `rounded-none` / `rounded` / `rounded-lg` / `rounded-xl` / `rounded-full`',
    pattern: /\brounded-(?:sm|md|2xl|3xl)\b/,
  },
  {
    name: 'shadow other than `shadow-none` / `shadow-overlay`',
    pattern: /\bshadow-(?:sm|md|lg|xl|2xl|inner)\b/,
  },
  {
    name: 'centered page container (layout is fluid and left-aligned)',
    pattern: /\bmx-auto\b/,
  },
  {
    name: 'gradient / blur / glass effect',
    pattern: /\b(?:bg-gradient-|backdrop-blur|blur-)/,
  },
  {
    name: 'pulse / bounce / ping / spin animation',
    pattern: /\banimate-(?:pulse|bounce|ping|spin)\b/,
  },
  {
    name: 'UI timer polling `setInterval` (live state must arrive via `/api/v1/events` SSE)',
    pattern: /\bsetInterval\s*\(/,
  },
  {
    name: 'Colab-duplicating runtime mutation endpoint in UI (runtime controls belong to Colab UI / MCP agent)',
    pattern:
      /\/api(?:\/v1)?\/sessions\/[^'"`\s]*\/(?:connect|disconnect|runall|run-all|save|accelerator|accelerators|interrupt|restart|running-cells)\b/,
  },
  {
    name: 'native alert/confirm/prompt',
    pattern: /\b(?:alert|confirm|prompt)\(/,
    skip: /useConfirm|confirm\(\{/,
  },
  {
    name: 'console logging in UI source',
    pattern: /\bconsole\.(?:log|debug|info)\b/,
  },
  {
    name: '`any` type in TypeScript',
    pattern: /:\s*any\b|\bas any\b|<any>/,
    tsOnly: true,
  },
];

function* walkFiles(dir) {
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) {
      yield* walkFiles(full);
    } else if (/\.(ts|tsx|css)$/.test(entry)) {
      yield full;
    }
  }
}

function parseHexToRgb(hex) {
  const raw = hex.slice(1);
  if (raw.length === 3 || raw.length === 4) {
    return [
      Number.parseInt(raw[0] + raw[0], 16),
      Number.parseInt(raw[1] + raw[1], 16),
      Number.parseInt(raw[2] + raw[2], 16),
    ];
  }
  if (raw.length === 6 || raw.length === 8) {
    return [
      Number.parseInt(raw.slice(0, 2), 16),
      Number.parseInt(raw.slice(2, 4), 16),
      Number.parseInt(raw.slice(4, 6), 16),
    ];
  }
  return null;
}

let violations = 0;
function report(file, lineNo, message, snippet) {
  violations += 1;
  const rel = relative(UI_DIR, file);
  console.error(`${rel}:${lineNo}  ${message}\n    ${snippet.trim()}`);
}

// 1. Validate tailwind.config.js contains zero chromatic tokens or non-neutral RGB literals.
const twText = readFileSync(TAILWIND_CONFIG, 'utf8');
twText.split('\n').forEach((line, idx) => {
  const tokenCall = line.match(/token\(\s*['"]([a-z0-9-]+)['"]\s*\)/);
  if (tokenCall && BANNED_TOKEN_NAMES.has(tokenCall[1])) {
    report(TAILWIND_CONFIG, idx + 1, `banned chromatic token "${tokenCall[1]}" in tailwind.config.js`, line);
  }
  const keyMatch = line.match(/^\s*['"]?([a-z0-9-]+)['"]?\s*:\s*token\(/);
  if (keyMatch && BANNED_TOKEN_NAMES.has(keyMatch[1])) {
    report(TAILWIND_CONFIG, idx + 1, `banned color key "${keyMatch[1]}" in tailwind.config.js`, line);
  }
  for (const m of line.matchAll(/\brgba?\(\s*(\d{1,3})[\s,]+(\d{1,3})[\s,]+(\d{1,3})\b/g)) {
    const [r, g, b] = [Number(m[1]), Number(m[2]), Number(m[3])];
    if (r !== g || g !== b) {
      report(TAILWIND_CONFIG, idx + 1, `non-monochrome RGB (${r}, ${g}, ${b}) — must satisfy R = G = B`, line);
    }
  }
  for (const m of line.matchAll(/#[0-9a-fA-F]{3,8}\b/g)) {
    const rgb = parseHexToRgb(m[0]);
    if (rgb && (rgb[0] !== rgb[1] || rgb[1] !== rgb[2])) {
      report(TAILWIND_CONFIG, idx + 1, `non-monochrome hex ${m[0]} — must satisfy R = G = B`, line);
    }
  }
});

// 2. Validate every file in ui/src/**/*.{ts,tsx,css}.
for (const file of walkFiles(SRC_DIR)) {
  const isCss = file.endsWith('.css');
  const relFromSrc = relative(SRC_DIR, file).split(sep).join('/');
  const lines = readFileSync(file, 'utf8').split('\n');

  lines.forEach((line, idx) => {
    const lineNo = idx + 1;

    for (const rule of LINE_RULES) {
      if (rule.tsOnly && isCss) continue;
      if (rule.cssOnly && !isCss) continue;
      if (rule.skip?.test(line)) continue;
      if (rule.pattern.test(line)) {
        report(file, lineNo, rule.name, line);
      }
    }

    // Strict R = G = B verification on CSS variables and rgb/hex literals in CSS.
    if (isCss) {
      const varDef = line.match(/--c-([a-z0-9-]+)\s*:\s*(\d{1,3})\s+(\d{1,3})\s+(\d{1,3})\s*;/);
      if (varDef) {
        const [, name, rs, gs, bs] = varDef;
        if (BANNED_TOKEN_NAMES.has(name)) {
          report(file, lineNo, `banned chromatic CSS token --c-${name}`, line);
        }
        const [r, g, b] = [Number(rs), Number(gs), Number(bs)];
        if (r !== g || g !== b) {
          report(file, lineNo, `CSS token --c-${name} (${r} ${g} ${b}) violates pure neutral monochrome R = G = B`, line);
        }
      }

      for (const m of line.matchAll(/\brgba?\(\s*(\d{1,3})[\s,]+(\d{1,3})[\s,]+(\d{1,3})\b/g)) {
        const [r, g, b] = [Number(m[1]), Number(m[2]), Number(m[3])];
        if (r !== g || g !== b) {
          report(file, lineNo, `CSS rgb(${r}, ${g}, ${b}) violates pure neutral monochrome R = G = B`, line);
        }
      }

      for (const m of line.matchAll(/#[0-9a-fA-F]{3,8}\b/g)) {
        const rgb = parseHexToRgb(m[0]);
        if (rgb && (rgb[0] !== rgb[1] || rgb[1] !== rgb[2])) {
          report(file, lineNo, `CSS hex ${m[0]} violates pure neutral monochrome R = G = B`, line);
        }
      }
    }

    // Clean UI folder boundary checks on import statements.
    const importMatch = line.match(/^\s*import\s+(?:type\s+)?(?:[^'"]*from\s+)?['"]([^'"]+)['"]/);
    if (importMatch) {
      const spec = importMatch[1];
      if (relFromSrc.startsWith('shared/')) {
        if (
          spec.startsWith('@/features/') ||
          spec.startsWith('@/domain/') ||
          spec.startsWith('@/app/') ||
          spec.includes('../features/') ||
          spec.includes('../domain/') ||
          spec.includes('../app/')
        ) {
          report(file, lineNo, 'layering violation: `shared/` must never import from `features/`, `domain/`, or `app/`', line);
        }
      }
      if (relFromSrc.startsWith('domain/')) {
        if (
          spec.startsWith('@/features/') ||
          spec.startsWith('@/app/') ||
          spec.includes('../features/') ||
          spec.includes('../app/')
        ) {
          report(file, lineNo, 'layering violation: `domain/` must never import from `features/` or `app/`', line);
        }
      }
      if (relFromSrc.startsWith('features/')) {
        const ownFeature = relFromSrc.split('/')[1];
        const crossAlias = spec.match(/^@\/features\/([^/]+)/);
        if (crossAlias && crossAlias[1] !== ownFeature) {
          report(
            file,
            lineNo,
            `layering violation: feature \`${ownFeature}\` must not import sibling feature \`${crossAlias[1]}\``,
            line,
          );
        }
      }
    }
  });
}

if (violations > 0) {
  console.error(`\n${violations} design/architecture violation(s). See ui/DESIGN.md.`);
  process.exit(1);
}
console.log('Gate 4 (check:design v2 monochrome + architecture) passed');
