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
| Fresh install | `fixtures/seed.json` is `[]`; no company-specific images are bundled |
| Configuration | `.env` values flow through Compose to their consuming service |
| Code and schema | Quality, complexity, security, and data-model checks pass |
| Tests | Unit, integration, and Go Playwright suites pass in order |
| Containers | Compose is valid; runtime images are multi-stage and healthy |

## 3. Step 1 — Inspect Data and Configuration

Check `git status --short`, `git diff --check`, tracked files, and staged/untracked paths.
Never print private job-selection records, attachment contents, credentials, or secret values.

| Path or file | Requirement |
| --- | --- |
| `data/`, `backend/data/` | Entire directories ignored by `.gitignore` |
| Docker build contexts | Exclude runtime data, databases, logos, uploads, and backups |
| `fixtures/seed.json` | Valid empty array `[]`; synthetic data belongs only in tests |
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

Run `down` even if tests fail; never add `--volumes` to routine cleanup.

| Order | Gate | Minimum result |
| --- | --- | --- |
| 1 | Version synchronization | Exact authorized SemVer bump |
| 2 | Format and lint | Go and frontend checks pass; complexity ≤10 |
| 3 | Backend unit tests | At least 90% statement coverage |
| 4 | Frontend unit tests | At least 90% configured coverage |
| 5 | Backend integration tests | All pass |
| 6 | Go Playwright tests | All pass after both test services are healthy |

## 6. Red Flags — Never / Always

### Never

- Never install project dependencies on the host or run tests outside Docker.
- Never commit after a failed or skipped required gate.
- Never stage `data/`, `backend/data/`, personal records, logos, or attachments.
- Never seed a fresh install with job-selection data.
- Never run `docker compose down --volumes` or overwrite a populated volume.
- Never weaken assertions, coverage, or security checks to hide a failure.

### Always

- Always verify Git ignore rules and Docker build-context exclusions.
- Always verify the seed fixture is `[]` and `.env` values reach their consumers.
- Always review data ownership, constraints, migrations, and query efficiency.
- Always run unit, integration, and Playwright suites in the listed order.
- Always report pass/fail, coverage, blockers, and the final file list.

## 7. Worked Example — Fresh Clone

A fresh clone uses an empty seed and a new empty Docker volume.
The app opens with no jobs or company-specific logos; local state remains ignored.
Playwright uses isolated temporary data and cannot change the app volume.
If local data appears in Git status, fix the ignore rule and preserve the files.
