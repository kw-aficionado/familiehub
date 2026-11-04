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
- Home previews at most four chores; its explicit shortcut opens the complete checklist for the shown day, retaining the selected person and resetting stale status filters.
- Tasks opens on today's Copenhagen date; Today, This week and All tasks remain distinct.
- AI task planner belongs in Settings immediately below Task archive, not on the Tasks page.
- Planner start date defaults to today in Europe/Copenhagen; any weekday is valid. No occurrences, completion or reminders before that start.
- Display the shorter first week's effort and the first rotation date. Rotation follows calendar weeks/Mondays.
- Balance effort points and time, not only the number of chore titles. Dishwashing is heavier than taking out rubbish.
- Deterministic code owns recurrence, effort calculations and persistence. AI is optional and explains; it is not an autonomous database writer.
- Preview and explicit approval precede plan persistence. Keep failures visible and never present a failed refresh as a failed save.
- Default automatic screensaver window is 20:00–07:00 Copenhagen time, after inactivity. Settings are per device, independent of dark theme; preserve Never/disabled preferences.
- Reuse the existing house icon; do not redesign branding unless requested.

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
