# War Room

War Room is a self-hosted, single-user app for tracking interview and candidate-selection processes. Run it with Docker on a machine you control; it keeps roles, stages, interview questions, meetings, notes, and related files together in one browser-based app. By default, it is available only from that machine because its ports bind to loopback.

## Quick start

Requirements: Git and Docker with the Compose plugin. Project packages and development tools are installed in Docker images; no host Node.js or npm is required.

```sh
git clone https://github.com/NoveliaYuki/war-room.git war-room
cd war-room
cp .env.example .env
docker compose up --build -d
```

Compose starts the app and isolated test services by default. The Playwright runner waits until its test backend and frontend are healthy, then runs the full suite. The test backend uses temporary storage and no host ports, so tests cannot read or alter the app's persistent database. Follow progress with `docker compose logs --tail=100 test`. Open <http://localhost:3000> when the app services report `healthy` in `docker compose ps`; the API is at <http://localhost:4040>.

Stop the app while keeping its database and uploads:

```sh
docker compose down
```

`docker compose down --volumes` permanently deletes the app's local database, snapshots, and uploads.

## Features

- Searchable selection-process board with status filters.
- Interview stages, questions, interviewers, meetings, and schedule view.
- Notes, salary details, company information, and attachments.
- SQLite storage, schema migrations, and a recovery snapshot in a Docker volume.

## Static browser demo

The repository also includes a frontend-only demo with fictional sample processes. It uses the same UI as the self-hosted app, stores edits in the current browser's local storage, and does not include or require the Go API. File attachments are unavailable in the demo. See [`frontend/demo/README.md`](frontend/demo/README.md) for details.

Build the static demo from `frontend/` with `npm run build:demo`. The generated site is written to `frontend/dist-demo` and can be hosted by any static-site host; it does not deploy the backend.

## Architecture

- `frontend/` — static HTML, CSS, and JavaScript served by Nginx.
- `backend/` — Go HTTP API, SQLite migrations, repositories, and services.
- `backend/tests/unit/` and `backend/tests/integration/` — backend unit and API integration tests.
- `frontend/tests/unit/` — frontend unit tests.
- `e2e/tests/` — Go Playwright browser tests.
- `docs/database.md` — schema, constraints, migrations, and persistence review.

The production backend is built in a Go builder stage and runs as a non-root user in `scratch`. The frontend build validates assets before copying them into an Nginx runtime image. The Playwright runner starts only after its isolated test backend and test frontend pass their 5-second health checks. Its test backend uses a temporary filesystem; application data remains in the separate `warroom-data` volume.

## Configuration and data

Copy `.env.example` to `.env`. Compose uses these local settings:

| Variable | Default | Purpose |
| --- | --- | --- |
| `APP_BIND_ADDRESS` | `127.0.0.1` | Host interface for published ports |
| `FRONTEND_HOST_PORT` | `3000` | Web app host port |
| `BACKEND_HOST_PORT` | `4040` | API host port |
| `BACKEND_HOST` | `0.0.0.0` | API listen address inside its container |
| `CORS_ALLOWED_ORIGINS` | localhost origins | Browser origins allowed by the API |
| `LOGO_LOOKUP_ENABLED` | `false` | Allow remote company-favicon lookups |

Compose uses the bind address and host ports for published-port mappings. It passes backend and test settings into the appropriate containers. A fresh `warroom-data` volume starts with an empty database. The backend stores its SQLite database, generated backup, company logos, and attachments in that local Docker volume. Workspace `data/` and `backend/data/` folders are ignored by Git and excluded from Docker build contexts; they are for local state only. Protect the volume and backups as private data.

## Development and tests

Use Docker for formatting, linting, and tests. Do not install project dependencies or tools on the host. Plain `docker compose up --build -d` starts the app and runs Playwright in an isolated test stack. For a synchronous pre-commit result, use the test-only Compose project below. The pre-commit requirements are in [.agents/skills/pre-commit/SKILL.md](.agents/skills/pre-commit/SKILL.md).

```sh
docker compose --project-name war-room-precommit up --build --abort-on-container-exit --exit-code-from test test
docker compose --project-name war-room-precommit down
```

Run the cleanup command even when a check fails. The test-only project has temporary data and does not use the app's published ports or persistent database.

The full test workflow checks version synchronization, formatting, Go and JavaScript quality rules, backend unit coverage (90% minimum), frontend unit coverage (90% minimum), backend integration tests, and Go Playwright tests. Playwright runs in Docker. Use `git diff --check` before submitting changes.

## Contributing

Use focused changes, add tests and documentation when behavior changes, and include versioned migrations with database changes. Before opening a pull request, run the test-only Compose command above; never include private candidate data, generated databases, backups, or attachments.

## Security and privacy

This is a single-user app with no authentication or authorization. “Self-hosted” means you run and manage it on a machine you control; it does not mean LAN access is enabled. The default ports bind to loopback. Do not expose the app to a public or shared network; authentication and additional security work are needed before network access is appropriate. CORS is not access control. Remote logo lookup is disabled by default; if enabled, the backend sends the inferred company domain to Google's favicon service. Avoid enabling it when company-search privacy matters.

To report a vulnerability, contact the maintainers privately through the hosting platform's security feature when available. Do not include candidate data, credentials, attachments, or unredacted logs in public issues.

## Troubleshooting

- If Docker cannot connect, start Docker Desktop or run `colima start` on macOS.
- If a service is unhealthy, inspect `docker compose ps` and `docker compose logs --tail=200 backend frontend`.
- If ports 3000 or 4040 are busy, stop the process using them and rerun `docker compose up --build -d`.

## Versioning

`VERSION` is the release source of truth. Keep `package.json` and both image labels synchronized with it. Later releases require the exact next semantic-version bump. The test container checks version references by default; release authors can optionally pass `BASE_VERSION` and `VERSION_BUMP` (defaults to `patch`) to validate the bump. These settings are not needed to run or deploy the app.

## License

This project is licensed under the GNU Affero General Public License v3.0 (AGPL-3.0). See the repository's `LICENSE` file for the full terms.
