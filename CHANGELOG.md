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
