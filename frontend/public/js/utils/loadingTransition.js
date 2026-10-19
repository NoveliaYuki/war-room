const LOADING_VIEW_SELECTOR = ".detail-loading-skeleton, .schedule-loading-state, .board-loading-state";
const loadingStartTimes = new WeakMap();
const loadingRevealTimers = new WeakMap();
const LOADING_FEEDBACK_DELAY_MS = 250;

/** Tracks a loading cycle, preserving its start when the same skeleton is refreshed. */
export function markLoadingStarted(container) {
  if (loadingStartTimes.has(container) && container.querySelector(LOADING_VIEW_SELECTOR)) return;
  window.clearTimeout(loadingRevealTimers.get(container));
  loadingStartTimes.set(container, performance.now());
  container.classList.remove("loading-placeholder-ready");
  const timer = window.setTimeout(() => {
    if (container.querySelector(LOADING_VIEW_SELECTOR)) container.classList.add("loading-placeholder-ready");
  }, LOADING_FEEDBACK_DELAY_MS);
  loadingRevealTimers.set(container, timer);
}

/** Captures the visible loading layout before its contents are replaced. */
export function captureLoadingView(container) {
  const now = performance.now();
  const loadDurationMs = Math.max(0, now - (loadingStartTimes.get(container) ?? now));
  const placeholderWasVisible = container.classList.contains("loading-placeholder-ready");
  loadingStartTimes.delete(container);
  window.clearTimeout(loadingRevealTimers.get(container));
  loadingRevealTimers.delete(container);
  container.classList.remove("loading-placeholder-ready");
  if (loadDurationMs < LOADING_FEEDBACK_DELAY_MS || !placeholderWasVisible) return null;
  if (typeof container.animate !== "function") return null;
  if (window.matchMedia?.("(prefers-reduced-motion: reduce)").matches) return null;
  const loadingView = container.querySelector(LOADING_VIEW_SELECTOR);
  if (!loadingView) return null;
  const bounds = loadingView.getBoundingClientRect();
  if (!bounds.width || !bounds.height) return null;
  const hostBounds = container.getBoundingClientRect();
  return {
    loadDurationMs,
    element: loadingView.cloneNode(true),
    top: bounds.top - hostBounds.top - container.clientTop + container.scrollTop,
    left: bounds.left - hostBounds.left - container.clientLeft + container.scrollLeft,
    width: bounds.width,
    height: bounds.height,
  };
}

/** Dissolves the placeholders before gently revealing content in the same panel. */
export function revealLoadedContent(container, snapshot) {
  if (!snapshot) return;
  const contents = [...container.children];
  const overlay = document.createElement("div");
  overlay.className = "loading-crossfade-overlay";
  overlay.inert = true;
  overlay.setAttribute("aria-hidden", "true");
  Object.assign(overlay.style, {
    top: `${snapshot.top}px`, left: `${snapshot.left}px`,
    width: `${snapshot.width}px`, height: `${snapshot.height}px`,
  });
  overlay.append(snapshot.element);
  container.classList.add("loading-content-host");
  container.append(overlay);
  const totalDuration = Math.round(160 + 80 * (1 - Math.exp(-(snapshot.loadDurationMs - LOADING_FEEDBACK_DELAY_MS) / 1000)));
  const revealDelay = Math.round(totalDuration * 0.28);
  const contentTiming = {
    duration: totalDuration - revealDelay, delay: revealDelay,
    easing: "cubic-bezier(0.4, 0, 0.2, 1)", fill: "both",
  };
  const animations = contents.map((element) => element.animate([{ opacity: 0 }, { opacity: 1 }], contentTiming));
  animations.push(overlay.animate([{ opacity: 1 }, { opacity: 0 }], {
    duration: Math.round(totalDuration * 0.32), easing: "cubic-bezier(0.4, 0, 1, 1)", fill: "both",
  }));
  void Promise.allSettled(animations.map((animation) => animation.finished)).then(() => {
    overlay.remove();
    animations.forEach((animation) => animation.cancel());
    if (!container.querySelector(".loading-crossfade-overlay")) container.classList.remove("loading-content-host");
  });
}
