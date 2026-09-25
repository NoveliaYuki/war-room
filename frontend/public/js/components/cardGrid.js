import { showToast } from "../utils/toast.js";
/**
 * @fileoverview Card Grid View Component.
 * Renders process cards, handles real-time mouse glow tracking, quick deletion,
 * quick addition, and triggers FLIP expansion.
 */

import { api } from "../api.js";
import { renderCompanyAvatar } from "../avatar.js";
import { openWithFlip } from "../flip.js";
import { openDetailModal } from "./detailModal.js";
import { icon } from "../icons.js";
import { makeInlineEditable, parseSalaryInput } from "../inlineEdit.js";
import { escapeHtml, escapeAttr } from "../utils/sanitize.js";
import { formatSalary } from "../utils/salary.js";

const EMPLOYMENT_TYPE_LABELS = new Map([
	["permanent", "Permanent"],
	["b2b", "B2B"],
	["permanent_b2b", "Permanent / B2B"],
]);

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
  return `<span class="referral-tag ${stateClass} editable-card-referral inline-icon-text" data-id="${escapeAttr(job.id)}" title="Double click to toggle Referral status">${icon("userCheck", 11)} ${label}</span>`;
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
  return highlights.length
    ? escapeHtml(highlights.join(" • "))
    : '<span class="card-summary-empty">No role highlights added</span>';
}

/** Creates and fills the static markup for one job card. */
function createCardElement(job) {
  const card = document.createElement("div");
  card.className = "process-card";
  card.setAttribute("data-id", job.id);
  card.addEventListener("mousemove", (event) => updateCardGlow(card, event));
  const avatarHtml = renderCompanyAvatar(job.company_name, job.avatar_seed, 64, job.company_domain, job.id);
  const stageIndicator = getStageIndicator(job);
  const salaryText = formatSalaryShort(job.salary_type, job.salary_min, job.salary_max, job.salary_currency);
  const companyClass = isUnknownCompany(job.company_name) ? "is-unknown" : "";
  const salaryClass = job.salary_type === "unknown" ? "undisclosed" : "";
  card.innerHTML = `<div class="card-content"><div class="card-header"><div class="card-avatar">${avatarHtml}</div><div class="card-title-group">
    <div class="card-company editable-card-company ${companyClass}" data-raw-value="${escapeAttr(job.company_name)}">${renderCardCompany(job.company_name)}</div>
    <h3 class="card-position editable-card-position" data-raw-value="${escapeAttr(job.position_title)}">${escapeHtml(job.position_title)}</h3>
  </div><div class="card-header-actions"><span class="status-pill ${escapeAttr(job.status)}">${escapeHtml(job.status)}</span>
    <button class="btn-card-delete" title="Delete this selection process" data-id="${escapeAttr(job.id)}">${icon("trash", 13)}</button></div></div>
  <div class="card-body"><span class="salary-tag editable-card-salary ${salaryClass}" data-raw-value="${escapeAttr(getCardSalaryRawValue(job))}">${escapeHtml(salaryText)}</span>
    ${renderReferralTag(job)}${renderEmploymentTypeTag(job.employment_type)}${renderWorkArrangementTag(job.work_arrangement)}</div>
  <div class="card-summary"><div class="card-summary-label">Role highlights</div><div class="keyword-note editable-card-keyword" data-raw-value="${escapeAttr(job.keyword_note || "")}" title="Role highlights">${renderCardRoleHighlights(job)}</div></div></div>
  <div class="card-footer">
    <div class="card-stage-indicator"><span style="font-weight: 500; color: #93c5fd; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 220px;" title="${escapeAttr(stageIndicator.text)}">${stageIndicator.markup}</span>
      <button class="btn-card-open-details" type="button" aria-label="Open details for ${escapeAttr(job.position_title)}" title="Open details">${icon("arrowUpRight", 14)}</button></div></div>`;
  return card;
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
  element.classList.add("editable-text");
  element.addEventListener("dblclick", async (event) => {
    try {
      event.preventDefault();
      event.stopPropagation();
      const nextValue = job.is_referral ? 0 : 1;
      await api.updateJob(job.id, { is_referral: nextValue });
      if (typeof window !== "undefined" && typeof window.showToast === "function") {
        window.showToast(nextValue ? "Marked as Referral" : "Marked as Non-referral", "success", 1200);
      }
      onGlobalRefresh?.();
    } catch (error) {
      showToast(error.message, "error");
    }
  });
}

/** Binds deletion to a card's delete button. */
function bindCardDeletion(card, job, onGlobalRefresh) {
  card.querySelector(".btn-card-delete").addEventListener("click", async (event) => {
    try {
      event.stopPropagation();
      if (!confirm(`Delete selection process for "${job.company_name} - ${job.position_title}"?`)) return;
      await api.deleteJob(job.id);
      onGlobalRefresh?.();
    } catch (error) {
      showToast(error.message, "error");
    }
  });
}

/** Opens a job detail modal unless the user is currently dragging the card. */
async function openCardDetails(card, job, modalEl, backdropEl, onGlobalRefresh, isDragActive) {
  if (isDragActive()) return;
  const targetModal = modalEl || document.querySelector("#detail-modal");
  const targetBackdrop = backdropEl || document.querySelector("#modal-backdrop");
  if (targetModal && targetBackdrop) openWithFlip(card, targetModal, targetBackdrop);
  await openDetailModal(targetModal, targetBackdrop, job.id, onGlobalRefresh);
}

/** Returns whether a card click originated from a nested control. */
function isNestedControl(target) {
  return Boolean(target.closest("button, a, input, textarea"));
}

/** Binds card click and double-click behavior for opening details. */
function bindCardNavigation(card, job, modalEl, backdropEl, onGlobalRefresh, isDragActive) {
  let cardClickTimer = null;
  const openDetails = () => openCardDetails(card, job, modalEl, backdropEl, onGlobalRefresh, isDragActive);
  card.querySelector(".btn-card-open-details")?.addEventListener("click", openDetails);
  card.addEventListener("click", (event) => {
    if (isDragActive() || isNestedControl(event.target)) return;
    if (event.target.closest(".editable-text")) {
      if (cardClickTimer) clearTimeout(cardClickTimer);
      cardClickTimer = setTimeout(() => {
        cardClickTimer = null;
        openDetails();
      }, 220);
      return;
    }
    openDetails();
  });
  card.addEventListener("dblclick", () => {
    if (!cardClickTimer) return;
    clearTimeout(cardClickTimer);
    cardClickTimer = null;
  });
}

/**
 * Renders all job selection cards into the grid container.
 *
 * @param {HTMLElement} containerEl - The DOM container for the grid.
 * @param {Array} jobs - Array of job summary records.
 * @param {HTMLElement} modalEl - The modal container element.
 * @param {HTMLElement} backdropEl - The modal backdrop element.
 * @param {Function} onGlobalRefresh - Global refresh callback.
 */
export function renderCardGrid(containerEl, jobs = [], modalEl = null, backdropEl = null, onGlobalRefresh = null) {
  containerEl.innerHTML = "";

  if (jobs.length === 0) {
    const empty = document.createElement("div");
    empty.className = "empty-state";
    empty.innerHTML = `
      <div class="empty-icon">${icon("briefcase", 36)}</div>
      <h3 style="margin-bottom: 8px;">No selection processes found</h3>
      <p style="color: var(--text-secondary); max-width: 400px; margin: 0 auto 16px;">
        Track your interviews, recruiters, salary negotiations, and interview questions in one place.
      </p>
      <button class="btn-primary btn-create-first-process" type="button">
        <span class="inline-icon-text">${icon("plus", 14)} Create First Selection Process</span>
      </button>
    `;
    empty.querySelector(".btn-create-first-process")?.addEventListener("click", () => {
      document.getElementById("btn-new-process")?.click();
    });
    containerEl.appendChild(empty);
    return;
  }

  // 1. Render all active selection cards
  jobs.forEach((job) => {
    const card = createCardElement(job);
    bindCardInlineEditors(card, job, onGlobalRefresh);
    bindCardDeletion(card, job, onGlobalRefresh);
    const isDragActive = enableHoldToDrag(card, containerEl, (newOrder) => api.reorderJobs(newOrder));
    bindCardNavigation(card, job, modalEl, backdropEl, onGlobalRefresh, isDragActive);
    containerEl.appendChild(card);
  });

  // 2. Add the "+ Add Selection Process" quick action card in the preview grid
  const addCard = document.createElement("div");
  addCard.className = "quick-add-card";
  addCard.innerHTML = `
    <div class="quick-add-icon">${icon("plus", 22)}</div>
    <div class="quick-add-title">Add Selection Process</div>
    <div class="quick-add-subtitle">Track a new job opportunity or recruiter reachout</div>
  `;
  addCard.addEventListener("click", () => {
    document.querySelector("#btn-new-process")?.click();
  });
  containerEl.appendChild(addCard);
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
  let holdTimer = null;
  let isDragging = false;
  let suppressClickUntil = 0;
  let placeholder = null;
  let startX = 0;
  let startY = 0;
  let offsetX = 0;
  let offsetY = 0;
  let initialOrder = [];

  const HOLD_DELAY_MS = 180;

  function onPointerDown(e) {
    if (e.button !== 0) return;
    if (e.target.closest("button, a, input, textarea, .btn-card-delete")) return;

    startX = e.clientX;
    startY = e.clientY;

    const rect = card.getBoundingClientRect();
    offsetX = e.clientX - rect.left;
    offsetY = e.clientY - rect.top;

    card.classList.add("is-holding");

    holdTimer = setTimeout(() => {
      startDragging(e, rect);
    }, HOLD_DELAY_MS);

    window.addEventListener("pointermove", onPointerMove);
    window.addEventListener("pointerup", onPointerUp);
    window.addEventListener("pointercancel", onPointerUp);
  }

  function startDragging(e, rect) {
    isDragging = true;
    card.classList.remove("is-holding");
    card.classList.add("is-dragging");

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
    if (distance <= 8) return;
    clearTimeout(holdTimer);
    card.classList.remove("is-holding");
  }

  function moveCardToPointer(e) {
    card.style.left = `${e.clientX - offsetX}px`;
    card.style.top = `${e.clientY - offsetY}px`;
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

  function onPointerMove(e) {
    if (!isDragging) {
      cancelHoldWhenMoved(e);
      return;
    }

    // Drag position
    moveCardToPointer(e);

    // Drop target detection
    const elemBelow = document.elementFromPoint(e.clientX, e.clientY);
    if (!elemBelow) return;

    if (placeOverQuickAdd(elemBelow)) return;
    placeRelativeToCard(e, elemBelow);
  }

  function onPointerUp() {
    clearTimeout(holdTimer);
    card.classList.remove("is-holding");

    window.removeEventListener("pointermove", onPointerMove);
    window.removeEventListener("pointerup", onPointerUp);
    window.removeEventListener("pointercancel", onPointerUp);

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

  return () => isDragging || Date.now() < suppressClickUntil;
}
