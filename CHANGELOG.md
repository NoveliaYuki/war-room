Version 0.1.0 — 2026-11-17 · Release: [v0.1.0](https://github.com/NoveliaYuki/war-room/releases/tag/v0.1.0)

# Highlights
- **Data & Backups** now has one Export and one Import. Export downloads the format you choose, a spreadsheet (`.xlsx`) or an encrypted backup (`.wrbackup`); Import takes either file and recognizes which it is.
- Import an edited board spreadsheet back into War Room: choose the Excel file from Export, or a CSV saved from its Board sheet, review every change, then import it, signed in or in the browser demo.
- Only edited cells are saved, so importing an unedited file changes nothing, whichever supported language it was exported in. Rows without a Process ID become new processes, and nothing missing from the file is deleted.
- Google Drive backups can now run on a schedule. In the Google Drive section, choose Off, Every day, or Every week on a chosen day, and a time of day; War Room describes the choice in one sentence, such as "Backs up every Monday at 9:00 AM", with no cron syntax anywhere.
- The Google Drive section shows when the next automatic backup runs and how the last one went, including when Google access has expired or been revoked and you need to sign in again.

# What changed
- Replace the separate spreadsheet export, encrypted export, and backup import sections with one Export section and one Import section. Export has a File format choice of Spreadsheet (.xlsx) or Encrypted backup (.wrbackup), each with a short description, and one download button named after the selected format. The browser demo shows the encrypted format disabled with an explanation.
- Import has a single file picker for `.xlsx`, `.csv`, and `.wrbackup` files. It recognizes an encrypted backup by its `.wrbackup` extension or by the marker both backup formats start with, before reading anything else, and reads any other file as a spreadsheet. A backup shows the replace warning, the confirmation, and Import and replace data, as before, and is sent to the server without being read in the browser; a spreadsheet shows its preview and Import changes. A file that is neither is reported in the selected language, and the browser demo explains that it cannot restore backups.
- Group Google Drive backup in one section: the folder with Choose Drive folder and Back up to Google Drive, then Automatic backups, then Revoke Google Drive access last.
- Add a hidden Process ID column after the Board sheet's groups so each row can be matched back to its process; the Interview questions sheet already carries process, stage, and question IDs.
- Read `.xlsx` workbooks whether compressed or not, with shared or inline text and either date system (checked with files saved again by LibreOffice Calc and openpyxl), and comma-, semicolon-, or tab-separated CSV in UTF-8, UTF-16, or Windows-1252. Files are read in the browser, up to 20 MB and 10,000 rows per sheet, with bounded decompression.
- Read statuses, work arrangements, employment types, Yes and No, and the undisclosed company in any supported language; salaries such as "60,000" or "60.000"; dates as date cells or YYYY-MM-DD; technologies by catalog name or alias; search periods by name; and CV versions by number.
- Keep the Markdown company and role overview unless its plain-text cell was edited, and warn in the preview when an edit would replace Markdown formatting.
- Skip, with the row and reason shown in the preview, cells War Room cannot read, edits to read-only columns such as Current stage or Date added, rows whose Process ID or Question ID is unknown or repeated, and new rows without a role or question. Warn when a process changed in War Room after the file was exported.
- Update interview questions by Question ID, and add a row without a Question ID to the stage named by its Stage ID.
- Match rows by company and role when a sheet has no Process ID column, such as a board exported before this version.
- Save one record at a time and list any record that fails with its row and reason; importing the same file again saves only what is left.
- Show the server's translated reason when saving a process, a question, or a process's CV version fails, instead of an HTTP status code.
- Add `GET` and `PUT /api/google/backup-schedule`. A schedule is stored as structured fields (frequency, weekday, time of day, and the browser's IANA time zone) in the new `backup_schedules` table (migration 25). Every account, including existing ones, starts with automatic backups off, so nothing changes until you turn them on.
- Validate every schedule field on the server: frequency must be off, daily, or weekly; the weekday 0 to 6; the time strict 24-hour `HH:MM`; and the time zone a named IANA zone (never the server's own). Errors are translated in every language.
- Run due backups from a background scheduler that checks once a minute, keeps the chosen wall-clock time across daylight-saving changes, uploads at most two backups at once, and claims each run before starting it, so a run never repeats after a restart. A run missed while the server was down happens once at startup. Shutdown stops the scheduler before the database closes.
- Record each automatic run's result: completed, failed, skipped because Drive is not connected or not set up on the server, or failed because Google access expired or was revoked. Signing in again clears the expired-access message.
- Show an expired or revoked Google grant in the account menu and on Backup now with its own translated message instead of a generic failure.
- Embed the time zone database in the backend binary so schedules work in any zone.
- Add the `BACKUP_SCHEDULER_ENABLED` setting (default `true`); the isolated test backend turns it off.
- In the browser demo, show the schedule control disabled with an explanation that automatic backups need a signed-in account.
- The schedule routes are set up in the browser, so the MCP route coverage test records them without tools; spreadsheet import adds no API routes, so the MCP server and its tools are unchanged.

# References
- [#2](https://github.com/NoveliaYuki/war-room/pull/2) / noissue

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
