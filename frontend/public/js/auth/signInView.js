/**
 * @fileoverview Sign-in gate shown when no backend session exists.
 * Checked by boot.js before app.js is ever imported.
 */

/** Returns the signed-in user, or null when there is no active session. */
export async function checkSession() {
  const response = await fetch("/api/auth/me", { credentials: "same-origin", cache: "no-store" });
  return response.ok ? response.json() : null;
}

/** Hides the app shell and shows the full-page sign-in screen. */
export function renderSignInView() {
  const appContainer = document.querySelector(".app-container");
  if (appContainer) appContainer.hidden = true;
  // The modal backdrop and detail skeleton live outside .app-container and may
  // already be active from showInitialLoadingView(); clear them so no dimmed
  // overlay sits on top of the sign-in screen.
  document.querySelector("#modal-backdrop")?.classList.remove("active");
  const detailModal = document.querySelector("#detail-modal");
  if (detailModal) detailModal.innerHTML = "";
  const screen = document.querySelector("#sign-in-screen");
  if (!screen) return;
  screen.hidden = false;
  const params = new URLSearchParams(window.location.search);
  if (params.get("google") === "error") {
    const message = document.querySelector("#sign-in-message");
    if (message) message.textContent = "Sign-in failed, or this Google account isn't authorized for War Room.";
  }
}
