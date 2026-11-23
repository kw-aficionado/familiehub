# FamilieHub — instructions for coding agents

## Start here
- Read `README.md` and `docs/CODEX.md` before changing the app.
- This is the existing `kw-aficionado/familiehub` project, not a new app or rewrite.
- Communicate with the owner in Danish. Keep user-facing UI text in Danish.
- Inspect the current branch, status and relevant tests; do not overwrite unrelated work.

## Architecture and setup
- Go module at the repository root; Go 1.23+ with CGO, a C compiler and SQLite development headers.
- The backend embeds `web/*`; frontend is plain HTML/CSS/JavaScript, no production npm dependencies.
- Maintain iPadOS 15-compatible frontend syntax and behavior. Node is for tests, not the deployed app.
- Run `sh scripts/setup-dev.sh` to check tools and download Go modules.
- Run `sh scripts/check.sh` for Go tests, race tests, Node tests, JS syntax and a temporary build.
- Tests and screenshots must use disposable data, never a production database.

## Product decisions to preserve
- Home opens in Day view. Week remains available.
- Both entrypoints use web/day-rollover.js for Copenhagen midnight and wake recovery. Only today/current-week views follow time; retain deliberately selected other dates, filters, login and unsaved forms. Do not replace this with page reloads, 24-hour timers or synthetic task writes. Bind completion actions to the day actually rendered, and invalidate pre-rollover responses.
- Home previews at most four chores; its explicit shortcut opens the complete checklist for the shown day, retaining the selected person and resetting stale status filters.
- Tasks opens on today's Copenhagen date; Today, This week and All tasks remain distinct.
- AI task planner belongs in Settings immediately below Task archive, not on the Tasks page.
- Planner start date defaults to today in Europe/Copenhagen; any weekday is valid. No occurrences, completion or reminders before that start.
- Display the shorter first week's effort and the first rotation date. Rotation uses the week-start saved in each plan (Monday by default). Global week-start changes apply to navigation and new plans, never silently rewrite old completions.
- Balance effort points and time, not only the number of chore titles. Dishwashing is heavier than taking out rubbish.
- Deterministic code owns recurrence, effort calculations and persistence. AI is optional and explains; it is not an autonomous database writer.
- Quickstart supports one shared rotating task series with an explicit starter, or deliberately separate personal tasks for each member. Never deduplicate solely by title.
- Existing series may be stopped from today/future only after explicit selection and review; never delete completion history as duplicate cleanup.
- Preview and explicit approval precede plan persistence. Keep failures visible and never present a failed refresh as a failed save.
- Default automatic screensaver window is 20:00–07:00 Copenhagen time, after inactivity. Settings are per device, independent of dark theme; preserve Never/disabled preferences.
- Reuse the existing house icon; do not redesign branding unless requested.

## Tablet access
- `/tablet` is a separate, limited browser interface; `/api/tablet/*` never falls back to an ordinary administrator session.
- Preserve admin-only management, one-use ten-minute pairing, hashed credentials, scoped cookies and atomic revocation/completion checks.
- Pairing exits only the ordinary session on that browser. Never remove other family sessions implicitly.
- Tablet today ticks and visible overdue followup actions are separate opt-in scopes. Followup never grants arbitrary historical edits, adult decisions, planning or administration.
- Display filters are preferences, not an authorization/privacy boundary. Do not project connection URLs, notes or notification credentials into tablet responses.
- Keep both 30-day idle and 180-day absolute expiry, HTTPS and CSRF protections; never replace them with permanent login URLs.
- Auth throttling uses account failure limits plus a separate generous network limit. Blocked attempts never extend the wait; do not blindly trust proxy IP headers.

## Data and security
- Preserve `/data`, `familiehub.sqlite` and `session.key`. Never Wipe or replace a family database as a shortcut.
- Database migrations must be additive and preserve completion history and existing accounts.
- Do not commit passwords, tokens, `.env` values, database files, session keys, private calendars or live-family screenshots.
- Preserve auth, CSRF, admin checks, input validation, safe error handling and SQL parameter binding.
- External calendar integrations are read-only; do not add remote calendar writes implicitly.
- MCP access is opt-in and scoped. Never broaden it to arbitrary SQL, secrets or unrestricted administration.
- MCP plan moves require a fresh preview, explicit approval, conflict checks, atomic writes and idempotent receipts; do not rewrite completed history.

## Verification and delivery
- Add regression coverage for behavior changes. For date work, cover midweek starts, Monday rotation, timezone boundaries and legacy tasks.
- For UI changes, run Node tests and inspect real browser screenshots, including a narrow phone viewport.
- `python3 scripts/capture-screenshots.py` builds the real app and creates fictional data in a new temporary SQLite database. It cannot target a live server.
- Update affected docs and screenshots. Screenshot provenance is in `docs/screenshots/manifest.json`.
- Keep changes focused. Do not change Rune format, mutable release tag or deployment architecture without a specific request.
- Use a feature branch and PR; do not force-push or discard work. Merge/publish only when requested or covered by the owner's explicit instruction.
- A merge to `main` triggers Docker publication. A successful CI test is not proof of image publication or live deployment.
- Report separately: code committed, tests run, PR/merge state, Docker publication, and whether the live server was actually updated.
- Never claim live MCP access, a Codex project registration, deployment, or device verification that has not been performed.

## Missed chores
- Followup is opt-in from an explicit date, stored as occurrences, never copied task series.
- Preserve original deadline/owner/effort and all audit records. Rotation does not transfer yesterday's residual work.
- Policies date/routine/carry apply per series; do not deduplicate by title. New policies do not rewrite prior snapshots.
- Excused and superseded are not completed. Legacy ticks without time are completed_unknown, not invented on-time performance.
- Synchronize ledger writes, ticks, policies and retirement in the same transaction domain. Require version and idempotency checks for followup actions.
- Do not silently enable notification delivery, tablet followup scope, or backfill before the chosen activation date.
