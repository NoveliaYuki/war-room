const FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(",");

let returnFocusTarget = null;
let backgroundState = null;

/** Gives an active dialog an accessible name and moves focus inside it. */
export function activateModal(modal, focusSelector) {
  const background = document.querySelector(".app-container");
  if (!backgroundState) {
    returnFocusTarget = document.activeElement;
  }
  if (background && !backgroundState) {
    backgroundState = {
      element: background,
      inert: background.inert,
      ariaHidden: background.getAttribute("aria-hidden"),
    };
    background.inert = true;
    background.setAttribute("aria-hidden", "true");
  }
  focusModalContents(modal, focusSelector);
}

/** Refreshes a dialog's accessible name and focuses the requested control. */
export function focusModalContents(modal, focusSelector) {
  const heading = modal.querySelector(".modal-title, h1, h2, h3");
  if (heading) {
    heading.id ||= "war-room-modal-title";
    modal.setAttribute("aria-labelledby", heading.id);
    modal.removeAttribute("aria-label");
  } else {
    modal.removeAttribute("aria-labelledby");
    modal.setAttribute("aria-label", "Dialog");
  }

  const requested = focusSelector ? modal.querySelector(focusSelector) : null;
  const target = requested || modal.querySelector(FOCUSABLE_SELECTOR) || modal;
  target.focus();
}

/** Keeps keyboard focus inside the active dialog while it is open. */
export function trapModalTab(event, modal) {
  if (event.key !== "Tab") return false;
  const focusable = [...modal.querySelectorAll(FOCUSABLE_SELECTOR)].filter((element) =>
    !element.hidden && element.getAttribute("aria-hidden") !== "true" && element.getClientRects().length > 0
  );
  if (focusable.length === 0) {
    event.preventDefault();
    modal.focus();
    return true;
  }

  const first = focusable[0];
  const last = focusable[focusable.length - 1];
  if (event.shiftKey && (document.activeElement === first || !modal.contains(document.activeElement))) {
    event.preventDefault();
    last.focus();
  } else if (!event.shiftKey && (document.activeElement === last || !modal.contains(document.activeElement))) {
    event.preventDefault();
    first.focus();
  }
  return true;
}

/** Returns focus to the control that opened the dialog. */
export function restoreModalFocus() {
  if (backgroundState?.element.isConnected) {
    backgroundState.element.inert = backgroundState.inert;
    if (backgroundState.ariaHidden === null) backgroundState.element.removeAttribute("aria-hidden");
    else backgroundState.element.setAttribute("aria-hidden", backgroundState.ariaHidden);
  }
  backgroundState = null;
  const target = returnFocusTarget;
  returnFocusTarget = null;
  if (target?.isConnected && typeof target.focus === "function") target.focus();
}
