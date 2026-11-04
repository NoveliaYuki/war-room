Version 0.2.0 — 2026-11-04 · Release: [v0.2.0](https://github.com/NoveliaYuki/war-room/releases/tag/v0.2.0)

# Highlights
- Start a new process from a job posting link: War Room reads the public posting, prefills the form for review, and suggests Tech Stack entries. Smart paste fills the form from copied posting text when a site cannot be read.
- Structured backend logs explain every failure in one record: request ID, user, status, timing, the full error chain, and for Google calls the API's own reason with an operator hint.
- Clients receive only generic error messages plus a `request_id` that locates the full record in the server log.
- Audit events for sign-in, denied sign-in, sign-out, Google access revocation, backup folder changes, manual and background Drive backups, and automatic job rejections.
- Click anywhere on a scheduled interview to open its process on that exact stage (from 0.1.1).

# What changed
- Add `JOB_IMPORT_ENABLED` (default `true`); the backend fetches only the submitted public https link.
- Add `LOG_LEVEL` (default `info`) and `LOG_FORMAT` (default `json`). Health checks and job-count polling log at debug.
- Return the `X-Request-ID` header on every API response and `request_id` in every error body.
- Never log tokens, secrets, cookies, OAuth codes, request bodies, URL query strings, or Drive upload session URLs; secret-named fields are redacted as a safeguard.
- Recover handler panics with a logged stack trace and a generic 500 response.
- Log every Google sign-in failure with the step that failed, instead of only redirecting with `?google=error`.
- Database failures now return 500 instead of 400, 404, or 409, without SQL text; validation messages about the caller's own input are unchanged.
- Store a generic message in the Drive backup status instead of the raw error.
- Cap Docker log files for the app containers.
- Scroll the board while a dragged card is held near the top or bottom edge, so a card can move past neighbors on phone screens.
- Keep the board's scroll position after dropping a card; iOS browsers previously jumped back to the top.
- Keep the question list steady while a question is lifted or dropped, so questions can be dragged down in iOS Chromium.
- Keep links, note toggles, and text selection working inside clickable interviews, and make the interview title a keyboard entry point (from 0.1.1).
- Open the stages view on phones when an interview is opened from the schedule (from 0.1.1).
- Remove the hover lift from process cards, the new process card and button, primary buttons, and schedule cards (from 0.1.1).

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
