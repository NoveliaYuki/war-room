/**
 * @fileoverview Top-right progress popup for long-running backup operations.
 * Stays visible with an indeterminate bar while the operation runs, then
 * shows its outcome for 10s before fading out, so a slow backup never
 * reads as the app doing nothing.
 */

let panel = null;
let hideTimer = null;

function ensurePanel() {
  if (!panel || !document.body.contains(panel)) {
    panel = document.createElement("div");
    panel.id = "backup-progress";
    panel.className = "backup-progress";
    panel.setAttribute("role", "status");
    panel.setAttribute("aria-live", "polite");
    panel.innerHTML = `
      <div class="backup-progress-row">
        <span class="backup-progress-icon" aria-hidden="true"></span>
        <span class="backup-progress-message"></span>
        <button type="button" class="backup-progress-close" aria-label="Dismiss">
          <svg aria-hidden="true" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="m18 6-12 12M6 6l12 12"/></svg>
        </button>
      </div>
      <div class="backup-progress-track"><div class="backup-progress-bar"></div></div>
    `;
    panel.querySelector(".backup-progress-close").addEventListener("click", () => {
      clearTimeout(hideTimer);
      panel.classList.remove("backup-progress-show");
    });
    document.body.appendChild(panel);
  }
  return panel;
}

/**
 * Shows an indeterminate progress popup for an in-flight operation.
 *
 * @param {string} message - In-progress message, e.g. "Backing up to Google Drive…".
 * @returns {{success: (message: string) => void, error: (message: string) => void}}
 *   Call one to resolve the popup; it then lingers for 10s before hiding.
 */
export function showBackupProgress(message) {
  const el = ensurePanel();
  clearTimeout(hideTimer);
  el.classList.remove("backup-progress-success", "backup-progress-error");
  el.classList.add("backup-progress-active");
  el.querySelector(".backup-progress-message").textContent = message;

  const safeRaf = typeof requestAnimationFrame === "function" ? requestAnimationFrame : (cb) => setTimeout(cb, 16);
  safeRaf(() => el.classList.add("backup-progress-show"));

  function settle(state, text) {
    el.classList.remove("backup-progress-active");
    el.classList.add(state === "success" ? "backup-progress-success" : "backup-progress-error");
    el.querySelector(".backup-progress-message").textContent = text;
    hideTimer = setTimeout(() => el.classList.remove("backup-progress-show"), 10000);
  }

  return {
    success: (text) => settle("success", text),
    error: (text) => settle("error", text),
  };
}
