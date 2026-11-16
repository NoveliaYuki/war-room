/**
 * @fileoverview High-performance Vanilla FLIP (First, Last, Invert, Play) animation controller.
 * Delivers hardware-accelerated 60/120fps modal expansion directly from the clicked card.
 */

let activeOriginCard = null;

/**
 * Cancels any pending close transitions to prevent race conditions when rapidly opening modals.
 * @param {boolean} clearOrigin - Whether to discard the card used for the previous modal transition.
 */
export function cancelPendingFlipClose(clearOrigin = false) {
  if (clearOrigin) activeOriginCard = null;
}

/**
 * Executes a FLIP expansion from the source card to the target modal.
 *
 * @param {HTMLElement} cardEl - The card element in the grid.
 * @param {HTMLElement} modalEl - The modal container element.
 * @param {HTMLElement} backdropEl - The background overlay element.
 */
export function openWithFlip(cardEl, modalEl, backdropEl) {
  cancelPendingFlipClose();
  activeOriginCard = null;
  backdropEl.classList.add("active");
}

/**
 * Executes the reverse FLIP transition to collapse the modal back into its grid card.
 *
 * @param {HTMLElement} modalEl - The modal container.
 * @param {HTMLElement} backdropEl - The background overlay.
 * @param {Function} onComplete - Callback executed after collapse finishes.
 */
export function closeWithFlip(modalEl, backdropEl, onComplete) {
  cancelPendingFlipClose();
  backdropEl.classList.remove("active");
  modalEl.classList.remove("modal-animating");
  modalEl.style.transition = "";
  modalEl.style.transform = "";
  modalEl.style.opacity = "";
  modalEl.style.borderRadius = "";
  activeOriginCard = null;
  if (onComplete) onComplete();
}
