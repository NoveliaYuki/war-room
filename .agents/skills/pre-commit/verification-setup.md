# Verification setup

How to run the full pre-commit suite and the browser and mobile checks without
rediscovering the setup. `SKILL.md` says what must pass; this file says how.
Everything here uses throwaway stacks and never touches the real app on `:3000`.

## 1. Ground rules

- Never stop, rebuild, or write to the real `war-room` stack or its volume for a check. If a check needs the real ports (Google sign-in and Drive only work on `:3000` and `:4040`), stop that stack without removing it, run an in-memory copy on those ports, then restore the real stack and confirm its data after the user signs in.
- Use a separate Compose project name, separate ports, and in-memory storage for every other stack.
- The shell is zsh: an unquoted variable holding several words is not split. Run multi-flag Compose commands inline or inside `bash -c`.
- The command safety check blocks scripts that contain `rm`, including `docker compose rm`. Reset a throwaway backend with `up -d --force-recreate --no-deps backend` instead; its in-memory data starts empty again.
- Do not bind-mount host temp folders into containers; the mount can show a stale, nearly empty view. Copy files in with `docker cp`.
- Keep scripts and screenshots in the session scratchpad (or `$TMPDIR`), never in the repository.
- Do not print tokens. A failing Google Picker call can echo an OAuth token in its error text; cut error output before printing it.

## 2. Full suite

From the repository root or a worktree (a worktree has no `.env`, so pass the main checkout's):

```sh
docker compose --project-name war-room-precommit --env-file <main checkout>/.env up --build --abort-on-container-exit --exit-code-from test test
docker compose --project-name war-room-precommit down
```

It takes about eight minutes. The runner prints one `=== suite ===` header per gate and exits 1 if any gate failed, even when the last gates pass, so a green last line proves nothing. Find the failing gate by reading the headers in order.

The frontend dependency audit fails when a new advisory is published, even with an unchanged lockfile. Check `git diff origin/main --stat -- '*package*.json' go.mod go.sum` and the Dockerfiles; if the PR changed no dependency, record the failure, open an issue, and fix it in a separate PR rather than bundling it.

## 3. Throwaway verification stack

Save this as an override file outside the repository:

```yaml
services:
  backend:
    environment:
      WARROOM_TEST_MODE: "true"
      SESSION_COOKIE_SECURE: "false"
      AUTH_ALLOWED_EMAILS: "e2e-test@example.com"
      GOOGLE_BACKUP_ENCRYPTION_KEY: "${VERIFY_BACKUP_KEY}"
      BACKUP_SCHEDULER_ENABLED: "false"
      CORS_ALLOWED_ORIGINS: "http://localhost:3300,http://localhost:3500,http://127.0.0.1:3300,http://frontend:8080"
    volumes: !reset []
    tmpfs:
      - /data:uid=65532,gid=65532,mode=0750
```

Start it from the worktree under test, then wait for both services to be healthy:

```sh
export VERIFY_BACKUP_KEY=$(openssl rand -base64 32) FRONTEND_HOST_PORT=3300 BACKEND_HOST_PORT=4340
docker compose --project-name war-room-verify -f docker-compose.yml -f <override> up --build -d backend frontend
```

Test mode adds `POST /api/test/session`, which signs in as the first allowed email without Google. Tear it down with `docker compose ... down` (no `--volumes`).

Baseline: when something looks wrong, run the identical check against `origin/main`. Use `git worktree add --detach <dir> origin/main`, the same override with other ports (for example 3400 and 4440), and a second project name. Behavior that is identical on `main` is not a regression of the PR; report it as pre-existing. Remove the worktree and stack afterwards.

## 4. Signing in and seeding

- In a scriptable browser, call `fetch('/api/test/session', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' })` from the page, then reload.
- Cookies are shared across ports on the same host. Signing in on one stack replaces the cookie of the other, so a signed-in browser suddenly shows the sign-in screen; sign in again on the stack you are testing.
- A browser you cannot script (Firefox on Android, Firefox and Chromium on iOS) can sign in through a one-line form page served from another port, for example `python3 -m http.server 3500` in a folder holding `<form method="post" action="http://localhost:3300/api/test/session" enctype="text/plain"><input type="hidden" name="{}" value=""></form>` with `onload` submitting it. That origin must be in `CORS_ALLOWED_ORIGINS`, or the response is `origin is not allowed`. Expect `{"success":true}`, then open the app.
- Seed through the API from the page: create jobs with `POST /api/jobs` (`company_name`, `position_title`, `create_default_stages: true`), set `PUT /api/jobs/<id>` `{ status: 'ongoing' }`, and add questions with `POST /api/questions` (`stage_id`, `question`). The board's default filter shows only Ongoing processes, so cards that are not Ongoing look like missing data.
- The app restores the last open process and the sort mode from browser storage. Start every run with `localStorage.clear(); sessionStorage.clear()`, then set what you need, then reload; otherwise a leftover detail modal covers the board and locks page scrolling.
- Cards only drag in Manual order. Choose Sort, then Manual order, or set `localStorage['war-room.process.sort.<filter>'] = 'manual'` for `all`, `ongoing`, `waiting`, `accepted`, and `rejected`. In Manual order the board is sorted by position, not by date added, so read the on-screen order; `GET /api/jobs` returns newest first and is the wrong thing to compare.

## 5. Desktop engines (Chromium, Firefox, WebKit)

Use the `mcr.microsoft.com/playwright` image that matches the Playwright version. Put it on the verification stack's network and copy the script in:

```sh
docker run -d --name pw-verify --network <project>_war-room-network -e BASE=http://frontend:8080 --entrypoint sleep mcr.microsoft.com/playwright:<tag> 3000
docker exec pw-verify sh -c 'mkdir -p /t /out && cd /t && npm init -y && npm i playwright@<same version>'
docker cp check.mjs pw-verify:/t/check.mjs
docker exec -w /t pw-verify node check.mjs
docker cp pw-verify:/out/. <results folder>/
docker rm -f pw-verify
```

For each engine, mode (signed in and `?demo`), language, and viewport (desktop and 375 wide), open the changed screen and record overflow, wrong-language text, console errors, and the changed interaction. Harmless noise to filter: the Cross-Origin-Opener-Policy warning for the plain-HTTP origin and a 404 on `/api/company-logo` when no logo is set. Record each engine's version.

## 6. Android emulator

```sh
export ANDROID_HOME="$(brew --prefix)/share/android-commandlinetools" ANDROID_SDK_ROOT="$ANDROID_HOME"
export PATH="$ANDROID_HOME/platform-tools:$ANDROID_HOME/emulator:$PATH"
emulator -avd warroom-firefox-android -no-snapshot-save -no-audio &
adb wait-for-device; adb shell getprop sys.boot_completed      # prints 1 when ready
adb reverse tcp:3300 tcp:3300                                   # repeat for every port the browser needs
```

With `adb reverse`, the browser uses `http://localhost:<port>`; without it use `http://10.0.2.2:<port>`. Start Appium with `./scripts/start-appium.sh` and check `curl http://127.0.0.1:4723/status`.

| Browser | How |
| --- | --- |
| Chrome | Appium UiAutomator2 with `browserName: Chrome` and `appium:chromedriverAutodownload: true` |
| Chromium | Same, plus `appium:chromeOptions: { androidPackage: "org.chromium.chrome" }` and `appium:chromedriverExecutable` pointing to a chromedriver that matches the installed Chromium major version |
| Firefox | No driver is installed. Open pages with `adb shell am start -a android.intent.action.VIEW -d <url> -p org.mozilla.firefox`, read the screen with `adb exec-out screencap -p > shot.png`, and tap with `adb shell input tap x y`. Screenshot pixels equal device pixels (1080 wide), so tap exactly where you see the control |

Touch input. `adb shell input draganddrop` scrolls the page instead of dragging. For a hold-and-drag, send the events yourself: `input motionevent DOWN x y`, sleep 0.9 s (the card hold is 500 ms), then `MOVE` in small steps with a short sleep, then `UP`. Convert page coordinates to device pixels with a calibration tap: add a `pointerdown` listener that stores `clientX/clientY`, tap a known device point, and derive the content offset and scale (about 106 CSS pixels of browser chrome at a pixel ratio of 2.625 on the emulator, but measure it).

## 7. iOS simulator

```sh
export DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer
xcrun simctl list devices available
xcrun simctl boot <UDID>; xcrun simctl bootstatus <UDID> -b
```

- Safari with Appium XCUITest: use one session for the whole run. Capabilities: `browserName: Safari`, `appium:safariInitialUrl: ""`, `appium:noReset: true`. Open the URL first with `xcrun simctl openurl <UDID> <url>` and wait about ten seconds; a fresh session otherwise stalls with "Maximum occupation time is exceeded in queue".
- Calibrate screen points to page coordinates with the same tap-and-listener method (on this simulator the page starts 62 points below the screen top).
- Taps and a handle drag using W3C pointer actions work. For swipes use `mobile: dragFromToForDuration` with a short duration, and never start a swipe at the left screen edge (x near 20): it is Safari's back gesture.
- Safari keeps its page and storage between sessions; clear storage at the start of each run (section 4).
- Firefox and Chromium are not installed by default and have no web automation. Install the local builds with `xcrun simctl install <UDID> <app>`: the Chromium simulator build is under `chromium-ios/src/out/Debug-iphonesimulator/Chromium.app` (bundle `org.chromium.ost.chrome.ios.dev`), and the Firefox build is `Client.app` under `~/Library/Developer/Xcode/DerivedData/Client/Build/Products/Debug-iphonesimulator/` (bundle `org.mozilla.ios.Fennec`). Check `xcrun simctl listapps <UDID>` first; do not report a browser as unavailable without looking.
- Drive them natively through Appium with `appium:bundleId`, `appium:noReset`, and `appium:forceAppLaunch: false`: tap by screen point (screenshot pixels divided by the pixel ratio, 3 on this device), type with `mobile: keys`, and read the screen with `xcrun simctl io <UDID> screenshot`. Chromium's first run shows prompts (default browser, Lens, import from Safari); dismiss each. Its address bar is at the top of the screen once a page is open. The Firefox build can sit on a blank onboarding card where Skip does nothing; if so, record it as blocked.
- An "Open in" system prompt can appear for custom URL schemes; tap Open.
- Branded Google Chrome cannot be installed in the simulator; record it as unavailable.

## 8. Touch checks (card drag, question handle, status, scroll)

Do these on every available mobile browser and compare against `origin/main` before calling anything a regression.

1. Manual order selected and storage cleared (section 4).
2. Scroll: swipe in the middle of the screen; the page must scroll, and still scroll after a drag.
3. Card drag: hold a card's handle past 500 ms, then move to the target. While dragging, the page must not scroll and the body is locked; after release the lock is gone. Keep the target inside the middle band of the screen: the app auto-scrolls within about 110 px of the top (below the header) and bottom edges, which moves the board under the finger and ruins the drop.
4. Question handle: the handle starts a drag immediately; drag it above another question and confirm the new order persists after a reload.
5. Status label: tap it through Ongoing, Rejected, Approved, Waiting, and back to Ongoing.
6. To see what a drag does, sample `.process-card.is-holding`, `.process-card.is-dragging`, `scrollY`, and the body `position` every 50 ms from the page while the gesture runs, then read the samples afterwards. A W3C action cannot be paused halfway, so mid-gesture inspection has to come from the page.

## 9. Known results and limits (v0.1.0)

- Android Firefox: a card lifted by hold-and-drag stays stuck at the bottom of the page and never drops. `origin/main` behaves the same, so it is pre-existing; it may also be an artifact of injected touches, so check by hand.
- iOS Safari: the hold starts a drag, locks scrolling, and ends cleanly, but synthetic gestures did not reorder cards on either build. The question handle, status cycle, and scrolling pass.
- The Firefox iOS build can stall on onboarding in the simulator (section 7).
- File pickers cannot be scripted on mobile; hand the input a `File` through `DataTransfer` in the page instead.

## 10. Cleanup checklist

- Stop and `down` every verification project; confirm only the real `war-room` stack is running (`docker ps`).
- Remove temporary worktrees with `git worktree remove --force <dir>`; check `git worktree list` and `git status --short`.
- Stop Appium and the form-page server; shut down the emulator (`adb emu kill`) and the simulator (`xcrun simctl shutdown <UDID>`).
- Restore the real stack if it was stopped, rebuilt from the final tree, and ask the user to sign in so its data can be compared with the pre-check baseline.
- List any files the checks created in the user's Drive so they can delete them; never confirm Revoke.
