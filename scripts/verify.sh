#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

log_gate() {
  printf "\n[%s] === %s ===\n" "$(date '+%H:%M:%S')" "$1"
}

START_TS="$(date +%s)"
printf "[%s] Starting GoLab v2 6-Gate Verification Suite in %s\n" "$(date '+%H:%M:%S')" "$ROOT_DIR"
printf "[%s] Plan: Gate 1 (AST) -> Gate 2 (Go Race) -> Gate 3 (API Contract) -> Gate 4 (Monochrome UI) -> Gate 5 (UI Build + Embed) -> Gate 6 (Real Binary + Headless Chrome E2E)\n" "$(date '+%H:%M:%S')"

log_gate "Gate 1/6: Go Clean Architecture AST Import Boundaries"
go test -count=1 -v -run 'TestDomainZeroInternalImports|TestCleanArchitectureImports' ./internal

log_gate "Gate 2/6: Go Unit & Integration Tests (-race)"
go test -race -count=1 ./internal/... ./cmd/...

log_gate "Gate 3/6: Go <-> TypeScript API Contract Verification"
node scripts/check-api-contract.mjs

log_gate "Gate 4/6: Strict Monochrome (R=G=B) & UI Architecture Gate"
(cd ui && node scripts/check-design.mjs)

log_gate "Gate 5/6: UI TypeScript Check, Vite Production Build & Go Binary Embed"
(cd ui && ./node_modules/.bin/tsc --noEmit && ./node_modules/.bin/vite build)
TMP_BIN="$(mktemp "${TMPDIR:-/tmp}/golab-v2-verify.XXXXXX")"
trap 'rm -f "$TMP_BIN"' EXIT
go build -o "$TMP_BIN" ./cmd/server
printf "[%s] Verified embedded Go binary: %s (%s bytes)\n" "$(date '+%H:%M:%S')" "$TMP_BIN" "$(wc -c < "$TMP_BIN" | tr -d ' ')"

log_gate "Gate 6/6: Real Binary + 2-Account Mock CDP + Headless Chrome E2E Suite"
go test -race -count=1 -v ./e2e/...

END_TS="$(date +%s)"
ELAPSED="$((END_TS - START_TS))"
printf "\n[%s] SUMMARY: All 6 GoLab v2 verification gates PASSED in %ss (UI bundle: %s/ui/dist).\n" "$(date '+%H:%M:%S')" "$ELAPSED" "$ROOT_DIR"
