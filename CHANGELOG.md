# Changelog

## 1.7.0 — 2026-10-09

## Highlights
- Group job processes into named Search Periods with date ranges, filtering, and automatic assignment for newly created jobs.

## What changed
- Style the Search Period selector with a consistent custom chevron across browsers.
- Preserve period associations in backups and let deleting a period leave its jobs unassigned.

## 1.6.0 — 2026-10-09

Joint release covering the 1.4.0, 1.5.x, and 1.6.0 changes.

## Highlights
- Edit notes and company or role descriptions with Markdown, including a rendered preview.
- Filter and sort job processes by status and other criteria, with clearer empty states and quick-create actions.
- New job cards default to Waiting.

## What changed
- Harden local security with safer database handling, security headers, and automated checks for shipped image vulnerabilities, source secrets, and security findings.
- Improve static demo packaging and add checks for its runtime configuration and deployment headers.
- Update Go, Node.js, Playwright, and lint tooling, including fixes for SQLite toolchain compatibility and Babel import resolution.
- Clarify status-specific empty states and add guidance for mapping named interviewers to stages.

## 1.0.0 — 2026-10-01

First stable release of War Room, a self-hosted, single-user interview-process tracker.

- Track selection processes with searchable status filters, interview stages, questions, interviewers, notes, salary details, and attachments.
- Review upcoming meetings in the daily schedule and open meeting links and preparation notes.
- Export and restore portable ZIP backups with validated attachments and company logos.
- Use the responsive browser app or the fictional-data static demo.
- Navigate the app and dialogs with a keyboard, with visible focus, labeled controls, trapped dialog focus, and focus restoration.
- Run formatting, lint, dependency audit, unit, integration, and browser checks through the precommit suite and GitHub Actions.

War Room has no authentication. The supported deployment is local, single-user use bound to loopback; do not expose it to a public or shared network.
