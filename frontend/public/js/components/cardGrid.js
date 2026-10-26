import { showToast } from "../utils/toast.js";
/**
 * @fileoverview Card Grid View Component.
 * Renders process cards, handles real-time mouse glow tracking, quick deletion,
 * quick addition, and triggers FLIP expansion.
 */

import { api } from "../api.js";
import { renderCompanyAvatar } from "../avatar.js";
import { openDetailModal } from "./detailModal.js";
import { icon } from "../icons.js";
import { makeInlineEditable, parseSalaryInput } from "../inlineEdit.js";
import { escapeHtml, escapeAttr } from "../utils/sanitize.js";
import { formatSalary } from "../utils/salary.js";
import { confirmAction } from "../utils/confirmDialog.js";

const EMPLOYMENT_TYPE_LABELS = new Map([
	["permanent", "Permanent"],
	["b2b", "B2B"],
	["permanent_b2b", "Permanent / B2B"],
]);
const CARD_TECHNOLOGY_PREVIEW_LIMIT = 4;

const EMPTY_STATUS_COPY = {
	waiting: { title: "No waiting processes yet", description: "Processes you mark as waiting will appear here." },
	ongoing: { title: "No ongoing processes yet", description: "Processes you mark as ongoing will appear here." },
	accepted: { title: "No accepted processes yet", description: "Processes you mark as accepted will appear here." },
	rejected: { title: "No rejected processes yet", description: "Processes you mark as rejected will appear here." },
};

/**
 * Formats the salary label used on preview cards.
 *
 * @param {string} type - Salary range type from the backend.
 * @param {number|null} min - Optional lower salary bound.
 * @param {number|null} max - Optional upper salary bound.
 * @param {string} [currency='EUR'] - ISO currency code.
 * @returns {string} Formatted salary label.
 */
export const formatSalaryShort = formatSalary;

/** Returns whether a company is represented as undisclosed. */
function isUnknownCompany(companyName) {
  return companyName.toLowerCase() === "unknown";
}

/** Renders the stage summary displayed in a card footer. */
function getStageIndicator(job) {
  if (job.current_stage_title) {
    const text = `Step ${job.current_stage_index}/${job.total_stages_count}: ${job.current_stage_title}`;
    return { text, markup: `<span class="inline-icon-text">${icon("pin", 12)} Step ${job.current_stage_index}/${job.total_stages_count}: ${escapeHtml(job.current_stage_title)}</span>` };
  }
  if (job.total_stages_count > 0) {
    const text = `${job.total_stages_count} interview stages`;
    return { text, markup: `<span class="inline-icon-text">${icon("target", 12)} ${job.total_stages_count} interview stages</span>` };
  }
  return { text: "No stages yet", markup: "<span>No stages yet</span>" };
}

/** Renders the company label displayed in a job card. */
function renderCardCompany(companyName) {
  return isUnknownCompany(companyName)
    ? `<span class="inline-icon-text">${icon("building", 13)} Unknown Company</span>`
    : escapeHtml(companyName);
}

/** Returns the unformatted salary value retained for inline editing. */
function getCardSalaryRawValue(job) {
  if (job.salary_min && job.salary_max) return `${job.salary_min} - ${job.salary_max}`;
  return job.salary_max || job.salary_min || "Salary undisclosed";
}

/** Renders the referral status tag for a job card. */
function renderReferralTag(job) {
  const stateClass = job.is_referral ? "is-referral" : "not-referral";
  const label = job.is_referral ? "Referral" : "No referral";
  const action = job.is_referral ? "Remove referral" : "Mark as referral";
  return `<button type="button" class="referral-tag ${stateClass} editable-card-referral inline-icon-text" data-id="${escapeAttr(job.id)}" aria-pressed="${Boolean(job.is_referral)}" aria-label="${label}. ${action}." title="${action}">${icon("userCheck", 11)} ${label}</button>`;
}

/** Renders a job's employment type on its card. */
function renderEmploymentTypeTag(employmentType) {
	const label = EMPLOYMENT_TYPE_LABELS.get(employmentType);
  return label ? `<span class="employment-type-tag">${escapeHtml(label)}</span>` : "";
}

/** Renders a concise known work arrangement on a job card. */
function renderWorkArrangementTag(arrangement) {
  const labels = new Map([
    ["remote", "Remote"],
    ["hybrid", "Hybrid"],
    ["on_site", "On-site"],
  ]);
  const label = labels.get(arrangement);
  return label ? `<span class="work-arrangement-tag">${label}</span>` : "";
}

/** Returns role highlights without details already shown elsewhere on the card. */
function getCardRoleHighlights(job) {
  const excludedValues = [job.company_name, job.recruiter_name, job.recruiter_agency]
    .filter(Boolean)
    .map((value) => value.trim().toLowerCase());
  return (job.keyword_note || "")
    .split(/[•|]/)
    .map((part) => part.trim())
    .filter((part) => {
      if (!part) return false;
      const normalized = part.toLowerCase();
      if (excludedValues.some((value) => normalized.includes(value))) return false;
      if (/\b(?:referral|remote|hybrid|on[- ]site|permanent|b2b)\b/i.test(part)) return false;
      if (/[€$£]\s?\d|\b(?:EUR|USD|GBP)\s?\d|\d\s?(?:k|000)\b/i.test(part)) return false;
      return true;
    });
}

/** Renders the role highlights in the card's consistent summary slot. */
function renderCardRoleHighlights(job) {
  const highlights = getCardRoleHighlights(job);
  if (!highlights.length) return "";
  return `<div class="card-summary"><div class="card-summary-label">Role highlights</div><div class="keyword-note editable-card-keyword" data-raw-value="${escapeAttr(job.keyword_note || "")}" title="Role highlights">${escapeHtml(highlights.join(" • "))}</div></div>`;
}

function renderCardTechnologyStack(job) {
  const technologies = job.technologies || [];
  if (!technologies.length) return "";
  const visibleTechnologies = technologies.slice(0, CARD_TECHNOLOGY_PREVIEW_LIMIT);
  const remainingCount = technologies.length - visibleTechnologies.length;
  const contents = `${visibleTechnologies.map((technology) => {
    const alias = technology.aliases?.find((item) => item.trim() && item.trim().length < technology.name.length);
    const label = alias || technology.name;
    const title = alias ? ` title="${escapeAttr(technology.name)}"` : "";
    return `<span class="card-technology-chip"${title}>${escapeHtml(label)}</span>`;
  }).join("")}${remainingCount ? `<span class="card-technology-more" title="Open details to view the full stack">+${remainingCount} more</span>` : ""}`;
  const previewLabel = remainingCount
    ? `Technology stack preview: ${visibleTechnologies.length} shown, ${remainingCount} more in process details.`
    : `Technology stack: ${technologies.length} items.`;
  return `<div class="card-tech-stack" role="group" aria-label="${escapeAttr(previewLabel)}"><div class="card-summary-label">Tech stack <span class="card-tech-count">${technologies.length}</span></div><div class="card-technology-list">${contents}</div></div>`;
}

/** Formats date-only fields without shifting them across time zones. */
function formatCardDate(value) {
  if (!value) return "";
  const date = /^\d{4}-\d{2}-\d{2}$/.test(value) ? new Date(`${value}T12:00:00`) : new Date(Number(value) * 1000);
  if (Number.isNaN(date.getTime())) return "";
  return new Intl.DateTimeFormat(undefined, { day: "numeric", month: "short" }).format(date);
}

/** Renders the creation and optional application/contact dates on a job card. */
function renderCardProcessDates(job) {
  const addedDate = formatCardDate(job.created_at);
  const cvSentDate = formatCardDate(job.application_sent_date);
  const dates = addedDate && cvSentDate && addedDate === cvSentDate
    ? [["Added & CV sent", addedDate], ["Recruiter", formatCardDate(job.recruiter_first_contact_date)]]
    : [["Added", addedDate], ["CV sent", cvSentDate], ["Recruiter", formatCardDate(job.recruiter_first_contact_date)]];
  const availableDates = dates.filter(([, date]) => date);
  return `<div class="card-process-dates" aria-label="Process dates">${availableDates.map(([label, date]) => `<span><strong>${label}</strong> ${escapeHtml(date)}</span>`).join("")}</div>`;
}

/** Creates and fills the static markup for one job card. */
function createCardElement(job, sortMode) {
  const card = document.createElement("div");
  card.className = "process-card";
  card.style.touchAction = "pan-y";
  card.setAttribute("data-id", job.id);
  card.addEventListener("mousemove", (event) => updateCardGlow(card, event));
  const avatarHtml = renderCompanyAvatar(job.company_name, job.avatar_seed, 64, job.company_domain, job.id);
  const stageIndicator = getStageIndicator(job);
  const inactivityNote = job.automatic_rejection_reason
    ? `<div class="card-automatic-rejection" role="note">Demo: ${escapeHtml(job.automatic_rejection_reason)}</div>`
    : "";
  const salaryText = formatSalaryShort(job.salary_type, job.salary_min, job.salary_max, job.salary_currency);
  const companyClass = isUnknownCompany(job.company_name) ? "is-unknown" : "";
  const accessibleCompanyName = isUnknownCompany(job.company_name) ? "undisclosed company" : job.company_name;
  const processLabel = `${job.position_title} at ${accessibleCompanyName}`;
  const salaryClass = job.salary_type === "unknown" ? "undisclosed" : "";
  const dragHandle = sortMode === "manual"
    ? `<button class="btn-card-drag" type="button" aria-label="Reorder ${escapeAttr(processLabel)}" aria-keyshortcuts="ArrowUp ArrowDown" title="Hold and drag to reorder · use ↑ or ↓ to move">${icon("gripLines", 15)}</button>`
    : "";
  card.innerHTML = `<div class="card-content"><div class="card-header"><div class="card-avatar">${avatarHtml}</div><div class="card-title-group">
    <div class="card-company editable-card-company ${companyClass}" data-raw-value="${escapeAttr(job.company_name)}">${renderCardCompany(job.company_name)}</div>
    <h3 class="card-position editable-card-position" data-raw-value="${escapeAttr(job.position_title)}">${escapeHtml(job.position_title)}</h3>
  </div><div class="card-header-actions"><span class="status-pill ${escapeAttr(job.status)}">${escapeHtml(job.status)}</span>
    <button class="btn-card-delete" type="button" aria-label="Delete selection process for ${escapeAttr(processLabel)}" title="Delete this selection process" data-id="${escapeAttr(job.id)}">${icon("trash", 15)}</button></div></div>
  <div class="card-body"><span class="salary-tag editable-card-salary ${salaryClass}" data-raw-value="${escapeAttr(getCardSalaryRawValue(job))}">${escapeHtml(salaryText)}</span>
    ${renderReferralTag(job)}${renderEmploymentTypeTag(job.employment_type)}${renderWorkArrangementTag(job.work_arrangement)}</div>
  ${renderCardRoleHighlights(job)}
  ${renderCardProcessDates(job)}
  ${inactivityNote}
  ${renderCardTechnologyStack(job)}</div>
  <div class="card-footer">
    <div class="card-stage-indicator"><span class="card-stage-summary" title="${escapeAttr(stageIndicator.text)}">${stageIndicator.markup}</span>
      <div class="card-footer-actions">${dragHandle}<button class="btn-card-open-details" type="button" aria-label="Open details for ${escapeAttr(processLabel)}" title="Open details">${icon("arrowUpRight", 14)}</button></div></div></div>`;
  setWideLogoFrame(card);
  return card;
}

/** Expands landscape logos while preserving the square frame for compact marks. */
function setWideLogoFrame(card) {
  const logo = card.querySelector(".company-logo-img");
  if (!logo) return;

  const applyFrame = () => {
    if (logo.naturalHeight === 0 || logo.naturalWidth / logo.naturalHeight <= 1.2) return;
    card.querySelector(".card-avatar")?.classList.add("card-avatar-wide-logo");
    card.querySelector(".card-header")?.classList.add("has-wide-logo");
  };

  logo.addEventListener("load", applyFrame, { once: true });
  if (logo.complete) applyFrame();
}

/** Creates the action card used to start a new selection process. */
function createQuickAddCard() {
  const addCard = document.createElement("button");
  addCard.className = "quick-add-card";
  addCard.type = "button";
  addCard.setAttribute("aria-label", "Add selection process");
  addCard.innerHTML = `
    <div class="quick-add-icon">${icon("plus", 22)}</div>
    <div class="quick-add-title">Add Selection Process</div>
    <div class="quick-add-subtitle">Track a new job opportunity or recruiter reachout</div>
  `;
  addCard.addEventListener("click", () => {
    document.querySelector("#btn-new-process")?.click();
  });
  return addCard;
}

/** Updates the CSS mouse position for the card's glow effect. */
function updateCardGlow(card, event) {
  const rect = card.getBoundingClientRect();
  card.style.setProperty("--mouse-x", `${event.clientX - rect.left}px`);
  card.style.setProperty("--mouse-y", `${event.clientY - rect.top}px`);
}

/** Registers inline editors for a card's editable job fields. */
function bindCardInlineEditors(card, job, onGlobalRefresh) {
  const editors = [
    [".editable-card-company", { placeholder: "Company Name", field: "company_name", fallback: "Unknown" }],
    [".editable-card-position", { placeholder: "Position Title", field: "position_title", required: true }],
  ];
  editors.forEach(([selector, options]) => {
    const element = card.querySelector(selector);
    if (element) bindTextEditor(element, job, options, onGlobalRefresh);
  });
  const salary = card.querySelector(".editable-card-salary");
  if (salary) makeInlineEditable(salary, {
    placeholder: "Salary range",
    onSave: async (value) => {
      await api.updateJob(job.id, parseSalaryInput(value));
      onGlobalRefresh?.();
    },
  });
  const keyword = card.querySelector(".editable-card-keyword");
  if (keyword) bindTextEditor(keyword, job, { placeholder: "Role highlights...", field: "keyword_note", maxLength: 100 }, onGlobalRefresh);
  bindCardReferralToggle(card.querySelector(".editable-card-referral"), job, onGlobalRefresh);
}

/** Binds a simple text editor to a single job field. */
function bindTextEditor(element, job, options, onGlobalRefresh) {
  makeInlineEditable(element, {
    placeholder: options.placeholder,
    maxLength: options.maxLength,
    onSave: async (value) => {
      if (options.required && !value) return;
      const nextValue = value || options.fallback || value;
      await api.updateJob(job.id, { [options.field]: nextValue });
      onGlobalRefresh?.();
    },
  });
}

/** Binds the referral status toggle to its card badge. */
function bindCardReferralToggle(element, job, onGlobalRefresh) {
  if (!element) return;
  element.addEventListener("click", async (event) => {
    event.preventDefault();
    event.stopPropagation();
    if (element.disabled) return;
    element.disabled = true;
    try {
      const nextValue = !job.is_referral;
      await api.updateJob(job.id, { is_referral: nextValue });
      if (typeof window !== "undefined" && typeof window.showToast === "function") {
        window.showToast(nextValue ? "Marked as Referral" : "Marked as Non-referral", "success", 1200);
      }
      await onGlobalRefresh?.();
      const updatedButton = [...document.querySelectorAll(".editable-card-referral")]
        .find((button) => button.dataset.id === String(job.id));
      (updatedButton || document.querySelector("#process-filter-trigger"))?.focus();
    } catch (error) {
      showToast(error.message, "error");
    } finally {
      if (element.isConnected) element.disabled = false;
    }
  });
}

/** Binds deletion to a card's delete button. */
function bindCardDeletion(card, job, onGlobalRefresh) {
  card.querySelector(".btn-card-delete").addEventListener("click", async (event) => {
    try {
      event.stopPropagation();
      const confirmed = await confirmAction({
        title: "Delete selection process?",
        message: `${job.company_name} — ${job.position_title} will be permanently removed.`,
        confirmLabel: "Delete process",
        danger: true,
      });
      if (!confirmed) return;
      const cardsBeforeDelete = [...(card.parentElement?.querySelectorAll(".process-card") || [])];
      const currentIndex = cardsBeforeDelete.indexOf(card);
      const adjacentCardId = cardsBeforeDelete[currentIndex + 1]?.dataset.id
        || cardsBeforeDelete[currentIndex - 1]?.dataset.id;
      await api.deleteJob(job.id);
      await onGlobalRefresh?.();
      const adjacentCard = [...document.querySelectorAll(".process-card")]
        .find((item) => item.dataset.id === adjacentCardId);
      const focusTarget = adjacentCard?.querySelector(".btn-card-open-details")
        || document.querySelector("#cards-grid .btn-create-first-process, #cards-grid .btn-reset-process-view");
      focusTarget?.focus();
    } catch (error) {
      showToast(error.message, "error");
    }
  });
}

/** Opens a job detail modal unless the user is currently dragging the card. */
async function openCardDetails(job, modalEl, backdropEl, onGlobalRefresh, isDragActive) {
  if (isDragActive()) return;
  const targetModal = modalEl || document.querySelector("#detail-modal");
  const targetBackdrop = backdropEl || document.querySelector("#modal-backdrop");
  await openDetailModal(targetModal, targetBackdrop, job.id, onGlobalRefresh);
}

/** Returns whether a card click originated from a nested control. */
function isNestedControl(target) {
  return Boolean(target.closest("button, a, input, textarea"));
}

/** Binds card click and double-click behavior for opening details. */
function bindCardNavigation(card, job, modalEl, backdropEl, onGlobalRefresh, isDragActive) {
  const detailClickDelayMs = 500;
  let cardClickTimer = null;
  const openDetails = () => openCardDetails(job, modalEl, backdropEl, onGlobalRefresh, isDragActive);
  card.querySelector(".btn-card-open-details")?.addEventListener("click", openDetails);
  card.addEventListener("click", (event) => {
    if (isDragActive() || isNestedControl(event.target)) return;
    if (event.target.closest(".editable-text") && event.pointerType !== "touch") {
      if (cardClickTimer) clearTimeout(cardClickTimer);
      cardClickTimer = setTimeout(() => {
        cardClickTimer = null;
        openDetails();
      }, detailClickDelayMs);
      return;
    }
    openDetails();
  });
  card.addEventListener("dblclick", () => {
    if (!cardClickTimer) return;
    clearTimeout(cardClickTimer);
    cardClickTimer = null;
  }, true);
}

function renderEmptyState(containerEl, context) {
  const hasProcesses = (context.totalCount ?? 0) > 0;
  const hasCustomView = Boolean(context.hasCustomView) && hasProcesses;
  const statusCopy = Object.hasOwn(EMPTY_STATUS_COPY, context.filter)
    ? EMPTY_STATUS_COPY[context.filter]
    : null;
  const statusTabIsEmpty = Boolean(statusCopy && context.statusCount === 0 && hasProcesses);
  const copy = resolveEmptyStateCopy(hasProcesses, hasCustomView, statusTabIsEmpty, statusCopy);
  const empty = document.createElement("div");
  empty.className = "empty-state";
  empty.innerHTML = `
    <div class="empty-icon">${icon("briefcase", 36)}</div>
    <h3 style="margin-bottom: 8px;">${copy.title}</h3>
    <p style="color: var(--text-secondary); max-width: 400px; margin: 0 auto 16px;">${copy.description}</p>
    ${hasCustomView
      ? '<button class="btn-secondary btn-reset-process-view" type="button">Reset search and filters</button>'
      : `<button class="btn-primary btn-create-first-process" type="button">
          <span class="inline-icon-text">${icon("plus", 14)} ${hasProcesses ? "Create Selection Process" : "Create First Selection Process"}</span>
        </button>`}
  `;
  empty.querySelector(".btn-create-first-process")?.addEventListener("click", () => {
    document.getElementById("btn-new-process")?.click();
  });
  empty.querySelector(".btn-reset-process-view")?.addEventListener("click", () => context.onResetView?.());
  containerEl.appendChild(empty);
}

function resolveEmptyStateCopy(hasProcesses, hasCustomView, statusTabIsEmpty, statusCopy) {
  if (hasCustomView) {
    return {
      title: "No selection processes match this view",
      description: "Try different criteria, or reset your search, period, and filters to see all processes.",
    };
  }
  if (statusTabIsEmpty) return statusCopy;
  if (hasProcesses) {
    return {
      title: "No selection processes match your current search or filters",
      description: "Try adjusting or clearing your search and filters.",
    };
  }
  return {
    title: "No selection processes found",
    description: "Track your interviews, recruiters, salary negotiations, and interview questions in one place.",
  };
}

/**
 * Renders all job selection cards into the grid container.
 *
 * @param {HTMLElement} containerEl - The DOM container for the grid.
 * @param {Array} jobs - Array of job summary records.
 * @param {HTMLElement} modalEl - The modal container element.
 * @param {HTMLElement} backdropEl - The modal backdrop element.
 * @param {Function} onGlobalRefresh - Global refresh callback.
 * @param {string} [sortMode='manual'] - Recent ordering or the user's saved manual order.
 * @param {{filter?: string, totalCount?: number, statusCount?: number, hasCustomView?: boolean, groupByStatus?: boolean, onResetView?: () => void}} [emptyStateContext] - Counts, grouping, and active tab for an empty grid.
 */
export function renderCardGrid(containerEl, jobs = [], modalEl = null, backdropEl = null, onGlobalRefresh = null, sortMode = "manual", emptyStateContext = {}) {
  containerEl.innerHTML = "";

  if (jobs.length === 0) {
    renderEmptyState(containerEl, emptyStateContext);
    return;
  }

  const groupedJobs = emptyStateContext.groupByStatus
    ? ["accepted", "ongoing", "waiting", "rejected"].flatMap((status) => {
      const statusJobs = jobs.filter((job) => job.status === status);
      return statusJobs.length ? [{ status, jobs: statusJobs }] : [];
    })
    : [{ status: "", jobs }];

  // 1. Render selection cards, optionally grouped by status.
  groupedJobs.forEach(({ status, jobs: statusJobs }) => {
    if (status) {
      const heading = document.createElement("h2");
      heading.className = `process-status-group-heading status-${status}`;
      heading.dataset.status = status;
      const label = document.createElement("span");
      label.textContent = status[0].toUpperCase() + status.slice(1);
      const count = document.createElement("span");
      count.className = "process-status-group-count";
      count.textContent = String(statusJobs.length);
      count.setAttribute("aria-label", `${statusJobs.length} processes`);
      heading.append(label, count);
      containerEl.appendChild(heading);
    }
    statusJobs.forEach((job) => {
      const card = createCardElement(job, sortMode);
      bindCardInlineEditors(card, job, onGlobalRefresh);
      bindCardDeletion(card, job, onGlobalRefresh);
      const isDragActive = sortMode === "manual"
        ? enableHoldToDrag(card, containerEl, (newOrder) => Promise.resolve(api.reorderJobs(newOrder))
          .then(async () => {
            await onGlobalRefresh?.();
            announceCardReorder(job, newOrder);
          })
          .catch(() => showToast("Could not save card order", "error")))
        : () => false;
      bindCardNavigation(card, job, modalEl, backdropEl, onGlobalRefresh, isDragActive);
      containerEl.appendChild(card);
    });
    if (status) containerEl.appendChild(createQuickAddCard());
  });

  if (!emptyStateContext.groupByStatus) containerEl.appendChild(createQuickAddCard());
}

/** Announces a saved card position change to assistive technology. */
function announceCardReorder(job, newOrder) {
  const announcer = document.querySelector("#card-reorder-announcer");
  const position = newOrder.indexOf(String(job.id));
  if (!announcer || position < 0) return;
  const company = isUnknownCompany(job.company_name) ? "undisclosed company" : job.company_name;
  announcer.textContent = `${job.position_title} at ${company} moved to position ${position + 1} of ${newOrder.length}.`;
}

/**
 * Enables hold-to-drag reordering on a process card.
 *
 * @param {HTMLElement} card - The process card element.
 * @param {HTMLElement} containerEl - The cards grid container.
 * @param {Function} onReorderFinished - Callback when user finishes reordering, receives array of job IDs.
 * @returns {Function} Function returning true if drag or suppression is currently active.
 */
function enableHoldToDrag(card, containerEl, onReorderFinished) {
  const dragHandle = card.querySelector(".btn-card-drag");
  if (dragHandle) dragHandle.style.touchAction = "none";
  const MOUSE_HOLD_DELAY_MS = 180;
  const TOUCH_HOLD_DELAY_MS = 500;
  const HOLD_MOVE_THRESHOLD_PX = 32;

  let holdTimer = null;
  let isDragging = false;
  let suppressClickUntil = 0;
  let placeholder = null;
  let startX = 0;
  let startY = 0;
  let holdStartedAt = 0;
  let activeHoldDelay = MOUSE_HOLD_DELAY_MS;
  let offsetX = 0;
  let offsetY = 0;
  let lockedScrollY = 0;
  let bodyScrollLockStyles = null;
  let previousTouchY = 0;
  let isTouchScrolling = false;
  let activePointerType = "mouse";
  let lastTouchPointerDown = Number.NEGATIVE_INFINITY;
  let initialOrder = [];

  function beginHold(e, holdDelay) {
    startX = e.clientX;
    startY = e.clientY;
    holdStartedAt = Date.now();
    activeHoldDelay = holdDelay;

    const rect = card.getBoundingClientRect();
    offsetX = e.clientX - rect.left;
    offsetY = e.clientY - rect.top;

    card.classList.add("is-holding");
    holdTimer = setTimeout(() => {
      holdTimer = null;
      startDragging(e, rect);
    }, holdDelay);
  }

  function onPointerDown(e) {
    if (e.button !== 0) return;
    const isDragHandle = e.target.closest(".btn-card-drag");
    if (e.target.closest("button, a, input, textarea, .btn-card-delete") && !isDragHandle) return;

    activePointerType = e.pointerType;
    if (e.pointerType === "touch") lastTouchPointerDown = Date.now();
    beginHold(e, e.pointerType === "touch" ? TOUCH_HOLD_DELAY_MS : MOUSE_HOLD_DELAY_MS);

    window.addEventListener("pointermove", onPointerMove);
    window.addEventListener("pointerup", onPointerUp);
    window.addEventListener("pointercancel", onPointerUp);
  }

  function onTouchStart(e) {
    if (Date.now() - lastTouchPointerDown < 100) return;
    if (e.touches.length !== 1 || !e.target.closest(".btn-card-drag")) return;

    const touch = e.touches[0];
    activePointerType = "touch";
    previousTouchY = touch.clientY;
    isTouchScrolling = false;
    beginHold({ clientX: touch.clientX, clientY: touch.clientY }, TOUCH_HOLD_DELAY_MS);
    window.addEventListener("touchmove", onTouchMove, { passive: false });
    window.addEventListener("touchend", onTouchEnd);
    window.addEventListener("touchcancel", onTouchEnd);
  }

  function startDragging(e, rect) {
    isDragging = true;
    card.classList.remove("is-holding");
    card.classList.add("is-dragging");
    document.documentElement.classList.add("is-reordering-cards");
    document.body.classList.add("is-reordering-cards");
    if (activePointerType === "touch") {
      lockedScrollY = window.scrollY;
      bodyScrollLockStyles = {
        position: document.body.style.position,
        top: document.body.style.top,
        left: document.body.style.left,
        right: document.body.style.right,
        width: document.body.style.width,
        overflow: document.body.style.overflow,
      };
      Object.assign(document.body.style, {
        position: "fixed",
        top: `-${lockedScrollY}px`,
        left: "0",
        right: "0",
        width: "100%",
        overflow: "hidden",
      });
    }

    // Capture initial order to detect actual changes
    initialOrder = Array.from(containerEl.querySelectorAll(".process-card"))
      .map((c) => c.getAttribute("data-id"))
      .filter(Boolean);

    // Create placeholder
    placeholder = document.createElement("div");
    placeholder.className = "card-drop-placeholder";
    placeholder.style.width = `${rect.width}px`;
    placeholder.style.height = `${rect.height}px`;

    // Insert placeholder where card was
    card.parentNode.insertBefore(placeholder, card);

    // Position dragged card fixed under cursor
    card.style.position = "fixed";
    card.style.width = `${rect.width}px`;
    card.style.height = `${rect.height}px`;
    card.style.left = `${rect.left}px`;
    card.style.top = `${rect.top}px`;
    card.style.zIndex = "1000";
    card.style.margin = "0";
    card.style.pointerEvents = "none";
  }

  function cancelHoldWhenMoved(e) {
    const distance = Math.hypot(e.clientX - startX, e.clientY - startY);
    if (distance <= HOLD_MOVE_THRESHOLD_PX) return;
    clearTimeout(holdTimer);
    holdTimer = null;
    card.classList.remove("is-holding");
  }

  function moveCardToPointer(e) {
    card.style.left = `${e.clientX - offsetX}px`;
    card.style.top = `${e.clientY - offsetY}px`;
  }

  function movePlaceholderToPoint(e) {
    const elemBelow = document.elementFromPoint(e.clientX, e.clientY);
    if (!elemBelow) return;
    if (placeOverQuickAdd(elemBelow)) return;
    placeRelativeToCard(e, elemBelow);
  }

  function placeOverQuickAdd(elemBelow) {
    const quickAdd = elemBelow.closest(".quick-add-card");
    if (!quickAdd || quickAdd.parentNode !== containerEl) return false;
    containerEl.insertBefore(placeholder, quickAdd);
    return true;
  }

  function placeRelativeToCard(e, elemBelow) {
    const targetCard = elemBelow.closest(".process-card");
    if (!targetCard || targetCard === card || targetCard.parentNode !== containerEl) return;
    const targetRect = targetCard.getBoundingClientRect();
    const targetMidpoint = targetRect.top + targetRect.height / 2;
    const isAfter = e.clientY > targetMidpoint ||
      (Math.abs(e.clientY - targetMidpoint) < 30 && e.clientX > targetRect.left + targetRect.width / 2);
    const insertionPoint = isAfter ? targetCard.nextSibling : targetCard;
    containerEl.insertBefore(placeholder, insertionPoint);
  }

  function movePendingTouch(e) {
    if (holdTimer !== null) {
      const distance = Math.hypot(e.clientX - startX, e.clientY - startY);
      if (Date.now() - holdStartedAt >= activeHoldDelay) {
        clearTimeout(holdTimer);
        holdTimer = null;
        startDragging(e, card.getBoundingClientRect());
      } else if (distance > HOLD_MOVE_THRESHOLD_PX) {
        cancelHoldWhenMoved(e);
        isTouchScrolling = true;
        window.scrollBy(0, startY - e.clientY);
      }
    } else if (isTouchScrolling) {
      window.scrollBy(0, previousTouchY - e.clientY);
    }
    previousTouchY = e.clientY;
  }

  function onPointerMove(e) {
    if (!isDragging) {
      if (activePointerType === "touch") movePendingTouch(e);
      else cancelHoldWhenMoved(e);
      if (!isDragging) return;
    }

    // Drag position
    if (activePointerType === "touch" && e.cancelable) e.preventDefault();
    moveCardToPointer(e);

    movePlaceholderToPoint(e);
  }

  function onTouchMove(e) {
    if (e.touches.length !== 1) return;
    const touch = e.touches[0];
    const point = { clientX: touch.clientX, clientY: touch.clientY };
    if (!isDragging) movePendingTouch(point);
    if (!isDragging) return;

    if (e.cancelable) e.preventDefault();
    moveCardToPointer(point);
    movePlaceholderToPoint(point);
  }

  function onTouchEnd() {
    window.removeEventListener("touchmove", onTouchMove);
    window.removeEventListener("touchend", onTouchEnd);
    window.removeEventListener("touchcancel", onTouchEnd);
    onPointerUp();
  }

  function onPointerUp() {
    clearTimeout(holdTimer);
    card.classList.remove("is-holding");

    window.removeEventListener("pointermove", onPointerMove);
    window.removeEventListener("pointerup", onPointerUp);
    window.removeEventListener("pointercancel", onPointerUp);
    isTouchScrolling = false;

    if (!isDragging) return;

    suppressClickUntil = Date.now() + 350;

    // Reset styles
    card.style.position = "";
    card.style.width = "";
    card.style.height = "";
    card.style.left = "";
    card.style.top = "";
    card.style.zIndex = "";
    card.style.margin = "";
    card.style.pointerEvents = "";
    card.classList.remove("is-dragging");
    document.documentElement.classList.remove("is-reordering-cards");
    document.body.classList.remove("is-reordering-cards");
    if (bodyScrollLockStyles) {
      Object.assign(document.body.style, bodyScrollLockStyles);
      bodyScrollLockStyles = null;
      window.scrollTo(0, lockedScrollY);
    }

    if (placeholder && placeholder.parentNode) {
      placeholder.parentNode.insertBefore(card, placeholder);
      placeholder.remove();
      placeholder = null;
    }

    isDragging = false;

    // Determine new order
    const currentCards = Array.from(containerEl.querySelectorAll(".process-card"));
    const newOrder = currentCards.map((c) => c.getAttribute("data-id")).filter(Boolean);

    const hasChanged =
      newOrder.length !== initialOrder.length ||
      newOrder.some((id, idx) => id !== initialOrder[idx]);

    if (hasChanged && typeof onReorderFinished === "function") {
      onReorderFinished(newOrder);
    }
  }

  card.addEventListener("pointerdown", onPointerDown);
  card.addEventListener("touchstart", onTouchStart, { passive: true });
  dragHandle?.addEventListener("keydown", (event) => {
    const direction = event.key === "ArrowUp" ? -1 : event.key === "ArrowDown" ? 1 : 0;
    if (!direction) return;
    const cards = Array.from(containerEl.querySelectorAll(".process-card"));
    const currentIndex = cards.indexOf(card);
    const nextCard = cards[currentIndex + direction];
    if (!nextCard) return;
    event.preventDefault();
    if (direction < 0) containerEl.insertBefore(card, nextCard);
    else containerEl.insertBefore(card, nextCard.nextSibling);
    const newOrder = Array.from(containerEl.querySelectorAll(".process-card"))
      .map((item) => item.getAttribute("data-id"))
      .filter(Boolean);
    Promise.resolve(onReorderFinished?.(newOrder)).finally(() => {
      const refreshedHandle = Array.from(containerEl.querySelectorAll(".process-card"))
        .find((item) => item.getAttribute("data-id") === card.getAttribute("data-id"))
        ?.querySelector(".btn-card-drag");
      refreshedHandle?.focus();
    });
  });
  card.addEventListener("contextmenu", (event) => {
    if (holdTimer !== null || isDragging) event.preventDefault();
  });

  return () => isDragging || Date.now() < suppressClickUntil;
}
