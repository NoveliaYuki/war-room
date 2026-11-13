Version 0.5.0 — 2026-11-13 · Release: [v0.5.0](https://github.com/NoveliaYuki/war-room/releases/tag/v0.5.0)

# Highlights
- Export the board as a CSV file for Excel, Google Sheets, or Numbers from **Data & Backups**, signed in and in the browser demo. Headers, statuses, work arrangements, employment types, and stage types follow the selected language.
- War Room has a privacy policy in English, Spanish, and Catalan at `/privacy.html`, linked from the sign-in screen and the account menu (from 0.4.1).
- The MCP server now covers every remaining API route: interview questions, search periods, stage order, meetings, board counts, and the CV version a job uses (from 0.4.2).

# What changed
- Add an "Export to a spreadsheet" section to Data & Backups. It downloads every process, not only the filtered board, as `war-room-board-YYYY-MM-DD.csv`, with company, role, status, current stage, work arrangement, employment type, salary range and currency, referral, recruiter details, tech stack, CV version and file, CV sent and first recruiter contact dates, search period, posting and application links, and the dates the process was added, last changed status, and was last updated.
- Build the CSV in the browser from the jobs API, with no new backend route. It uses RFC 4180 quoting, CRLF line endings, and a UTF-8 byte order mark so spreadsheets show accented text correctly; dates are ISO `YYYY-MM-DD`, and text you entered is exported as typed.
- Prefix cells that start with `=`, `+`, `-`, `@`, a tab, or a carriage return with a single quote, so text from imported job postings cannot run as a spreadsheet formula.
- The CSV file is created on your device and is not encrypted; use the encrypted backup to move data between installations.
- Add `privacy.html` to the app image and the static demo build. It opens in the interface language, with links to the other languages (from 0.4.1).
- Describe what War Room collects from Google Sign-In and Drive (`drive.file`), what it stores and where, cookies and browser storage, server logs, sharing, the Google API Services User Data Policy, deletion, and how the demo keeps data in the browser (from 0.4.1).
- Add the MCP tools `count_jobs`, `set_job_cv_version`, `reorder_stages`, `list_meetings`, `create_question`, `update_question`, `reorder_questions`, `list_search_periods`, `create_search_period`, and `update_search_period`, with unit and integration tests. The route coverage test now rejects deferral reasons, so every API route needs a tool or a real reason (from 0.4.2).

# References
- [#8](https://github.com/NoveliaYuki/war-room/pull/8) / noissue
- [#9](https://github.com/NoveliaYuki/war-room/pull/9) / noissue
- [#10](https://github.com/NoveliaYuki/war-room/pull/10) / noissue

Version 0.4.0 — 2026-11-09 · Release: [v0.4.0](https://github.com/NoveliaYuki/war-room/releases/tag/v0.4.0)

# Highlights
- War Room now speaks English, Spanish, and Catalan. Pick a language in the account menu or on the sign-in screen and the whole interface switches without reloading the page; stored data never changes.
- With no language chosen, War Room follows your browser's preferred languages and falls back to English when none is supported.
- The MCP server now lives in its own Go package with full unit and integration tests (from 0.3.1).

# What changed
- Add a language selector to the account menu's Preferences and to the sign-in screen. Each language is listed in its own language, and an explicit choice is remembered in this browser.
- Translate every interface string: the board, filters, sort menu, cards, schedule, job details, forms, dialogs, toasts, accessible labels, frontend error fallbacks, and the browser demo's messages.
- Format dates, relative times, numbers, and file sizes for the selected language, keeping the browser's regional variant when it matches.
- Translate stored values only for display: statuses, work arrangements, employment types, stage types, meeting formats, and card history entries keep their stored English values, so data, backups, and the API are unchanged.
- Set the page language before the first paint and keep a non-English page hidden until it is translated, so English never flashes first.
- Show translated, correctly capitalized status labels on cards instead of CSS-capitalized stored values.
- Make layouts hold longer translations from 320px phones to wide desktops: filter option groups stack into one column when a label no longer fits, phones give search its own toolbar row, tags and pills stay on one line, dialog titles keep their close button beside them, and the stage schedule editor's action bar spans the dialog.
- Shorten placeholders that were cut off, and show meeting dates in schedule rows with a short weekday.
- Show server error messages, job import warnings, connection failures, and the browser's form validation bubbles in the selected language; a server message without a translation falls back to a translated summary.
- Translate the titles and descriptions of the default interview stages a new process starts with, including on existing processes, until the user edits them.
- Show the browser demo's sample processes, stages, interview questions, notes, and search periods in the selected language, while anything a visitor edits stays exactly as typed.
- End a long stage name on a card with an ellipsis inside the card instead of letting it spill past the edge; the tooltip keeps the full name.
- Show a stage title exactly as typed, even when it matches a stage type such as "Technical"; only untitled stages and the default stage titles are translated. Job lists now include `current_stage_title_is_type`, which is true when `current_stage_title` is the stage type of an untitled stage.
- Read salaries typed the way the Spanish and Catalan screens show them: "desde", "des de", and "a partir de" mark a lower bound, and "70.000" or "72,5k" use a dot for thousands and a comma for decimals.
- Translate the demo's "changed tech stack" card history entry, and keep the backup panel's Dismiss label in the selected language after a switch.
- Save an empty company as "Unknown" from Edit details in the demo too, instead of a blank name and a false "changed company" history entry.
- Check every supported language in the pre-commit suite: a Playwright test opens every main screen in each language, signed in and in the demo, at phone and desktop widths, including after switching language with the screen open, and fails on text in another language (screen-reader labels and every dropdown option included) or text that overflows; another fails when a backend message, card history entry, or default stage text has no translation, and a unit test fails when a translation is copied from English.
- Fail the backend tests when an API route has neither an MCP tool nor a recorded reason, so the MCP server keeps up with the API.
- Show an undisclosed company as "Unknown Company" in every language, and start its edit fields empty instead of with the stored English placeholder.
- Move the MCP tools into `pkg/mcp`, share `service.IsValidationError`, and cover token authentication through the production middleware chain (from 0.3.1).

# References
- [#7](https://github.com/NoveliaYuki/war-room/pull/7) / noissue
- [#6](https://github.com/NoveliaYuki/war-room/pull/6) / noissue

Version 0.3.1 — 2026-11-06

# Highlights
- The MCP server now lives in its own Go package, `backend/pkg/mcp`, with its tests next to it.
- New integration tests drive `/api/mcp` through the production middleware chain with real personal API tokens.

# What changed
- Move the MCP tools from `pkg/handlers` into `pkg/mcp`, which depends on a narrow job-service interface; `pkg/handlers` only mounts it.
- Share the list of user-facing validation errors as `service.IsValidationError`, used by both the JSON API and the MCP tools.
- Cover every MCP tool's success and error paths, empty lists, schema rejection, request-size limits, and user scoping, at 100% statement coverage for `pkg/mcp`.
- Add integration tests for token-authenticated MCP and REST calls, cross-user isolation, and rejection of missing, unknown, malformed, revoked, expired, and no-longer-allowed tokens, including that an invalid token never falls back to the session cookie.
- Test the `api-token` command against a real configured database, and the remaining token storage and request-logging paths.
- Include `pkg/mcp` in the backend coverage gate.
- Document where the MCP code lives, and that agents check live data through MCP instead of the container database.

# References
- [#6](https://github.com/NoveliaYuki/war-room/pull/6) / noissue

Version 0.3.0 — 2026-11-06 · Release: [v0.3.0](https://github.com/NoveliaYuki/war-room/releases/tag/v0.3.0)

# Highlights
- Let an agent read and edit your War Room data without a browser: the backend now serves a Model Context Protocol (MCP) server at `/api/mcp`.
- Personal API tokens tied to your account authenticate scripts and agents. Create, list, and revoke them with the `api-token` command in the backend container.

# What changed
- Add the `api_tokens` table (migration 24). Only a SHA-256 hash of each token is stored; tokens last 1 to 365 days (default 90) and are never included in backups or Drive exports.
- Add `/server api-token create|list|revoke --email EMAIL`. `create` prints only the token on stdout so it can be redirected to a private file. The account must be on the allowlist and must have signed in once.
- Authenticate any request that carries an `Authorization` header by its bearer token alone, never falling back to the session cookie. Each use re-checks the allowlist and expiry, records the last-used time, and logs `auth=api_token`; rejected tokens write an `api_token.denied` audit event.
- Revoking Google access now also deletes the account's API tokens.
- Add MCP tools to list, read, create, and partially update jobs and stages, manage the technology catalog, and list CV versions. Every tool is scoped to the token's owner, rejects unknown fields, and hides internal errors behind a `request_id`.
- Document token creation and `claude mcp add --scope local` registration in the README and AGENTS.md.

# Upgrade notes
- Rebuild the backend to apply migration 24. Then create a token with `docker compose exec -T backend /server api-token create --email you@example.com --name my-agent > war-room-token` and keep the file private. Never put a token in a checked-in `.mcp.json`.

# References
- [#5](https://github.com/NoveliaYuki/war-room/pull/5) / noissue

Version 0.2.0 — 2026-11-04 · Release: [v0.2.0](https://github.com/NoveliaYuki/war-room/releases/tag/v0.2.0)

# Highlights
- Start a new process from a job posting link; War Room prefills the form for review and suggests Tech Stack entries.
- Structured backend logs explain each failure in one record, while clients get a generic message and a `request_id` to find it.
- Dragging cards and questions works on phones, including iOS browsers.
- Click a scheduled interview to open its process on that stage (from 0.1.1).

# What changed
- Add `JOB_IMPORT_ENABLED`, `LOG_LEVEL`, `LOG_FORMAT`, `LOG_MAX_SIZE`, and `LOG_MAX_FILES`.
- Log audit events for sign-in, sign-out, Google access, and Drive backups, and never log tokens, secrets, or query strings.
- Database failures return 500 without SQL text; input validation messages are unchanged.
- Scroll the board while a dragged card nears the screen edge, and keep the scroll position after a drop.
- Keep links and text selection working inside clickable interviews, and remove the card hover lift (from 0.1.1).

# References
- [#3](https://github.com/NoveliaYuki/war-room/pull/3) / noissue
- [#4](https://github.com/NoveliaYuki/war-room/pull/4) / noissue

Version 0.1.1 — 2026-11-02

# Highlights
- Click anywhere on a scheduled interview to open its process on that exact stage, replacing the Open Process button.
- Cards and primary buttons stay still on hover instead of lifting.
- The Job details and Interview stages tabs are now compact and attached to the job header on small screens.

# What changed
- Keep links, note toggles, and text selection working inside clickable interviews, and make the interview title a keyboard entry point.
- Open the stages view on phones when an interview is opened from the schedule.
- Remove the hover lift from process cards, the new process card and button, primary buttons, and schedule cards.

# References
- [#3](https://github.com/NoveliaYuki/war-room/pull/3) / noissue

Version 0.1.0 — 2026-11-02 · Release: [v0.1.0](https://github.com/NoveliaYuki/war-room/releases/tag/v0.1.0)

# Highlights
- Open the location of an on-site interview directly: stages with a Google Maps link show an Open in Maps button in the stage banner and the schedule, and stages without one offer Add Maps Link.
- Company logos now fit their avatar: solid logos fill it without a white frame, and transparent logos and wordmarks are trimmed and stay inside it.

# What changed
- Relabel the schedule form's link field for each format, and keep a saved link with its own format when switching.
- Classify each logo by its backdrop once per source, put white marks on a dark backing, and keep non-square painted logos whole on their own color.
- Reach the 44px phone touch target on stage meeting links, whose rules previously matched no rendered element.
- Cover on-site links and logo fitting with Playwright tests that check rendered avatar pixels across logo families and avatar shapes.

# References
- [#1](https://github.com/NoveliaYuki/war-room/pull/1) / noissue
- [#2](https://github.com/NoveliaYuki/war-room/pull/2) / noissue

Version 0.0.1 — 2026-10-26 · Issues: none · Release: [v0.0.1](https://github.com/NoveliaYuki/war-room/releases/tag/v0.0.1)

# Highlights
- Track interview processes on a searchable board with stages, questions, interviewers, meetings, a schedule view, notes, salary details, and attachments.
- Mandatory Google Sign-In limited to `AUTH_ALLOWED_EMAILS`, with all data scoped to the signed-in account and a custom profile photo (PNG or JPEG, up to 5 MiB).
- Automatic Google Drive backup and portable encrypted `.wrbackup` export and restore, streamed in constant memory.
- Reusable CV versions, a technology library, and search periods.
- Static browser demo (`?demo`) that deploys to Cloudflare Pages from `frontend/dist-demo`.

# What changed
- Add SQLite storage with versioned migrations (schema v23), running as a non-root Go service behind Nginx in Docker Compose.
- Add Google Sign-In that rejects unlisted or unverified accounts, stores sessions as hashes in `HttpOnly`, `SameSite=Lax` cookies, encrypts Drive tokens at rest, blocks cross-site state-changing requests, and purges expired sessions.
- Return to the sign-in screen when a session expires, and return generic messages for unexpected server errors while logging the details.
- Encrypt backups in authenticated chunks and reject truncated or modified files before using any data; concurrent transfers are capped at two.
- Reject reorder requests with unknown IDs as a client error.
- Apply no whole-request timeouts so large backups and uploads on slow connections are not cut off.
- Document sign-in setup, environment variables, and the account schema, and gate changes in CI with lint, complexity, coverage, integration, Playwright, vulnerability, secret, and Semgrep checks.
