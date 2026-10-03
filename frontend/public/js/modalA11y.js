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
const BODY_SCROLL_PROPERTIES = ["position", "top", "left", "right", "width", "overflow"];

/** Gives an active dialog an accessible name and moves focus inside it. */
export function activateModal(modal, focusSelector) {
  const background = document.querySelector(".app-container");
  if (!backgroundState) {
    returnFocusTarget = document.activeElement;
    backgroundState = {
      element: background,
      inert: background?.inert,
      ariaHidden: background?.getAttribute("aria-hidden"),
      scrollX: window.scrollX,
      scrollY: window.scrollY,
      htmlOverflow: document.documentElement.style.overflow,
      bodyStyles: Object.fromEntries(BODY_SCROLL_PROPERTIES.map((property) => [property, document.body.style[property]])),
    };
    if (background) {
      background.inert = true;
      background.setAttribute("aria-hidden", "true");
    }
    document.documentElement.style.overflow = "hidden";
    document.body.style.position = "fixed";
    document.body.style.top = `-${backgroundState.scrollY}px`;
    document.body.style.left = `-${backgroundState.scrollX}px`;
    document.body.style.right = "0";
    document.body.style.width = "100%";
    document.body.style.overflow = "hidden";
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
  const state = backgroundState;
  if (state?.element?.isConnected) {
    state.element.inert = state.inert;
    if (state.ariaHidden === null) state.element.removeAttribute("aria-hidden");
    else state.element.setAttribute("aria-hidden", state.ariaHidden);
  }
  backgroundState = null;
  if (state) {
    document.documentElement.style.overflow = state.htmlOverflow;
    BODY_SCROLL_PROPERTIES.forEach((property) => {
      document.body.style[property] = state.bodyStyles[property];
    });
  }
  const target = returnFocusTarget;
  returnFocusTarget = null;
  if (target?.isConnected && typeof target.focus === "function") target.focus();
  if (state && (state.scrollX || state.scrollY)) window.scrollTo(state.scrollX, state.scrollY);
}
