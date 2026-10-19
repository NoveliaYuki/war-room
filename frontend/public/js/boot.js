/**
 * @fileoverview Entry point that selects the demo or backend app mode.
 * Loaded as an external module to comply with the Content Security Policy.
 */

import { renderDetailLoadingSkeleton, renderScheduleLoadingState } from "./components/loadingViews.js";
import { activateModal, restoreModalFocus } from "./modalA11y.js";
import { markLoadingStarted } from "./utils/loadingTransition.js";

/** Restores the saved view's loading shell before requesting application data. */
function showInitialLoadingView() {
  markLoadingStarted(document.querySelector("#cards-grid"));
  let savedJob = "";
  let schedule = false;
  try {
    savedJob = window.localStorage.getItem("war-room.open-job") || "";
    schedule = window.localStorage.getItem("war-room.active-filter") === "schedule";
  } catch { /* Use the board skeleton when storage is unavailable. */ }
  if (schedule) {
    const grid = document.querySelector("#cards-grid");
    grid.classList.add("schedule-mode");
    renderScheduleLoadingState(grid);
    document.querySelector("#process-toolbar").hidden = true;
    document.querySelector("#tab-board").classList.remove("active");
    document.querySelector("#tab-board").setAttribute("aria-pressed", "false");
    document.querySelector("#tab-schedule").classList.add("active");
    document.querySelector("#tab-schedule").setAttribute("aria-pressed", "true");
  }
  if (!savedJob) return;
  const modal = document.querySelector("#detail-modal");
  markLoadingStarted(modal);
  modal.innerHTML = renderDetailLoadingSkeleton();
  modal.setAttribute("aria-busy", "true");
  document.querySelector("#modal-backdrop").classList.add("active");
  modal.querySelector(".modal-close-btn").addEventListener("click", () => {
    try { window.localStorage.removeItem("war-room.open-job"); } catch { /* Closing remains available without storage. */ }
    document.querySelector("#modal-backdrop").classList.remove("active");
    modal.innerHTML = "";
    modal.removeAttribute("aria-busy");
    restoreModalFocus();
  });
  activateModal(modal);
}

showInitialLoadingView();

const configuredMode = await fetch("/runtime-config.json", { cache: "no-store" })
  .then((response) => response.ok ? response.json() : null)
  .catch(() => null);
const demo = new URLSearchParams(window.location.search).has("demo") || configuredMode?.mode === "demo";
document.body.dataset.demo = String(demo);

if (demo) {
  const [{ demoApi }, { useApiAdapter }] = await Promise.all([
    import("/demo/demoStore.js"),
    import("./api.js"),
  ]);
  useApiAdapter(demoApi);
}

await import("./app.js");
