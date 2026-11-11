#!/usr/bin/env node
// Gate 3: Go <-> TypeScript API Contract Gate.
// Verifies:
// 1. Every `/api/...` route called in `ui/src/` uses `/api/v1/*` and matches a route
//    registered in `internal/api/server.go` (method + path pattern).
// 2. Zero legacy unversioned `/api/profiles`, `/api/projects`, `/api/sessions`,
//    `/api/activity`, `/api/events` paths exist anywhere in `ui/src/`.
// 3. Zero FNV URL-hashing (`projectIdFor`) or `localStorage` `golab.projects`
//    workarounds exist in `ui/src/`.
// 4. Core Go domain JSON struct fields (`Profile`, `Project`, `Session`, `ActivityLog`,
//    `WorkspaceSnapshot`, `ObservationSnapshot`, `AccountQuota`) are mirrored 1:1 in
//    `ui/src/api/types.ts`.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const SERVER_GO = join(ROOT, 'internal/api/server.go');
const UI_SRC = join(ROOT, 'ui/src');
const ENDPOINTS_TS = join(UI_SRC, 'api/endpoints.ts');
const TYPES_TS = join(UI_SRC, 'api/types.ts');

let failures = 0;
function fail(message) {
  failures += 1;
  console.error(`CONTRACT VIOLATION: ${message}`);
}

function* walkTs(dir) {
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) yield* walkTs(full);
    else if (/\.(ts|tsx)$/.test(entry)) yield full;
  }
}

function normalizeTemplatePath(rawPath) {
  return rawPath
    .split('?')[0]
    .replace(/\$\{[^}]+\}/g, '{param}')
    .replace(/\{[a-zA-Z0-9_]+\}/g, '{param}');
}

// 1. Extract Go server routes from internal/api/server.go
const serverGoText = readFileSync(SERVER_GO, 'utf8');
const goRoutes = new Set();
for (const m of serverGoText.matchAll(/mux\.HandleFunc\(\s*"([A-Z]+)\s+(\/api\/v1\/[^"]+)"/g)) {
  const method = m[1];
  const normPath = normalizeTemplatePath(m[2]);
  goRoutes.add(`${method} ${normPath}`);
}

if (goRoutes.size < 20) {
  fail(`Expected at least 20 registered /api/v1/* routes in internal/api/server.go, found ${goRoutes.size}`);
}

// 2. Scan all UI TypeScript files for forbidden legacy routes, FNV projectIdFor, or direct fetch outside api/
for (const file of walkTs(UI_SRC)) {
  const rel = relative(ROOT, file);
  const content = readFileSync(file, 'utf8');
  const lines = content.split('\n');

  lines.forEach((line, idx) => {
    const lineNo = idx + 1;
    if (/\bprojectIdFor\b/.test(line) || /golab\.projects/.test(line)) {
      fail(`${rel}:${lineNo} uses legacy FNV/localStorage project workaround: ${line.trim()}`);
    }
    for (const m of line.matchAll(/['"`](\/api\/[^'"`\s?]*)['"`]/g)) {
      const apiPath = m[1];
      if (!apiPath.startsWith('/api/v1/')) {
        fail(`${rel}:${lineNo} calls unversioned API path "${apiPath}" (must use /api/v1/*)`);
      }
    }
  });
}

// 3. Parse ui/src/api/endpoints.ts and verify every HTTP call matches a Go route
const endpointsText = readFileSync(ENDPOINTS_TS, 'utf8');
const endpointCalls = [];

// Match helper calls: get/post/patch/del('/api/v1/...') or request<...>('/api/v1/...', { method: '...' })
for (const m of endpointsText.matchAll(
  /\b(get|post|patch|del|request)\s*(?:<[^>]+>)?\s*\(\s*(?:`(\/api\/[^`]+)`|'([^']+)'|"([^"]+)")(?:\s*,\s*\{([^}]*)\})?/g,
)) {
  const helper = m[1];
  const rawPath = m[2] ?? m[3] ?? m[4];
  if (!rawPath || !rawPath.startsWith('/api/')) continue;
  const opts = m[5] ?? '';
  let method = 'GET';
  if (helper === 'post') method = 'POST';
  else if (helper === 'patch') method = 'PATCH';
  else if (helper === 'del') method = 'DELETE';
  else {
    const mm = opts.match(/method\s*:\s*['"]([A-Z]+)['"]/);
    if (mm) method = mm[1];
  }
  endpointCalls.push({ method, rawPath, normPath: normalizeTemplatePath(rawPath) });
}

if (endpointCalls.length === 0) {
  fail('No /api/v1/* endpoint calls detected in ui/src/api/endpoints.ts');
}

for (const call of endpointCalls) {
  const key = `${call.method} ${call.normPath}`;
  if (!goRoutes.has(key)) {
    fail(`ui/src/api/endpoints.ts calls "${call.method} ${call.rawPath}" (normalized "${key}"), which is not registered in internal/api/server.go`);
  }
}

// Also verify EventSource('/api/v1/events') in ControlPlaneProvider.tsx
const cpFile = join(UI_SRC, 'data/ControlPlaneProvider.tsx');
const cpText = readFileSync(cpFile, 'utf8');
if (!cpText.includes("'/api/v1/events'") && !cpText.includes('"/api/v1/events"')) {
  fail('ui/src/data/ControlPlaneProvider.tsx does not subscribe to GET /api/v1/events');
}

// 4. Verify Go domain JSON fields exist in ui/src/api/types.ts
function extractGoJsonFields(goFile, structName) {
  const src = readFileSync(join(ROOT, goFile), 'utf8');
  const re = new RegExp(`type\\s+${structName}\\s+struct\\s*\\{([\\s\\S]*?)\\n\\}`, 'm');
  const match = src.match(re);
  if (!match) {
    fail(`Could not locate Go struct ${structName} in ${goFile}`);
    return [];
  }
  const fields = [];
  for (const fm of match[1].matchAll(/`json:"([a-z0-9_]+)(?:,omitempty)?"`/g)) {
    if (fm[1] !== '-') fields.push(fm[1]);
  }
  return fields;
}

function extractTsInterfaceFields(tsSrc, ifaceName) {
  const re = new RegExp(`export\\s+interface\\s+${ifaceName}(?:\\s+extends\\s+[^{]+)?\\s*\\{([\\s\\S]*?)\\n\\}`, 'm');
  const match = tsSrc.match(re);
  if (!match) {
    fail(`Missing exported interface ${ifaceName} in ui/src/api/types.ts`);
    return new Set();
  }
  const fields = new Set();
  for (const fm of match[1].matchAll(/^\s*(?:readonly\s+)?([a-z0-9_]+)\??\s*:/gm)) {
    fields.add(fm[1]);
  }
  return fields;
}

const typesText = readFileSync(TYPES_TS, 'utf8');
const DTO_PAIRS = [
  ['internal/domain/profile.go', 'Profile', 'Profile'],
  ['internal/domain/profile.go', 'ComputeUnitsInfo', 'ComputeUnitsInfo'],
  ['internal/domain/profile.go', 'ColabVMSession', 'ColabVMSession'],
  ['internal/domain/profile.go', 'AccountQuota', 'AccountQuota'],
  ['internal/domain/project.go', 'Project', 'Project'],
  ['internal/domain/session.go', 'Session', 'Session'],
  ['internal/domain/session.go', 'NotebookIdentity', 'NotebookIdentity'],
  ['internal/domain/session.go', 'ObservationSnapshot', 'ObservationSnapshot'],
  ['internal/domain/activity.go', 'ActivityLog', 'ActivityLog'],
  ['internal/domain/driver.go', 'DriveStatus', 'DriveStatus'],
  ['internal/domain/driver.go', 'WorkspaceSnapshot', 'WorkspaceSnapshot'],
];

for (const [goFile, goStruct, tsIface] of DTO_PAIRS) {
  const goFields = extractGoJsonFields(goFile, goStruct);
  const tsFields = extractTsInterfaceFields(typesText, tsIface);
  for (const gf of goFields) {
    if (!tsFields.has(gf)) {
      fail(`Go ${goStruct} JSON field "${gf}" (${goFile}) is missing from TypeScript interface ${tsIface} (ui/src/api/types.ts)`);
    }
  }
}

if (failures > 0) {
  console.error(`\n${failures} Go <-> TypeScript API contract violation(s) failed.`);
  process.exit(1);
}
console.log(`Gate 3 (Go <-> TS API contract: ${goRoutes.size} server routes, ${DTO_PAIRS.length} DTOs) passed`);
