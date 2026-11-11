# GoLab UI v2 — Pure Monochrome Control-Plane Design & Engineering Contract

Binding for every human and agent that touches `ui/`. If code and this file disagree, fix the code.

Verify before reporting done (all must pass):

```bash
cd ui && ./node_modules/.bin/tsc --noEmit      # Gate 5a: strict TypeScript
cd ui && node scripts/check-design.mjs         # Gate 4: R=G=B monochrome + folder boundaries
cd ui && ./node_modules/.bin/vite build        # Gate 5b: production bundle -> ui/dist
node scripts/check-api-contract.mjs            # Gate 3: Go <-> TS API contract
```

---

## 1. Product Role & Core Principles

1. **Control-Plane & Read-Only Observability Console (Zero Colab Runtime Button Duplication).**
   - Google Colab's browser UI and the 43 canonical GoLab MCP tools already control runtime lifecycle (`Connect`, `Disconnect`, `Run All`, `Save`, `Change Accelerator`, `Interrupt`, `Restart`).
   - The GoLab Web UI exists strictly to **govern Accounts, Canonical Notebook Projects, Per-Project Single-Account Exclusive Locks, Session Tokens (`glp_` / `gls_`), Chrome Profile Processes**, and to **observe live telemetry, cells outline, and 100% of MCP tool-call audit payloads in real time**.
2. **100% Pure Neutral Monochrome (`R = G = B`).**
   - Every CSS token (`--c-app`, `--c-canvas`, `--c-surface`, `--c-raised`, `--c-hover`, `--c-line`, `--c-line-strong`, `--c-fg`, `--c-muted`, `--c-subtle`) is a pure gray RGB triple (`R = G = B`) in both Light and Dark themes.
   - Zero chromatic tokens (`primary`, `success`, `warning`, `danger`, `purple`, `accent`, `info`) exist anywhere in CSS, Tailwind, or TSX.
3. **State by Glyph + Monospace Label Only (Zero Hue, Zero Pill Badges).**
   - `●` solid disc: `idle`, `owner`, `editor`, `running_chrome`, `locked`, `success`
   - `◎` double ring: `busy`, `connecting`, `executing`
   - `○` hollow circle: `unlocked`, `runtime_disconnected`, `browser_stopped`, `tab_closed`
   - `△` triangle: `view_only`, `commenter`, `denied`, `auth_required`, `drive_auth_pending`
   - `▪` square: `error`, `corrupted_local_dir`
4. **Event-Driven Real-Time Updates (Zero UI Timer Polling).**
   - Initial hydration loads atomically from `GET /api/v1/workspace` and subscribes to `GET /api/v1/events` typed SSE (`workspace.snapshot`, `profile.updated`, `profile.deleted`, `project.updated`, `project.deleted`, `session.updated`, `session.deleted`, `telemetry.updated`, `activity.appended`).
   - `setInterval` is banned across `ui/src/`.

---

## 2. Domain Model & Canonical Routes

- **Account (`Profile`):** Isolated Chrome `--user-data-dir` profile signed into a Google Account (`id`, `name`, `account_email`, `tier`, `ccu_balance`, `ccu_burn_rate`, `eligible_gpus`, `eligible_tpus`, `cdp_port`, `chrome_pid`, `is_running`).
- **Project:** First-class SQLite WAL entity representing **1 Canonical Colab Notebook** (`id` UUID, `notebook_id`, `notebook_url`, `owner_profile_id`, `project_token` `glp_<32>`, `locked_session_id`).
- **Session:** Binding `(project_id, profile_id)` representing an Account attached to that Project's Notebook (`session_token` `gls_<32>`, `is_locked`, `permission`, `connection_state`). At most **one** Session may be locked per Project at a time; multiple Projects may be locked concurrently.
- **Audit Log (`ActivityLog` / `ActivityDetail`):** Outermost MCP tool-call audit record with `project_id`, `session_id`, `profile_id`, `token_prefix`, `error_code`, `latency_ms`, and full `request` / `response` JSON payloads.

### Hash Routes (`shared/lib/paths.ts`)

| Route | Screen |
|---|---|
| `#/projects` | Projects overview grid + `New Project` |
| `#/projects/<projectId>` | Project Lock Matrix, `glp_`/`gls_` MCP Config, 6-Card Read-Only Telemetry, Cells Snapshot, Recent Tool Calls + Right Split Inspector |
| `#/accounts` | Accounts table (Chrome status, tier, CU balance, sessions) + `New Account` |
| `#/accounts/<profileId>` | Account Chrome controls, Quota & Eligible GPUs/TPUs, Active Colab VMs (`listNotebookSessions`), Attached Project Sessions |
| `#/logs?project=&account=&session=&status=&tool=&q=` | Global faceted MCP tool-call stream (`Load Older` cursor pagination + `Pause/Resume` + Right Split JSON Payload Inspector) |

---

## 3. 3-Column Control-Plane Shell & Modern Web Patterns

1. **Column 1 — Global Rail (`56px` fixed):**
   - Top-level destinations: `Projects`, `Accounts`, `Audit Logs`.
   - Bottom controls: Contextual Sidebar Toggle (`Ctrl/⌘ + B`), Daemon SSE status glyph (`●` online / `▪` reconnecting), and OS-aware Light/Dark monochrome toggle (`<meta name="color-scheme">`).
2. **Column 2 — Contextual Collapsible Sidebar (`240px`, collapsible to `0px` with `inert`):**
   - `Projects`: Searchable UUID-keyed project list with lock glyph (`●` locked + account name, `○` unlocked) + `New Project`.
   - `Accounts`: Chrome profiles with process glyph (`●` running / `○` stopped) + `CU` balance + `New Account`.
   - `Audit Logs`: `<search>` faceted filter form (`Project`, `Account`, `Session`, `Status`, `Tool`, text query).
3. **Column 3 — Fluid Main Workspace + Optional Right Split Inspector (`380px–440px`):**
   - Main workspace establishes `container-type: inline-size` (`cq-inline`) so telemetry grids and cards respond to column width rather than viewport width.
   - Selecting any MCP tool-call row opens the **Right Split Payload Inspector** displaying formatted Request JSON, Response JSON, byte sizes, token prefix, latency, and structured error code/hint.
4. **Native Platform Elements:**
   - Modals use `<dialog>` opened via `showModal()`, `closedby="any"` + pointerdown/pointerup outside-box light-dismiss fallback, and `dialog:modal` / `dialog::backdrop` CSS scroll containment.
   - Read-only Notebook Cells Snapshot uses native `<details>` and `<summary>`.
   - Responsive tables and `<pre><code>` blocks use keyboard-focusable scroll regions (`tabIndex={0}`, `role="region"`, `aria-label`).

---

## 4. Clean Folder Layering

`app` → `features/{projects,accounts,logs}` → `domain/logs` → `shared/{layout,ui,notice,lib}` & `data` → `api`.

- `shared/` MUST NEVER import from `features/`, `domain/`, or `app/`.
- `domain/` MUST NEVER import from `features/` or `app/`.
- A feature (`features/projects`) MUST NEVER import a sibling feature (`features/accounts` or `features/logs`).
