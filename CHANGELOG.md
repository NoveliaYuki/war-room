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
