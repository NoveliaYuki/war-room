import { markLoadingStarted } from "../utils/loadingTransition.js";

/** Shows the detail layout while job information loads. */
export function renderDetailLoadingSkeleton() {
  const lines = (count = 3) => Array.from({ length: count }, (_, index) => `<span class="detail-skeleton-line${index === count - 1 ? " is-short" : ""}"></span>`).join("");
  return `<div class="detail-loading-skeleton" role="status" aria-label="Loading job details" aria-live="polite">
    <div class="detail-skeleton-header"><span class="detail-skeleton-avatar" aria-hidden="true"></span><div class="detail-skeleton-heading" aria-hidden="true">${lines(2)}</div><div class="detail-skeleton-pills" aria-hidden="true">${lines(3)}</div><button type="button" class="modal-close-btn detail-skeleton-close" aria-label="Close dialog"><svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="m18 6-12 12M6 6l12 12"/></svg></button></div>
    <div class="mobile-detail-view-switch detail-skeleton-mobile-switch" aria-hidden="true"><span></span><span></span></div>
    <div class="modal-split-layout detail-skeleton-layout" aria-hidden="true">
      <div class="split-pane-left detail-skeleton-pane"><div class="detail-skeleton-section-title"></div><div class="detail-skeleton-panel">${lines(4)}</div><div class="detail-skeleton-section-title is-short"></div><div class="detail-skeleton-panel">${lines(3)}</div><div class="detail-skeleton-section-title is-short"></div><div class="detail-skeleton-panel">${lines(5)}</div></div>
      <div class="split-pane-resizer" aria-hidden="true"></div>
      <div class="split-pane-right detail-skeleton-pane"><div class="detail-skeleton-tabs">${lines(3)}</div><div class="detail-skeleton-panel detail-skeleton-hero">${lines(2)}</div><div class="detail-skeleton-panel">${lines(3)}</div><div class="detail-skeleton-panel">${lines(4)}</div></div>
    </div>
  </div>`;
}

/** Shows a schedule-shaped placeholder while meeting data loads. */
export function renderScheduleLoadingState(containerEl) {
  markLoadingStarted(containerEl);
  containerEl.innerHTML = `<div class="schedule-container schedule-loading-state" role="status" aria-label="Loading schedule" aria-live="polite">
    <div class="schedule-skeleton-today"><div class="schedule-skeleton-heading"></div><div class="schedule-skeleton-copy"></div><div class="schedule-skeleton-today-card"><span></span><div><i></i><i></i></div><span></span></div></div>
    <div class="schedule-skeleton-group"><div class="schedule-skeleton-day-heading"><span></span><i></i></div><div class="schedule-skeleton-row"><span class="schedule-skeleton-avatar"></span><div class="schedule-skeleton-row-content"><span class="schedule-skeleton-title"></span><div class="schedule-skeleton-meta"><i></i><i></i></div><span class="schedule-skeleton-note"></span></div><div class="schedule-skeleton-actions"><i></i><i></i></div></div></div>
    <div class="schedule-skeleton-group is-secondary"><div class="schedule-skeleton-day-heading"><span></span><i></i></div><div class="schedule-skeleton-row"><span class="schedule-skeleton-avatar"></span><div class="schedule-skeleton-row-content"><span class="schedule-skeleton-title"></span><div class="schedule-skeleton-meta"><i></i></div><span class="schedule-skeleton-note"></span></div><div class="schedule-skeleton-actions"><i></i></div></div></div>
  </div>`;
}

