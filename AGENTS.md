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

After every project change, rebuild and recreate the app containers so the running app uses the current files. Run `docker compose --project-name war-room up --build --force-recreate -d frontend backend` (include any other affected app service), then wait for `docker compose --project-name war-room ps` to report healthy services. Keep the same project name, port overrides, and CORS settings used by the active local stack. The standard local stack serves the app at `http://localhost:3000`.

```sh
docker compose --project-name war-room up --build --force-recreate -d frontend backend
```

After each reload, open both `http://localhost:3000` and `http://localhost:3000/?demo` in Google Chrome. Confirm the change appears and behaves correctly in both modes. Inspect the affected UI at relevant desktop and narrow viewport sizes, checking spacing, alignment, wrapping, and overlap. Do not hand off the change until both views have been checked; report any view that could not be verified.

## Mobile browser setup

Use the root `Brewfile` to manage host-side browser and mobile tooling with Homebrew. On a fresh Mac, trust the optional Wix simulator utility with `brew trust --formula wix/brew/applesimutils`, then run `brew bundle`. Install the Appium extensions once with `appium driver install uiautomator2` and `appium driver install xcuitest`. Start Appium with `./scripts/start-appium.sh`; it supplies the Brew Android SDK and OpenJDK paths, selects `/Applications/Xcode.app/Contents/Developer` for that process when present, and keeps the server bound to loopback. Check Android setup with the Brew SDK and Java paths exported and iOS setup with `DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer appium driver doctor xcuitest`.

### Android emulator

Start the configured AVD and confirm it is online:

```sh
export ANDROID_HOME="$(brew --prefix)/share/android-commandlinetools"
export ANDROID_SDK_ROOT="$ANDROID_HOME"
export PATH="$ANDROID_HOME/platform-tools:$ANDROID_HOME/emulator:$PATH"
emulator -avd warroom-firefox-android
adb wait-for-device
adb shell getprop sys.boot_completed
```

The last command should print `1`. Confirm the installed browsers with `adb shell pm list packages | grep -E 'firefox|chrome'`. Open the app in Firefox or Chrome with `adb shell am start -a android.intent.action.VIEW -d 'http://10.0.2.2:3000' -p org.mozilla.firefox` or substitute `com.android.chrome`; Android emulators reach the host app through `10.0.2.2`. Use `http://10.0.2.2:3000/?demo` for demo mode. If the browser package is absent, install that browser in the AVD before claiming coverage.

### iOS Simulator

Do not change the machine-wide active developer directory when a per-command override works. On this setup Xcode 27 is at `/Applications/Xcode.app`, while `xcode-select` may still point to Command Line Tools. Set `DEVELOPER_DIR` on Xcode commands:

```sh
export DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer
xcrun simctl list devices available
```

If no usable iPhone is booted, choose an available iPhone UDID from that list and run `xcrun simctl boot <UDID>` followed by `xcrun simctl bootstatus <UDID> -b`. Xcode 27 may manage simulator devices through DeviceHub and may not include the older `Simulator.app`; use `simctl` and the installed runtime rather than assuming `open -a Simulator` exists. Open Safari with `xcrun simctl launch booted com.apple.mobilesafari`, then navigate to `http://localhost:3000` and `http://localhost:3000/?demo` in the simulator. If Firefox for iOS or another browser build is installed, launch its installed bundle identifier (inspect with `xcrun simctl listapps booted`).

Test Firefox, Google Chrome, and Chromium as separate browsers wherever builds are available. Safari is also required on iOS. On Android, check installed browser packages and test Chromium separately if a Chromium app is installed; do not count Google Chrome as Chromium coverage. On iOS, the installed Chromium source build is unbranded and uses WebKit; test it as Chromium, never as Google Chrome. The App Store version of branded Google Chrome is an iPhone/iPad app, and this Xcode simulator does not include the App Store or a Chrome simulator build. Do not ask the user to connect an iPhone when they have said they do not own one. Mark branded iOS Chrome unavailable in the simulator, continue with iOS Firefox, Chromium, and Safari, and report the limitation without claiming Chrome coverage. Do not spend time trying to install the App Store app into the simulator. Verify the engine for the exact browser build and platform and record browser version/build and engine; do not infer it from the browser name or OS.

Do not sign into an Apple account or the App Store on the Mac or simulator on the user's behalf. A simulator screenshot can help inspect layout, but it does not prove touch behavior. Use Appium XCUITest native touch actions or direct simulator operation. Do not request or grant Screen & System Audio Recording permission; it is not needed for Appium touch actions.

After Xcode's local license has been accepted by the user and an iOS runtime is installed, the Appium launcher detects Xcode at the default path. Set `DEVELOPER_DIR` explicitly to override that path:

```sh
DEVELOPER_DIR=/path/to/Xcode.app/Contents/Developer ./scripts/start-appium.sh
```

Use Appium's XCUITest driver against the booted iPhone simulator for native touch input; use UiAutomator2 against the Android AVD. Keep the Appium server on its loopback binding. A simulator screenshot can help inspect layout, but it does not prove touch behavior; verify gestures using native touch actions or by physically operating the simulator UI.

### Mobile UI behavior check

Check both `/` and `/?demo` at phone-sized viewports in Firefox, Google Chrome, and Chromium as separate browsers on Android; if no separate Chromium build is available for the AVD, report that exact limitation and do not count Chrome as Chromium. On iOS test Firefox, Chromium, Safari, and Google Chrome only when an official simulator build is available. Confirm readable text and labels, no clipped or overlapping controls, appropriate one-column card layout, and scrolling in both directions. Exercise actual touch interactions: hold and drag a job card upward and downward to reorder it, use the interview question's drag handle to reorder questions, verify ordinary page/list scrolling still works when not dragging, and tap the detail status label through Ongoing → Rejected → Approved → Waiting → Ongoing. Check that dragging a card suppresses page scrolling for the duration of the drag and releases scrolling afterward. Record results per browser and OS, including version/build and engine; report each unavailable browser with the exact limitation and continue the available matrix. Never count Chrome as separate Chromium coverage or an unbranded Chromium build as Google Chrome. Do not infer results from a different browser, OS, or desktop viewport.

## Required practices

- Use project Docker containers for development, formatting, linting, and tests. The default `docker compose up` starts an isolated test backend, test frontend, and Go Playwright runner; the runner waits for both test services to become healthy.
- Keep all runtime data, company logos, and attachments out of Git and Docker build contexts. The `data/` and `backend/data/` directories are local-only; do not add placeholders or exceptions that make their contents trackable. Fresh installs must start with no job-selection records; synthetic records may be used in tests only. Never print private values while checking local state.
- Keep `.env` and secrets local. Add safe defaults to `.env.example`, pass settings through Compose to the process that consumes them, and document each new setting. Never put credentials in the example file.
- Use SVG for icons. Do not use emoji or emoticons in the app or documentation.
- Keep comments short and useful. Use Google-style Go doc comments and JSDoc for exported interfaces; remove comments that merely narrate obvious code. Retain rationale for security-sensitive or non-obvious behavior.
- Keep functions at cyclomatic complexity 10 or lower and avoid cyclic package/module dependencies. Validate untrusted input, use parameterized SQL, preserve escaping and security headers, and bound request, file, and network resources.
- Make database changes with versioned migrations and tests covering new and existing data. Preserve user data; never replace a populated local volume with fixture data.
- When adding a job process, read its job posting and record every explicitly named technology in that process's Tech Stack. Select canonical entries from the shared technology catalog; if a term is missing, add it in Manage technologies and record known alternate names as aliases instead of creating duplicate entries. Do not infer technologies the posting does not mention.
- When a job posting names interviewers or describes hiring rounds, customize the matching interview stages with the round details and assign each named interviewer to the stage where the posting places them. Preserve the stated order, roles, and durations.
- When creating a job card, default its status to Waiting. If it has no referral, set its CV sent date to the card creation date and use the latest CV version already uploaded to the system. In the Company and role overview markdown field, copy the role description from the job posting link, preserving its formatting.
- Keep Go, npm, Docker base images, and GitHub Actions under weekly Dependabot version updates. CI scans production runtime images and dependency manifests daily, and fails if a vulnerability has no EPSS score, an EPSS score above `0.05`, or a CISA Known Exploited Vulnerability entry. CI also scans tracked source for secrets and runs Semgrep Community Edition's `p/security-audit` ruleset with findings treated as errors. The isolated Playwright test image is not a production runtime image and is not part of this vulnerability policy scan.
- Keep production Docker images multi-stage and minimal. Health checks run every 5 seconds; the test runner must wait for both isolated test services to be healthy.
- Use the root `VERSION` file as the only application release version for the frontend and backend together. Change only this file when the user requests a release bump; do not add duplicate app versions to package metadata or image labels. Keep `VERSION` at `0.0.1` for the initial release.
- For release commits, follow `.agents/skills/git-workflow/SKILL.md`: use `type: [vX.Y.Z] - summary`, assign one version per PR, and reuse it for every commit in that PR. Push only when the user explicitly requests it.
- For every minor or major release, add a dated `CHANGELOG.md` entry with `Highlights` and `What changed` sections, covering all changes since the previous published release. Add `Upgrade notes` when users must take manual action. After the release PR merges and CI passes, create and publish the matching GitHub Release tagged `vX.Y.Z`, using those notes as its release description; do not leave a completed release as version or changelog changes only. Never publish patch releases; include them in the next minor or major release.

## Checks before handoff

Before committing, run `docker compose --project-name war-room-precommit up --build --abort-on-container-exit --exit-code-from test test`. It verifies the single root version source, formatting, Go and JavaScript quality rules, backend and frontend unit coverage (90% minimum), backend integration, and Go Playwright tests in order inside Docker. Then run `docker compose --project-name war-room-precommit down`, even if a check fails. Also run `git diff --check`, inspect staged and untracked files, and validate Compose changes. Report blocked checks; do not claim public-production readiness while authentication is absent.

The README is a short human onboarding guide. These instructions and `.agents/skills/pre-commit/SKILL.md` are the operational checklist; do not assume a contributor or agent has read the full README.
