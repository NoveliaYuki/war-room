/**
 * @fileoverview High-performance Vanilla FLIP (First, Last, Invert, Play) animation controller.
 * Delivers hardware-accelerated 60/120fps modal expansion directly from the clicked card.
 */

let activeOriginCard = null;
let closeTimeoutId = null;

/**
 * Cancels any pending close transitions to prevent race conditions when rapidly opening modals.
 */
export function cancelPendingFlipClose() {
  if (closeTimeoutId) {
    clearTimeout(closeTimeoutId);
    closeTimeoutId = null;
  }
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
  activeOriginCard = cardEl;

  // 1. FIRST: Get starting position of the card
  const firstRect = cardEl.getBoundingClientRect();

  // Show backdrop
  backdropEl.classList.add("active");
  modalEl.classList.add("modal-animating");

  // 2. LAST: Get target position of the modal
  const lastRect = modalEl.getBoundingClientRect();

  // 3. INVERT: Calculate delta transform
  const deltaX = firstRect.left - lastRect.left;
  const deltaY = firstRect.top - lastRect.top;
  const scaleX = firstRect.width / lastRect.width;
  const scaleY = firstRect.height / lastRect.height;

  // Apply inversion instantly with no transition
  modalEl.style.transition = "none";
  modalEl.style.transformOrigin = "top left";
  modalEl.style.transform = `translate(${deltaX}px, ${deltaY}px) scale(${scaleX}, ${scaleY})`;
  modalEl.style.borderRadius = "18px";
  modalEl.style.opacity = "0.8";

  // Force synchronous layout reflow
  modalEl.getBoundingClientRect();

  // 4. PLAY: Animate to natural final state
  requestAnimationFrame(() => {
    modalEl.style.transition = `
      transform 340ms cubic-bezier(0.16, 1, 0.3, 1),
      border-radius 340ms cubic-bezier(0.16, 1, 0.3, 1),
      opacity 220ms ease-out
    `;
    modalEl.style.transform = "none";
    modalEl.style.borderRadius = "24px";
    modalEl.style.opacity = "1";

    setTimeout(() => {
      modalEl.classList.remove("modal-animating");
      modalEl.style.transition = "";
    }, 350);
  });
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

  if (!activeOriginCard || !document.body.contains(activeOriginCard)) {
    // Fallback if origin card is no longer present
    modalEl.style.transition = "opacity 200ms ease-out, transform 200ms ease-out";
    modalEl.style.opacity = "0";
    modalEl.style.transform = "scale(0.95)";
    closeTimeoutId = setTimeout(() => {
      closeTimeoutId = null;
      modalEl.style.transition = "";
      modalEl.style.transform = "";
      modalEl.style.opacity = "";
      if (onComplete) onComplete();
    }, 200);
    return;
  }

  const firstRect = modalEl.getBoundingClientRect();
  const lastRect = activeOriginCard.getBoundingClientRect();

  const deltaX = lastRect.left - firstRect.left;
  const deltaY = lastRect.top - firstRect.top;
  const scaleX = lastRect.width / firstRect.width;
  const scaleY = lastRect.height / firstRect.height;

  modalEl.classList.add("modal-animating");
  modalEl.style.transition = `
    transform 280ms cubic-bezier(0.16, 1, 0.3, 1),
    border-radius 280ms cubic-bezier(0.16, 1, 0.3, 1),
    opacity 220ms ease-in
  `;
  modalEl.style.transformOrigin = "top left";
  modalEl.style.transform = `translate(${deltaX}px, ${deltaY}px) scale(${scaleX}, ${scaleY})`;
  modalEl.style.borderRadius = "18px";
  modalEl.style.opacity = "0";

  closeTimeoutId = setTimeout(() => {
    closeTimeoutId = null;
    modalEl.classList.remove("modal-animating");
    modalEl.style.transition = "";
    modalEl.style.transform = "";
    modalEl.style.opacity = "";
    modalEl.style.borderRadius = "";
    activeOriginCard = null;
    if (onComplete) onComplete();
  }, 290);
}
