/**
 * @fileoverview Lightweight, non-intrusive toast notification component.
 * Delivers immediate visual feedback for copy events, autosaves, and status updates.
 */

let toastContainer = null;

/**
 * Returns or creates the persistent toast container in the DOM.
 *
 * @returns {HTMLElement} The toast container element.
 */
function ensureToastContainer() {
  if (!toastContainer || !document.body.contains(toastContainer)) {
    toastContainer = document.createElement("div");
    toastContainer.id = "toast-container";
    toastContainer.className = "toast-container";
    document.body.appendChild(toastContainer);
  }
  return toastContainer;
}

/**
 * Displays a lightweight toast notification.
 *
 * @param {string} message - Notification message.
 * @param {"info"|"success"|"warning"|"error"} [type="info"] - Notification style.
 * @param {number} [duration=2200] - Duration in ms before auto-dismissal.
 */
export function showToast(message, type = "info", duration = 2200) {
  const container = ensureToastContainer();
  const toast = document.createElement("div");
  toast.className = `toast-item toast-${type}`;
  toast.setAttribute("role", "status");
  const text = document.createElement("span");
  text.className = "toast-message";
  text.textContent = String(message);
  toast.appendChild(text);
  container.appendChild(toast);

  // Trigger smooth CSS slide/fade entrance
  const safeRaf = typeof requestAnimationFrame === "function" ? requestAnimationFrame : (cb) => setTimeout(cb, 16);
  safeRaf(() => {
    toast.classList.add("toast-show");
  });

  setTimeout(() => {
    toast.classList.remove("toast-show");
    toast.classList.add("toast-hide");
    const removeToast = () => toast.remove();
    toast.addEventListener("transitionend", removeToast, { once: true });
    setTimeout(removeToast, 300);
  }, duration);
}
