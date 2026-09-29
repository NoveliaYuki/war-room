---
name: pre-commit
description: Run War Room’s required privacy, quality, security, data-model, Docker, and test gates before a commit.
---

## 1. Overview

These gates are mandatory before every commit.
Use project Docker containers for tools and tests; do not install dependencies on the host.
Do not stage or commit unless the user explicitly authorizes it.

## 2. Quick Reference

| Gate | Required result |
| --- | --- |
| Git/data boundary | Local records, images, attachments, databases, and secrets are untracked |
| Fresh install | New databases start empty; no company-specific images are bundled |
| Configuration | `.env` values flow through Compose to their consuming service |
| Code and schema | Quality, complexity, security, and data-model checks pass |
| Tests | Unit, integration, and Go Playwright suites pass in order |
| UI runtime | Reloaded app is visually and behaviorally verified in Chrome and mobile simulators |
| Containers | Compose is valid; runtime images are multi-stage and healthy |

## 3. Step 1 — Inspect Data and Configuration

Check `git status --short`, `git diff --check`, tracked files, and staged/untracked paths.
Never print private job-selection records, attachment contents, credentials, or secret values.

| Path or file | Requirement |
| --- | --- |
| `data/`, `backend/data/` | Entire directories ignored by `.gitignore` |
| Docker build contexts | Exclude runtime data, databases, logos, uploads, and backups |
| Fresh database | No job-selection records; synthetic data belongs only in tests |
| `.env` | Local and ignored; never commit credentials |
| `.env.example` | Safe defaults only; document each variable |

Trace every setting through `.env` → Compose → container environment → consumer.
Check for hardcoded secrets, user-specific paths, and duplicated configuration values.
Keep remote logo lookup disabled by default; it sends company-domain data externally.
On a fresh clone, a new app volume must contain no jobs, attachments, or cached logos.
Preserve existing volumes; never delete or overwrite user data during checks.

## 4. Step 2 — Review Code, Data Models, and Security

### 4.1 Code Quality

Require correct formatting, lint, typing, and Google-style Go comments/JSDoc where useful.
Keep cyclomatic complexity at or below 10 and package/module dependencies acyclic.
Remove comments that merely restate code; keep security and non-obvious rationale.

### 4.2 Data Model

Check model/schema/API consistency, nullability, constraints, indexes, and ownership.
Use versioned migrations; preserve existing data and test both clean and upgraded databases.
Review query plans for costly queries; avoid N+1 reads and unnecessary duplicate indexes.

### 4.3 Security

Validate inputs and uploads; use parameterized SQL and safe DOM rendering.
Check URL handling, CORS, security headers, file paths, request limits, and outbound calls.
Keep app ports loopback-bound; CORS does not provide authentication.

## 5. Step 3 — Validate Docker and Run Tests

Run `docker compose config --quiet` and inspect health checks, volumes, networks, and environment.
Production Dockerfiles must separate builder and minimal runtime stages and run non-root where supported.
The Playwright runner waits for healthy isolated test backend and frontend services.
Test services use temporary storage and must never access the app’s persistent volume.

For the normal developer flow, `docker compose up --build -d` starts the app and test stack.
For a synchronous pre-commit result, run:

```sh
docker compose --project-name war-room-precommit up --build --abort-on-container-exit --exit-code-from test test
docker compose --project-name war-room-precommit down
```

The test container has no Git history, so ordinary runs verify that root `VERSION` is the only application version without a release baseline. When validating a release bump, pass the prior version and bump type through the shell, for example `BASE_VERSION=0.0.2 VERSION_BUMP=patch docker compose --project-name war-room-precommit up --build --abort-on-container-exit --exit-code-from test test`.

Run `down` even if tests fail; never add `--volumes` to routine cleanup.

| Order | Gate | Minimum result |
| --- | --- | --- |
| 1 | Version synchronization | Exact authorized SemVer bump |
| 2 | Format and lint | Go and frontend checks pass; complexity ≤10 |
| 3 | Backend unit tests | At least 90% statement coverage |
| 4 | Frontend unit tests | At least 90% configured coverage |
| 5 | Backend integration tests | All pass |
| 6 | Go Playwright tests | All pass after both test services are healthy |

## 6. Step 4 — Verify the Reloaded UI in Browsers and Mobile Simulators

This is a required check before every commit. Follow `AGENTS.md`'s “Start and verify” and “Mobile browser setup” instructions. Rebuild and force-recreate the affected app containers, wait until healthy, then open both `http://localhost:3000` and `http://localhost:3000/?demo` in desktop Google Chrome. Verify the current change in both modes at relevant wide and narrow viewport sizes. For a change with no rendered UI impact, still load both modes and state why no UI interaction changed.

Start the `warroom-firefox-android` Android AVD, start Appium with `./scripts/start-appium.sh`, and verify the changed flow separately in Firefox, Google Chrome, and Chromium. Install a separate Chromium browser build in the AVD if a supported build is available; if none is available, report the exact limitation and do not count Chrome as Chromium coverage. For iOS, use Xcode via `DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer`, boot an available iPhone simulator with `xcrun simctl`, and verify Firefox, Chromium, Safari, and branded Google Chrome only when an official simulator build is available. Check installed apps using `adb shell pm list packages` and `xcrun simctl listapps booted`; do not report coverage for an absent app. This Xcode simulator has no App Store or branded Chrome simulator build. Do not ask the user to connect an iPhone when they have said they do not own one, and do not try to install the App Store build into the simulator. Test the installed unbranded iOS Chromium build as Chromium, not as Google Chrome. Mark any unavailable browser/platform cell with the exact limitation and continue testing every available browser. Use Appium UiAutomator2/XCUITest native touch actions or direct simulator operation for touch behavior; screenshots and desktop emulation alone do not verify gestures. Inspect both app and demo modes and the affected layout at phone size.

At minimum, inspect text readability, wrapping, spacing, control alignment, clipping/overlap, scrolling, and the changed interaction. For card/question behavior, actually hold and drag a card up and down, reorder a question using its handle, then verify scrolling works normally when no drag is active. For status behavior, tap through Ongoing → Rejected → Approved → Ongoing. For other changes, exercise the affected controls and neighboring flows. Confirm the page still scrolls after a gesture and that a drag does not turn into page scrolling.

Record results for each browser separately: Android Firefox, Google Chrome, and Chromium; iOS Firefox, Chromium, Safari, and branded Google Chrome only if an official simulator build is available. Include each browser's version/build and verified engine. Safari and the available iOS Chromium source build use WebKit. Never count Google Chrome as separate Chromium coverage or call unbranded Chromium Google Chrome. Do not infer Firefox or Chrome's engine from its name or OS; verify the exact build configuration and any applicable alternative-engine entitlement. This simulator has no App Store or branded Chrome simulator build: report iOS Chrome as unavailable in this simulator and continue, without requesting a physical iPhone from a user who has said they do not own one. If no supported standalone Chromium browser build can be installed in Android, report Android Chromium as unavailable; do not count Chrome as its substitute. Do not substitute Safari or Chromium and claim Google Chrome passed. Do not sign into an Apple account or accept Xcode legal terms for the user. Never request or grant Screen & System Audio Recording permission; Appium touch actions do not require it. If a required OS, browser, or simulator runtime is unavailable, document the exact limitation and continue all available checks; do not claim coverage for a browser that was not tested.

Run the mobile browser matrix even for documentation-only changes; confirm the app still loads and the mobile layouts remain usable, then note that the changed files have no rendered UI impact. Never mark the gate as passed when a required browser/platform combination was unavailable or skipped.

## 7. Red Flags — Never / Always

### Never

- Never install project dependencies on the host or run tests outside Docker.
- Never commit after a failed or skipped required gate.
- Never stage `data/`, `backend/data/`, personal records, logos, or attachments.
- Never seed a fresh install with job-selection data.
- Never run `docker compose down --volumes` or overwrite a populated volume.
- Never weaken assertions, coverage, or security checks to hide a failure.

### Always

- Always verify Git ignore rules and Docker build-context exclusions.
- Always verify a fresh database contains no job-selection records and `.env` values reach their consumers.
- Always review data ownership, constraints, migrations, and query efficiency.
- Always run unit, integration, and Playwright suites in the listed order.
- Always report pass/fail, coverage, blockers, and the final file list.

## 8. Worked Example — Fresh Clone

A fresh clone uses an empty seed and a new empty Docker volume.
The app opens with no jobs or company-specific logos; local state remains ignored.
Playwright uses isolated temporary data and cannot change the app volume.
If local data appears in Git status, fix the ignore rule and preserve the files.
