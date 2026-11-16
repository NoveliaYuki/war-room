Version 0.0.1 — 2026-11-16 · Issues: none · Release: [v0.0.1](https://github.com/NoveliaYuki/war-room/releases/tag/v0.0.1)

# Highlights
- Track interview processes on a searchable board with stages, interview questions, interviewers, meetings, a schedule view, notes, salary details, and attachments.
- Mandatory Google Sign-In limited to `AUTH_ALLOWED_EMAILS`, with all data scoped to the signed-in account, a custom profile photo, and a privacy policy at `/privacy.html`.
- Automatic Google Drive backup and portable encrypted `.wrbackup` export and restore, streamed in constant memory.
- Export the board and its interview questions as an Excel workbook (`.xlsx`) with grouped column headers, signed in and in the browser demo.
- Start a new process from a job posting link; War Room prefills the form for review and suggests Tech Stack entries.
- Let an agent read and edit your data through a Model Context Protocol (MCP) server at `/api/mcp`, authenticated by personal API tokens.
- The interface speaks English, Spanish, and Catalan, and follows the browser's preferred language until one is chosen.
- Static browser demo (`?demo`) that deploys to Cloudflare Pages from `frontend/dist-demo`.

# What changed
- Add SQLite storage with versioned migrations (schema v24), running as a non-root Go service behind Nginx in Docker Compose.
- Add Google Sign-In that rejects unlisted or unverified accounts, stores sessions as hashes in `HttpOnly`, `SameSite=Lax` cookies, encrypts Drive tokens at rest, blocks cross-site state-changing requests, and purges expired sessions.
- Encrypt backups in authenticated chunks and reject truncated or modified files before using any data; concurrent transfers are capped at two.
- Add reusable CV versions, a technology library with aliases, and search periods.
- Click a scheduled interview to open its process on that stage, and open on-site interview locations with Open in Maps.
- Fit company logos to their avatar: solid logos fill it, and transparent logos and wordmarks are trimmed and stay inside it.
- Drag cards and interview questions to reorder them, including on phones and iOS browsers, scrolling the board while a dragged card nears the screen edge.
- Import job postings from links (`JOB_IMPORT_ENABLED`), extracting structured posting data and suggesting technologies from the catalog.
- Write structured JSON logs (`LOG_LEVEL`, `LOG_FORMAT`, `LOG_MAX_SIZE`, `LOG_MAX_FILES`) with audit events for sign-in, sign-out, Google access, and Drive backups, never logging tokens, secrets, or query strings; clients get a generic message and a `request_id`.
- Add personal API tokens (`/server api-token create|list|revoke`). Only a SHA-256 hash is stored, tokens last 1 to 365 days, and they are never included in backups.
- Add MCP tools for every API route except deleting records, uploading files, Google Drive and backup setup, and arranging the board, each scoped to the token's owner. A backend test fails when an API route has neither a tool nor a recorded reason.
- Translate every interface string, server message, default stage title, and the demo's sample data into English, Spanish, and Catalan, translating stored values only for display. Format dates, numbers, and file sizes for the selected language.
- The Excel workbook has a Board sheet with numbered group headers (Job specification, Process, Application, Recruiter) that separate the company's published salary range from your expected salary, and an Interview questions sheet. Text is stored so formula-like values never run, and the Markdown overview is exported as plain text.
- Publish a privacy policy in every supported language, linked from the sign-in screen and the account menu.
- Gate changes in CI with lint, complexity, coverage, integration, Playwright (including every screen in every language), vulnerability, secret, and Semgrep checks.

# References
- Initial release; no pull requests.
