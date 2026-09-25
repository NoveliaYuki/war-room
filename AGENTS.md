# Agent instructions

## Project at a glance

War Room is a local, single-user interview-process tracker. `frontend/` contains static HTML, CSS, and JavaScript served by Nginx; `backend/` is a Go API backed by SQLite. Docker Compose starts the app and an isolated Playwright test stack. The API has no authentication, so keep app ports on loopback and do not describe it as safe for public or shared-network deployment.

## Start and verify

Run these from the repository root. Docker builds the app and test images, installs all project tools in those images, starts the isolated Playwright stack after its services are healthy, and leaves the app available.

```sh
cp .env.example .env
docker compose up --build -d
```

Open <http://localhost:3000>. Check app health with `docker compose ps`; view browser-test output with `docker compose logs --tail=100 test`. The test backend uses temporary storage and never reads or changes the app database. Stop containers with `docker compose down`; this preserves app data. **Do not run `docker compose down --volumes` unless the user explicitly asks to delete the local database and uploads.**

## Required practices

- Use project Docker containers for development, formatting, linting, and tests. The default `docker compose up` starts an isolated test backend, test frontend, and Go Playwright runner; the runner waits for both test services to become healthy.
- Keep all runtime data, company logos, and attachments out of Git and Docker build contexts. The `data/` and `backend/data/` directories are local-only; do not add placeholders or exceptions that make their contents trackable. Keep `fixtures/seed.json` empty (`[]`) so fresh installs contain no job-selection records. Never print private values while checking local state.
- Keep `.env` and secrets local. Add safe defaults to `.env.example`, pass settings through Compose to the process that consumes them, and document each new setting. Never put credentials in the example file.
- Use SVG for icons. Do not use emoji or emoticons in the app or documentation.
- Keep comments short and useful. Use Google-style Go doc comments and JSDoc for exported interfaces; remove comments that merely narrate obvious code. Retain rationale for security-sensitive or non-obvious behavior.
- Keep functions at cyclomatic complexity 10 or lower and avoid cyclic package/module dependencies. Validate untrusted input, use parameterized SQL, preserve escaping and security headers, and bound request, file, and network resources.
- Make database changes with versioned migrations and tests covering new and existing data. Preserve user data; never replace a populated local volume with fixture data.
- Keep production Docker images multi-stage and minimal. Health checks run every 5 seconds; the test runner must wait for both isolated test services to be healthy.
- Keep `VERSION` at `0.0.1` for the initial release. Change it only when the user requests a later release, and update all version references together.
- For release commits, follow `.agents/skills/git-workflow/SKILL.md`: use `type: [vX.Y.Z] - summary`, assign one version per PR, and reuse it for every commit in that PR. Never push unless the user says exactly: `push the current version to remote`.

## Checks before handoff

Before committing, run `docker compose --project-name war-room-precommit up --build --abort-on-container-exit --exit-code-from test test`. It runs version synchronization, formatting, Go and JavaScript quality rules, backend and frontend unit coverage (90% minimum), backend integration, and Go Playwright tests in order inside Docker. Then run `docker compose --project-name war-room-precommit down`, even if a check fails. Also run `git diff --check`, inspect staged and untracked files, and validate Compose changes. Report blocked checks; do not claim public-production readiness while authentication is absent.

The README is a short human onboarding guide. These instructions and `.agents/skills/pre-commit/SKILL.md` are the operational checklist; do not assume a contributor or agent has read the full README.
