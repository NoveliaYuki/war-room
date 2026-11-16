---
name: pre-commit
description: Run War Room’s required privacy, quality, security, data-model, Docker, and test gates before a commit.
---

## 1. Overview

These gates are mandatory before commits for PRs that include files outside
documentation and agent skills. PRs limited to documentation or agent skills
can skip the full suite after reviewing the diff and running `git diff --check`.
For mixed-scope PRs, run the full suite.
Use project Docker containers for tools and tests; do not install dependencies on the host.
Do not stage or commit unless the user explicitly authorizes it.

## 2. Quick Reference

| Gate | Required result |
| --- | --- |
| Git/data boundary | Local records, images, attachments, databases, and secrets are untracked |
| Fresh install | New databases start empty; no company-specific images are bundled |
| Configuration | `.env` values flow through Compose to their consuming service |
| Code and schema | Quality, complexity, security, and data-model checks pass |
| Tests | Static demo build, unit, integration, and Go Playwright suites pass in order |
| Languages | Every supported language shows only its own text, without overflow, signed in and in the demo |
| MCP | Every API route has an MCP tool or a recorded reason; tools match the current API |
| UI runtime | Reloaded app is visually and behaviorally verified in Chrome and mobile simulators, in every supported language |
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

### 4.3 Languages

War Room supports the languages listed in `LANGUAGES` in `frontend/public/js/i18n.js` (currently English, Spanish, and Catalan). Every string a user can see must come from the dictionaries in `frontend/public/js/locales/`, in every supported language:

- Add each new interface string to every dictionary; never hard-code visible text, `aria-label`, `title`, or `placeholder` values.
- Every message the backend can send to the interface (errors, job import warnings, and stored text it generates, such as default stage titles) needs an entry in `frontend/public/js/utils/serverMessages.js` or `frontend/public/js/utils/labels.js`.
- Store values in English and translate them only for display; never machine-translate user input.
- Sample data in the browser demo (`frontend/demo/data/text.js`) needs wording in every language.
- A translation that legitimately reads the same as English must be added to `SAME_AS_ENGLISH` in `frontend/tests/unit/i18n.test.js` after checking it in that language.

These gates enforce it and must pass: the dictionary tests in `frontend/tests/unit/i18n.test.js`, `TestEveryServerMessageHasATranslation`, and `TestEveryLanguageRendersOnlyItsOwnText`. The last one opens every main screen in every supported language, signed in and in the demo, at phone and desktop widths. It fails on interface text shown in another language, browser validation or server messages in another language, and text that overflows, is clipped, or overlaps where English does not. When you add a screen, add it to `languageScreens` in `e2e/tests/language_ui_test.go`.

### 4.4 MCP Server

The MCP server in `backend/pkg/mcp/` must always match the current API. When a backend change adds, removes, or changes an API route, a model field, a validation rule, or a behavior that agents rely on, update the MCP tools, their descriptions, `instructions`, and their unit and integration tests in the same PR. Tools reuse the API's request types, so check that new fields make sense to an agent and that the schemas still mark the right fields as required. `TestEveryAPIRouteIsCoveredOrExplained` in `backend/pkg/mcp/routes_test.go` fails when a route has neither an MCP tool nor a recorded reason in `routesWithoutTools`; never record a reason only to make it pass. Update the README's Agent access section when tools change.

### 4.5 Security

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

The test container has no Git history, so ordinary runs verify that root `VERSION` is the only application version without a release baseline. The test sequence also builds `frontend/dist-demo` and checks its demo runtime config, app entry files, and root-level Cloudflare Pages security headers. When validating a release bump, pass the prior version and bump type through the shell, for example `BASE_VERSION=0.0.2 VERSION_BUMP=patch docker compose --project-name war-room-precommit up --build --abort-on-container-exit --exit-code-from test test`.

Run `down` even if tests fail; never add `--volumes` to routine cleanup.

GitHub Actions supplements this local suite with production-image and dependency
vulnerability scans, a tracked-source secret scan, and Semgrep Community Edition
SAST using the `p/security-audit` ruleset. Confirm these CI checks pass before
requesting a merge.

| Order | Gate | Minimum result |
| --- | --- | --- |
| 1 | Version synchronization | Exact authorized SemVer bump |
| 2 | Format and lint | Go and frontend checks pass; complexity ≤10 |
| 3 | Static demo output | Build contains demo mode config and root security headers |
| 4 | Backend unit tests | At least 90% statement coverage |
| 5 | Frontend unit tests | At least 90% configured coverage |
| 6 | Backend integration tests | All pass |
| 7 | Go Playwright tests | All pass after both test services are healthy, including the every-language screen check |

## 6. Step 4 — Verify the Reloaded UI in Browsers and Mobile Simulators

For app-affecting changes, follow `AGENTS.md`'s “Start and verify” and “Mobile browser setup” instructions. Rebuild and force-recreate the affected app containers, wait until healthy, then open both `http://localhost:3000` and `http://localhost:3000/?demo` in desktop Google Chrome. Verify the current change in both modes at relevant wide and narrow viewport sizes, in every supported language: switch the language in the account menu and recheck each affected screen in English, Spanish, and Catalan (and any language added to `LANGUAGES` later). Look for text in the wrong language, including browser validation bubbles and error toasts, and for wrapping, clipping, or overflow caused by longer translations. PRs limited to documentation or agent skills have no rendered UI impact and can skip browser checks.

Start the `warroom-firefox-android` Android AVD, start Appium with `./scripts/start-appium.sh`, and verify the changed flow separately in Firefox, Google Chrome, and Chromium. Install a separate Chromium browser build in the AVD if a supported build is available; if none is available, report the exact limitation and do not count Chrome as Chromium coverage. For iOS, use Xcode via `DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer`, boot an available iPhone simulator with `xcrun simctl`, and verify Firefox, Chromium, and Safari. Branded Google Chrome cannot be installed in this iOS simulator because it has no App Store or official Chrome simulator build; skip this browser/platform cell, record that limitation, and continue with the available browsers. Check installed apps using `adb shell pm list packages` and `xcrun simctl listapps booted`; do not report coverage for an absent app. Do not ask the user to connect an iPhone when they have said they do not own one, and do not try to install the App Store build into the simulator. Test the installed unbranded iOS Chromium build as Chromium, not as Google Chrome. Mark any other unavailable browser/platform cell with the exact limitation and continue testing every available browser. Use Appium UiAutomator2/XCUITest native touch actions or direct simulator operation for touch behavior; screenshots and desktop emulation alone do not verify gestures. Inspect both app and demo modes and the affected layout at phone size, in every supported language.

At minimum, inspect text readability, wrapping, spacing, control alignment, clipping/overlap, scrolling, and the changed interaction. For card/question behavior, actually hold and drag a card up and down, reorder a question using its handle, then verify scrolling works normally when no drag is active. For status behavior, tap through Ongoing → Rejected → Approved → Ongoing. For other changes, exercise the affected controls and neighboring flows. Confirm the page still scrolls after a gesture and that a drag does not turn into page scrolling.

Record results for each available browser separately: Android Firefox, Google Chrome, and Chromium; iOS Firefox, Chromium, and Safari. Include each browser's version/build and verified engine. Safari and the available iOS Chromium source build use WebKit. Never count Google Chrome as separate Chromium coverage or call unbranded Chromium Google Chrome. Do not infer Firefox or Chrome's engine from its name or OS; verify the exact build configuration and any applicable alternative-engine entitlement. Record branded iOS Chrome as unavailable because this simulator cannot install it, and skip that cell without failing the browser matrix; do not request a physical iPhone from a user who has said they do not own one. If no supported standalone Chromium browser build can be installed in Android, report Android Chromium as unavailable; do not count Chrome as its substitute. Do not substitute Safari or Chromium and claim Google Chrome passed. Do not sign into an Apple account or accept Xcode legal terms for the user. Never request or grant Screen & System Audio Recording permission; Appium touch actions do not require it. If another required OS, browser, or simulator runtime is unavailable, document the exact limitation and continue all available checks; do not claim coverage for a browser that was not tested.

Run the available mobile browser matrix for app-affecting changes. Documentation-only and agent-skill-only PRs can skip the mobile matrix because they do not change app behavior. The unavailable branded iOS Chrome cell is an accepted skip when its simulator limitation is recorded; do not mark that cell as tested.

## 7. Red Flags — Never / Always

### Never

- Never install project dependencies on the host or run tests outside Docker.
- Never commit after a failed required gate; skip the full suite only for PRs limited to documentation or agent skills.
- Never stage `data/`, `backend/data/`, personal records, logos, or attachments.
- Never seed a fresh install with job-selection data.
- Never run `docker compose down --volumes` or overwrite a populated volume.
- Never weaken assertions, coverage, or security checks to hide a failure.
- Never hard-code user-visible text, or add a string to only some languages.
- Never change the API without updating the MCP server to match.

### Always

- Always verify Git ignore rules and Docker build-context exclusions.
- Always verify a fresh database contains no job-selection records and `.env` values reach their consumers.
- Always review data ownership, constraints, migrations, and query efficiency.
- Always run unit, integration, and Playwright suites in the listed order when the full pre-commit suite is required.
- Always check the changed UI in every supported language, and report each language checked.
- Always report pass/fail, coverage, blockers, and the final file list.

## 8. Worked Example — Fresh Clone

A fresh clone uses an empty seed and a new empty Docker volume.
The app opens with no jobs or company-specific logos; local state remains ignored.
Playwright uses isolated temporary data and cannot change the app volume.
If local data appears in Git status, fix the ignore rule and preserve the files.
