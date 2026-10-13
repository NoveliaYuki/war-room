# Changelog

## 1.9.0 — 2026-10-13

## Highlights
- Automatically move Waiting job cards to Rejected after 30 elapsed days without saved activity, with activity tracking for card edits and related interview data.
- Make the automatic inactivity rule clear in demo mode, including a sample card already rejected after 30 inactive days.

## What changed
- Track job activity in SQLite and run the inactivity sweep when the backend starts and every 24 hours while it runs. Existing cards receive a 30-day grace period after upgrade.
- Apply the same 30-day rule in browser-only demo mode, explain the rule on the demo board, and ensure demo Waiting cards have recent activity.
- Include the 1.8.x and earlier unreleased improvements since the last published release: responsive board and filter updates, Search Periods, dark theme refinements, improved status and meeting navigation, Markdown notes, and security and CI checks.
- Keep technology aliases, named interview stages, and job creation defaults consistent with their role and posting details.

## 1.8.3 — 2026-10-13

## Highlights
- Make meeting counts and interview stage navigation match the current process state.

## What changed
- Count ongoing and upcoming meetings in the Schedule badge instead of past meetings.
- Keep the final interview stage reachable by scrolling without wrapping stage navigation or showing a right arrow.
- Make the desktop filters panel wider and more compact, preserve space beside its scrollbar, and keep it open while selecting statuses.

## 1.8.0 — 2026-10-12

## Highlights
- Keep process controls clear and usable across desktop, narrow windows, and mobile screens.

## What changed
- Keep status and Daily Schedule controls visible while reserving the hamburger menu for future options.
- Align toolbar controls and active filter chips across viewport widths, with removable filters that stay open while editing.
- Show filters in a full-screen mobile sheet with independently scrolling fields and pinned actions on phone-sized viewports.
- Improve responsive process cards and keep control sizes consistent as the window narrows.
- Default newly created processes to Waiting.

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
