/**
 * @fileoverview Main Client Application Entry Point.
 * Coordinates global state, filter tabs, instant search, and keyboard shortcuts.
 */

import { api } from "./api.js";
import { renderCardGrid } from "./components/cardGrid.js";
import { renderScheduleView } from "./components/scheduleView.js";
import { openCvLibrary } from "./components/cvLibrary.js";
import { openTechnologyLibrary } from "./components/technologyLibrary.js";
import { closeWithFlip, cancelPendingFlipClose } from "./flip.js";
import { icon } from "./icons.js";
import { activateModal, restoreModalFocus, trapModalTab } from "./modalA11y.js";
import { showToast } from "./utils/toast.js";
import { escapeHtml } from "./utils/sanitize.js";
import { filterJobs } from "./utils/processFilters.js";
import { readSavedSortMode, sortJobsForDisplay } from "./utils/processOrdering.js";

if (typeof window !== "undefined") {
  window.showToast = showToast;
}

/** Shows the generated fallback when a company logo request fails. */
function handleAvatarImageError(event) {
  const image = event.target;
  if (!(image instanceof HTMLImageElement) || !image.hasAttribute("data-avatar-fallback")) return;
  image.style.display = "none";
  const fallback = image.nextElementSibling;
  if (fallback) fallback.style.display = "block";
}

document.addEventListener("error", handleAvatarImageError, true);

const FILTER_STORAGE_KEY = "war-room.active-filter";
const PROCESS_FILTER_STORAGE_KEY = "war-room.process.filters.v1";
const THEME_STORAGE_KEY = "war-room.theme";
const validFilters = new Set(["waiting", "ongoing", "accepted", "rejected", "all", "schedule"]);

/** Restores a saved filter when it is a supported page. */
function getSavedFilter() {
  try {
    const savedFilter = window.localStorage.getItem(FILTER_STORAGE_KEY);
    return validFilters.has(savedFilter) ? savedFilter : "ongoing";
  } catch {
    return "ongoing";
  }
}

let currentFilter = getSavedFilter();
let currentStatusFilter = currentFilter === "schedule" ? "ongoing" : currentFilter;
let currentSearch = "";
let searchDebounceTimer = null;
let processFilters = loadProcessFilters();
let availableTechnologies = [];
let technologiesLoaded = false;

/** Infers the salary type from entered range bounds. */
function resolveSalaryType(minimum, maximum) {
  if (minimum && maximum) return "limited";
  if (minimum) return "no_max";
  if (maximum) return "no_min";
  return null;
}

const cardGridEl = document.querySelector("#cards-grid");
const modalEl = document.querySelector("#detail-modal");
const backdropEl = document.querySelector("#modal-backdrop");
const searchInput = document.querySelector("#search-input");
const searchFocusButton = document.querySelector(".search-focus");
const filterTabs = document.querySelectorAll(".filter-tab");
const scheduleButton = document.querySelector("#tab-schedule");
const filterMenu = document.querySelector("#filter-menu");
const filterMenuTrigger = document.querySelector("#filter-menu-trigger");
const filterCurrentAction = document.querySelector("#filter-current-action");
const moreActionsMenu = document.querySelector("#toolbar-more-options");
const moreActionsTrigger = document.querySelector("#toolbar-more-trigger");
const newProcessButtons = document.querySelectorAll(".new-process-trigger");
const menuToggle = document.querySelector("#btn-menu-toggle");
const headerControls = document.querySelector("#header-controls");
const dataManagementButton = document.querySelector("#btn-data-management");
const cvLibraryButton = document.querySelector("#btn-cv-library");
const technologyLibraryButton = document.querySelector("#btn-technology-library");
const themeToggle = document.querySelector("#btn-theme-toggle");
const processFilterTrigger = document.querySelector("#process-filter-trigger");
const processFilterPanel = document.querySelector("#process-filter-panel");
const processFilterArrangements = document.querySelectorAll('input[name="process-filter-arrangement"]');
const processFilterPostedSalaryMin = document.querySelector("#process-filter-posted-salary-min");
const processFilterPostedSalaryMax = document.querySelector("#process-filter-posted-salary-max");
const processFilterExpectedSalary = document.querySelector("#process-filter-expected-salary");
const processFilterCurrency = document.querySelector("#process-filter-currency");
const processFilterReferral = document.querySelector("#process-filter-referral");
const processFilterNoReferral = document.querySelector("#process-filter-no-referral");
const processFilterTechnologySearch = document.querySelector("#process-filter-technology-search");
const processFilterTechnologyOptions = document.querySelector("#process-filter-technology-options");
const processFilterClear = document.querySelector("#process-filter-clear");
const processFilterChips = document.querySelector("#process-filter-chips");
const processSortTrigger = document.querySelector("#process-sort-trigger");
const processSortCurrent = document.querySelector("#process-sort-current");
const processSortMenu = document.querySelector("#process-sort-menu");
const processSortOptions = document.querySelectorAll("[data-sort-mode]");
const processFilterAnyReferral = document.querySelector("#process-filter-any-referral");

/** Returns the selected theme, falling back to the current system preference. */
function getActiveTheme() {
  const selected = document.documentElement.dataset.theme;
  if (selected === "dark" || selected === "light") return selected;
  return window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
}

/** Updates the theme toggle's accessible label for the next action. */
function updateThemeToggle() {
  if (!themeToggle) return;
  const nextTheme = getActiveTheme() === "dark" ? "light" : "dark";
  const label = `Switch to ${nextTheme} theme`;
  themeToggle.setAttribute("aria-label", label);
  themeToggle.title = label;
}

themeToggle?.addEventListener("click", () => {
  const nextTheme = getActiveTheme() === "dark" ? "light" : "dark";
  const root = document.documentElement;
  root.classList.add("theme-switching");
  root.dataset.theme = nextTheme;
  const scheduleFrame = window.requestAnimationFrame?.bind(window) ?? ((callback) => window.setTimeout(callback, 0));
  scheduleFrame(() => scheduleFrame(() => root.classList.remove("theme-switching")));
  try {
    window.localStorage.setItem(THEME_STORAGE_KEY, nextTheme);
  } catch {
    // Keep the selected theme active for this page when browser storage is blocked.
  }
  updateThemeToggle();
});

window.matchMedia?.("(prefers-color-scheme: light)").addEventListener?.("change", updateThemeToggle);
updateThemeToggle();

/** Closes the compact navigation menu after choosing an action. */
function closeMobileMenu() {
  if (!menuToggle || !headerControls) return;
  menuToggle.setAttribute("aria-expanded", "false");
  menuToggle.setAttribute("aria-label", "Open navigation menu");
  headerControls.classList.remove("is-open");
}

/** Closes the process view menu and restores focus when a choice hides. */
function closeFilterMenu(restoreFocus = false) {
  if (!filterMenu || filterMenu.hidden) return;
  const focusWasInMenu = filterMenu.contains(document.activeElement);
  filterMenu.hidden = true;
  filterMenuTrigger?.setAttribute("aria-expanded", "false");
  if (restoreFocus || focusWasInMenu) filterMenuTrigger?.focus();
}

/** Closes the low-frequency actions menu and restores focus if an item hides. */
function closeMoreActionsMenu(restoreFocus = false) {
  if (!moreActionsMenu || moreActionsMenu.hidden) return;
  const focusWasInMenu = moreActionsMenu.contains(document.activeElement);
  moreActionsMenu.hidden = true;
  moreActionsTrigger?.setAttribute("aria-expanded", "false");
  if (restoreFocus || focusWasInMenu) moreActionsTrigger?.focus();
}

/** Updates the compact view control with the active view and its current count. */
function updateFilterSummary() {
  const active = [...filterTabs].find((tab) => tab.getAttribute("data-filter") === currentStatusFilter);
  const label = active?.querySelector(".filter-label")?.textContent?.trim();
  const count = active?.querySelector(".tab-count")?.textContent?.trim();
  const labelTarget = document.querySelector("#filter-current-label");
  const countTarget = document.querySelector("#filter-current-count");
  if (labelTarget && label) labelTarget.textContent = label;
  if (countTarget && count !== undefined) countTarget.textContent = count;
  if (filterCurrentAction && label) filterCurrentAction.setAttribute("aria-label", `Show ${label.toLowerCase()} processes`);
}

/**
 * Updates filter counters and re-renders cards.
 */
async function refreshApp() {
  try {
    const counts = await api.getJobCounts();
    document.querySelector("#count-all").textContent = counts.all;
    document.querySelector("#count-waiting").textContent = counts.waiting;
    document.querySelector("#count-ongoing").textContent = counts.ongoing;
    document.querySelector("#count-accepted").textContent = counts.accepted;
    document.querySelector("#count-rejected").textContent = counts.rejected;

    const meetings = await api.getMeetings();
    const countMeetingsEl = document.querySelector("#count-meetings");
    if (countMeetingsEl) {
      countMeetingsEl.textContent = meetings.length;
    }
    updateFilterSummary();
    updateSortControl();
    await loadAvailableTechnologies();

    if (currentFilter === "schedule") {
      cardGridEl.classList.add("schedule-mode");
      await renderScheduleView(cardGridEl, modalEl, backdropEl, refreshApp);
      return;
    }

    cardGridEl.classList.remove("schedule-mode");
    const jobs = await api.getJobs(currentFilter, currentSearch);
    const filteredJobs = filterJobs(jobs, processFilters);
    const sortMode = currentFilter === "all" ? "manual" : readSavedSortMode(window.localStorage, currentFilter);
    renderCardGrid(cardGridEl, sortJobsForDisplay(filteredJobs, currentFilter, sortMode), modalEl, backdropEl, refreshApp, sortMode, {
      filter: currentFilter,
      totalCount: counts.all,
      statusCount: counts[currentFilter] ?? counts.all,
    });
  } catch (err) {
    console.error("Failed to load jobs data:", err);
  }
}

/** Restores validated card filter choices from browser storage. */
function loadProcessFilters() {
  try {
    const saved = JSON.parse(window.localStorage.getItem(PROCESS_FILTER_STORAGE_KEY) || "{}");
    return {
      arrangements: readSavedArrangements(saved),
      expectedSalaryQuery: typeof saved.expectedSalaryQuery === "string" ? saved.expectedSalaryQuery : "",
      postedSalaryMin: parseFilterNumber(saved.postedSalaryMin ?? saved.salaryMin),
      postedSalaryMax: parseFilterNumber(saved.postedSalaryMax ?? saved.salaryMax),
      currency: readSavedCurrency(saved.currency),
      referral: readSavedReferral(saved.referral),
      technologies: readSavedTechnologies(saved.technologies),
    };
  } catch {
    return { arrangements: [], expectedSalaryQuery: "", postedSalaryMin: null, postedSalaryMax: null, currency: "", referral: "", technologies: [] };
  }
}

function readSavedArrangements(saved) {
  const accepted = ["remote", "hybrid", "on_site"];
  if (Array.isArray(saved.arrangements)) return saved.arrangements.filter((item) => accepted.includes(item));
  return accepted.includes(saved.arrangement) ? [saved.arrangement] : [];
}

function readSavedCurrency(currency) {
  return ["EUR", "USD", "GBP"].includes(currency) ? currency : "";
}

function readSavedReferral(referral) {
  return ["yes", "no"].includes(referral) ? referral : "";
}

function readSavedTechnologies(technologies) {
  return Array.isArray(technologies) ? technologies.filter((id) => typeof id === "string") : [];
}

function parseFilterNumber(value) {
  return value !== null && value !== "" && Number.isFinite(Number(value)) ? Number(value) : null;
}

async function loadAvailableTechnologies() {
  if (!processFilterTechnologyOptions || technologiesLoaded) return;
  try {
    availableTechnologies = await api.getTechnologies();
    technologiesLoaded = true;
    renderTechnologyFilterOptions();
    updateProcessFilterSummary();
  } catch (error) {
    console.error("Failed to load technologies for filters:", error);
  }
}

function renderTechnologyFilterOptions() {
  if (!processFilterTechnologyOptions) return;
  const query = (processFilterTechnologySearch?.value || "").trim().toLocaleLowerCase();
  const technologies = availableTechnologies.filter((technology) => technology.name.toLocaleLowerCase().includes(query));
  processFilterTechnologyOptions.innerHTML = technologies.map((technology) => `
    <label><input type="checkbox" name="process-filter-technology" value="${escapeHtml(technology.id)}" ${processFilters.technologies.includes(technology.id) ? "checked" : ""} />${escapeHtml(technology.name)}</label>
  `).join("");
}

/** Updates the compact filter count and active filter summary. */
function updateProcessFilterSummary() {
  const count = Number(processFilters.arrangements.length > 0)
    + Number(Boolean(processFilters.expectedSalaryQuery))
    + Number(processFilters.postedSalaryMin !== null || processFilters.postedSalaryMax !== null)
    + Number(Boolean(processFilters.currency))
    + Number(Boolean(processFilters.referral))
    + Number(processFilters.technologies.length > 0);
  const badge = document.querySelector("#process-filter-count");
  const summary = document.querySelector("#process-filter-summary");
  if (badge) {
    badge.textContent = String(count);
    badge.hidden = count === 0;
  }
  const chips = getProcessFilterChips();
  if (processFilterChips) {
    processFilterChips.innerHTML = chips.map(({ category, label, value = "" }) => `
      <button type="button" class="process-filter-chip" data-remove-filter="${category}" ${value ? `data-filter-value="${escapeHtml(value)}"` : ""} aria-label="Remove ${escapeHtml(label)} filter">
        <span>${escapeHtml(label)}</span><svg aria-hidden="true" width="11" height="11" viewBox="0 0 12 12" fill="none"><path d="m3 3 6 6m0-6L3 9" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>
      </button>
    `).join("");
  }
  if (processFilterClear) processFilterClear.hidden = chips.length === 0;
  if (summary) summary.textContent = "";
}

function getProcessFilterChips() {
  const chips = processFilters.arrangements.map((value) => ({ category: "arrangement", value, label: ({ remote: "Remote", hybrid: "Hybrid", on_site: "On-site" })[value] || value }));
  if (processFilters.referral) chips.push({ category: "referral", label: processFilters.referral === "yes" ? "Referral" : "No referral" });
  if (processFilters.expectedSalaryQuery) chips.push({ category: "expectedSalary", label: `Target: ${processFilters.expectedSalaryQuery}` });
  if (processFilters.postedSalaryMin !== null || processFilters.postedSalaryMax !== null || processFilters.currency) {
    const lower = processFilters.postedSalaryMin === null ? "Any" : processFilters.postedSalaryMin.toLocaleString();
    const upper = processFilters.postedSalaryMax === null ? "Any" : processFilters.postedSalaryMax.toLocaleString();
    chips.push({ category: "postingSalary", label: `Posting ${lower}–${upper}${processFilters.currency ? ` ${processFilters.currency}` : ""}` });
  }
  processFilters.technologies.forEach((id) => {
    const name = availableTechnologies.find((item) => item.id === id)?.name;
    if (name) chips.push({ category: "technology", value: id, label: name });
  });
  return chips;
}

function initializeProcessFilterControls() {
  processFilterArrangements.forEach((control) => { control.checked = processFilters.arrangements.includes(control.value); });
  if (processFilterAnyReferral) processFilterAnyReferral.checked = !processFilters.referral;
  if (processFilterReferral) processFilterReferral.checked = processFilters.referral === "yes";
  if (processFilterNoReferral) processFilterNoReferral.checked = processFilters.referral === "no";
  if (processFilterExpectedSalary) processFilterExpectedSalary.value = processFilters.expectedSalaryQuery;
  if (processFilterPostedSalaryMin) processFilterPostedSalaryMin.value = processFilters.postedSalaryMin ?? "";
  if (processFilterPostedSalaryMax) processFilterPostedSalaryMax.value = processFilters.postedSalaryMax ?? "";
  if (processFilterCurrency) processFilterCurrency.value = processFilters.currency;
  updateProcessFilterSummary();
}

function saveProcessFilters() {
  const visibleTechnologyControls = [...(processFilterTechnologyOptions?.querySelectorAll('input[name="process-filter-technology"]') || [])];
  const visibleTechnologyIds = visibleTechnologyControls.map((control) => control.value);
  const hiddenSelectedTechnologies = processFilters.technologies.filter((id) => !visibleTechnologyIds.includes(id));
  processFilters = {
    arrangements: [...processFilterArrangements].filter((control) => control.checked).map((control) => control.value),
    expectedSalaryQuery: processFilterExpectedSalary?.value.trim() || "",
    postedSalaryMin: parseFilterNumber(processFilterPostedSalaryMin?.value),
    postedSalaryMax: parseFilterNumber(processFilterPostedSalaryMax?.value),
    currency: processFilterCurrency?.value || "",
    referral: processFilterReferral?.checked ? "yes" : processFilterNoReferral?.checked ? "no" : "",
    technologies: [...hiddenSelectedTechnologies, ...visibleTechnologyControls.filter((control) => control.checked).map((control) => control.value)],
  };
  try { window.localStorage.setItem(PROCESS_FILTER_STORAGE_KEY, JSON.stringify(processFilters)); } catch { /* Filters remain active until this page closes. */ }
  updateProcessFilterSummary();
  void refreshApp();
}

/** Updates the visible order selector and explains the active status order. */
function updateSortControl() {
  const row = document.querySelector("#process-toolbar");
  if (!row || !processSortTrigger || !processSortCurrent) return;
  const isStatusTab = ["waiting", "ongoing", "accepted", "rejected"].includes(currentFilter);
  row.hidden = !isStatusTab;
  if (!isStatusTab) return;
  const mode = readSavedSortMode(window.localStorage, currentFilter);
  const labels = { "added-newest": "Added · Newest", "added-oldest": "Added · Oldest", "status-newest": "Status · Newest", "status-oldest": "Status · Oldest", advanced: "Advanced", early: "Least advanced", manual: "Manual" };
  const fullLabels = { "added-newest": "newest added first", "added-oldest": "oldest added first", "status-newest": "most recently changed first", "status-oldest": "least recently changed first", advanced: "most advanced stage first", early: "least advanced stage first", manual: "manual order" };
  processSortCurrent.textContent = labels[mode];
  processSortTrigger.setAttribute("aria-label", `Sort processes: ${fullLabels[mode]}`);
  processSortOptions.forEach((option) => option.setAttribute("aria-checked", String(option.dataset.sortMode === mode)));
}

function closeSortMenu() {
  if (!processSortMenu || !processSortTrigger) return;
  processSortMenu.hidden = true;
  processSortTrigger.setAttribute("aria-expanded", "false");
}

processSortTrigger?.addEventListener("click", () => {
  if (!processSortMenu) return;
  const willOpen = processSortMenu.hidden;
  processSortMenu.hidden = !willOpen;
  processSortTrigger.setAttribute("aria-expanded", String(willOpen));
});

processSortMenu?.addEventListener("click", (event) => {
  const option = event.target.closest("[data-sort-mode]");
  if (!option) return;
  try {
    window.localStorage.setItem(`war-room.process.sort.${currentFilter}`, option.dataset.sortMode);
  } catch {
    // Sorting remains available for the current render when browser storage is unavailable.
  }
  closeSortMenu();
  void refreshApp();
});

processSortMenu?.addEventListener("keydown", (event) => {
  if (event.key !== "Escape") return;
  closeSortMenu();
  processSortTrigger?.focus();
});

/** Derives a salary type from submitted bounds or keeps the selected type. */
function resolveSubmittedSalaryType(data, minValue, maxValue) {
  if (minValue && maxValue) return "limited";
  if (minValue) return "no_max";
  if (maxValue) return "no_min";
  return data.get("salary_type");
}

/** Converts the creation form into the backend job payload. */
function buildJobPayload(form) {
  const data = new FormData(form);
  const minValue = data.get("salary_min");
  const maxValue = data.get("salary_max");
  const salaryMin = minValue ? parseInt(minValue, 10) : null;
  const salaryMax = maxValue ? parseInt(maxValue, 10) : null;
  return {
    company_name: data.get("company_name") || "Unknown",
    position_title: data.get("position_title"),
    status: "ongoing",
    salary_type: resolveSubmittedSalaryType(data, salaryMin, salaryMax),
    salary_min: salaryMin,
    salary_max: salaryMax,
    salary_currency: "EUR",
    recruiter_type: data.get("recruiter_type"),
    recruiter_name: data.get("recruiter_name"),
    recruiter_agency: data.get("recruiter_agency"),
    job_post_url: data.get("job_post_url"),
    company_domain: data.get("company_domain") || null,
    keyword_note: data.get("keyword_note"),
    company_overview: data.get("company_overview"),
    reasons_to_change: data.get("reasons_to_change"),
    experience_notes: data.get("experience_notes"),
    expected_salary: data.get("expected_salary"),
    interview_notes: data.get("interview_notes"),
    is_referral: data.get("is_referral") === "on",
    create_default_stages: data.get("create_default_stages") === "on",
  };
}

/** Persists a submitted job form and reports the result to the user. */
async function submitNewJobForm(event, form, closeForm) {
  try {
    event.preventDefault();
    await api.createJob(buildJobPayload(form));
    showToast("Selection process created successfully!", "success");
    closeForm();
    refreshApp();
  } catch (err) {
    showToast(err.message, "error");
  }
}

/**
 * Sets active filter tab.
 */
function setFilter(filter) {
  if (!validFilters.has(filter)) return;
  currentFilter = filter;
  if (filter !== "schedule") currentStatusFilter = filter;
  try {
    window.localStorage.setItem(FILTER_STORAGE_KEY, filter);
  } catch {
    // Keep navigation working when browser storage is unavailable.
  }
  filterTabs.forEach((tab) => {
    const isActive = tab.getAttribute("data-filter") === currentStatusFilter;
    tab.classList.toggle("active", isActive);
    tab.setAttribute("aria-checked", String(isActive));
  });
  scheduleButton?.classList.toggle("active", filter === "schedule");
  scheduleButton?.setAttribute("aria-pressed", String(filter === "schedule"));
  updateFilterSummary();
  closeFilterMenu(true);
  refreshApp();
}

/** Returns whether keyboard focus is currently in an editable control. */
function isTypingTarget(target = document.activeElement) {
  return ["INPUT", "TEXTAREA", "SELECT"].includes(target?.tagName) || Boolean(target?.isContentEditable);
}

/** Returns whether an event requests the new-process form. */
function requestsNewProcess(event, isTyping) {
  return !isTyping && !event.metaKey && !event.ctrlKey && !event.altKey && event.key === "n";
}

/** Returns whether an event requests search focus. */
function requestsSearch(event, isTyping) {
  const searchSlash = event.key === "/" && !isTyping;
  const searchShortcut = (event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k";
  return searchSlash || searchShortcut;
}

/** Maps a numeric shortcut to a filter, if one is available. */
function filterFromShortcut(event, isTyping) {
  if (isTyping || event.metaKey || event.ctrlKey || event.altKey) return null;
  const filters = { "1": "waiting", "2": "ongoing", "3": "accepted", "4": "rejected", "5": "all", "6": "schedule" };
  return filters[event.key] || null;
}

/** Closes the modal and clears its content. */
function closeModal() {
  closeWithFlip(modalEl, backdropEl, () => {
    modalEl.innerHTML = "";
    restoreModalFocus();
  });
}

/** Closes the data transfer dialog and returns focus to its trigger. */
function closeManagementModal(focusTarget) {
  backdropEl.classList.remove("active");
  modalEl.classList.remove("data-transfer-modal");
  modalEl.innerHTML = "";
  resetModalAnimation();
  restoreModalFocus();
  const compact = window.matchMedia("(max-width: 800px)").matches;
  (compact ? menuToggle : focusTarget)?.focus();
}

/** Closes the data transfer dialog and returns focus to its trigger. */
function closeDataModal() {
  closeManagementModal(moreActionsTrigger);
}

/** Closes the CV library and returns focus to its trigger. */
function closeCvLibraryModal() {
  closeManagementModal(moreActionsTrigger);
}

/** Closes the technology catalog and returns focus to its trigger. */
function closeTechnologyLibraryModal() {
  closeManagementModal(moreActionsTrigger);
}

/** Closes whichever data or CV view is currently in the shared dialog. */
function closeTransferModal() {
  if (modalEl.querySelector("#btn-close-cv-library")) closeCvLibraryModal();
  else if (modalEl.querySelector("#btn-close-technology-library")) closeTechnologyLibraryModal();
  else closeDataModal();
}

/** Clears FLIP styles before reusing the modal while a close animation is pending. */
function resetModalAnimation() {
  modalEl.classList.remove("modal-animating");
  modalEl.style.transition = "";
  modalEl.style.transform = "";
  modalEl.style.transformOrigin = "";
  modalEl.style.borderRadius = "";
  modalEl.style.opacity = "";
}

/** Opens the data transfer dialog. */
function openDataModal() {
  closeMoreActionsMenu();
  closeMobileMenu();
  cancelPendingFlipClose(true);
  resetModalAnimation();
  modalEl.classList.add("data-transfer-modal");
  modalEl.innerHTML = `
    <div class="modal-header data-transfer-header">
      <div><div class="modal-title">Data &amp; Backups</div><div class="modal-company">Move your War Room data between machines</div></div>
      <button class="btn-secondary" id="btn-close-data-modal" type="button" aria-label="Close">${icon("close", 18)}</button>
    </div>
    <div class="data-transfer-content">
      <section class="data-transfer-section">
        <h3>Export backup</h3>
        <p>Download your processes, technology catalog, company logos, and uploaded files in one portable ZIP archive.</p>
        <button class="btn-primary" id="btn-export-backup" type="button">${icon("download", 15)} Export ZIP backup</button>
      </section>
      <section class="data-transfer-section">
        <h3>Import backup</h3>
        <p>Choose a War Room ZIP backup created with Export backup.</p>
        <label class="data-transfer-file-label" for="backup-import-file">Backup ZIP file</label>
        <div class="data-transfer-file-picker">
          <input id="backup-import-file" class="data-transfer-file-input" type="file" accept=".zip,application/zip" aria-label="Choose backup file" />
          <button id="backup-import-choose" class="data-transfer-choose" type="button">Choose file</button>
          <span id="backup-import-file-name" class="data-transfer-file-name">No file selected</span>
        </div>
        <div class="data-transfer-warning">Import replaces all processes, the technology catalog, company logos, and uploaded files currently saved in this War Room.</div>
        <label class="data-transfer-confirm"><input id="backup-import-confirm" type="checkbox" /> I understand this replaces the current data</label>
        <button class="btn-danger" id="btn-import-backup" type="button" disabled>Import and replace data</button>
        <div class="data-transfer-status" id="backup-import-status" aria-live="polite"></div>
      </section>
    </div>`;
  backdropEl.classList.add("active");
  activateModal(modalEl, "#btn-close-data-modal");
  modalEl.querySelector("#btn-close-data-modal").addEventListener("click", closeDataModal);
  modalEl.querySelector("#btn-export-backup").addEventListener("click", downloadBackup);
  const fileInput = modalEl.querySelector("#backup-import-file");
  const confirmation = modalEl.querySelector("#backup-import-confirm");
  const importButton = modalEl.querySelector("#btn-import-backup");
  const updateImportButton = () => { importButton.disabled = !fileInput.files.length || !confirmation.checked; };
  modalEl.querySelector("#backup-import-choose").addEventListener("click", () => fileInput.click());
  fileInput.addEventListener("change", () => {
    modalEl.querySelector("#backup-import-file-name").textContent = fileInput.files[0]?.name || "No file selected";
    updateImportButton();
  });
  confirmation.addEventListener("change", updateImportButton);
  importButton.addEventListener("click", () => importBackup(fileInput.files[0]));
}

/** Opens CV version management directly from the main toolbar. */
function openCvLibraryModal() {
  closeMoreActionsMenu();
  closeMobileMenu();
  cancelPendingFlipClose(true);
  resetModalAnimation();
  modalEl.classList.add("data-transfer-modal");
  backdropEl.classList.add("active");
  openCvLibrary(modalEl, closeCvLibraryModal);
}

/** Opens shared technology catalog management from the secondary actions menu. */
function openTechnologyLibraryModal() {
  closeMoreActionsMenu();
  closeMobileMenu();
  cancelPendingFlipClose(true);
  resetModalAnimation();
  modalEl.classList.add("data-transfer-modal");
  backdropEl.classList.add("active");
  openTechnologyLibrary(modalEl, closeTechnologyLibraryModal);
}

/** Downloads the generated ZIP archive. */
async function downloadBackup(event) {
  const button = event.currentTarget;
  button.disabled = true;
  try {
    const blob = await api.exportBackup();
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = `war-room-backup-${new Date().toISOString().slice(0, 10)}.zip`;
    document.body.append(anchor);
    anchor.click();
    anchor.remove();
    window.setTimeout(() => URL.revokeObjectURL(url), 1000);
    showToast("Backup downloaded", "success");
  } catch (err) {
    showToast(err.message || "Failed to export backup", "error");
  } finally {
    button.disabled = false;
  }
}

/** Replaces the local process data with the selected validated backup. */
async function importBackup(file, allowEmpty = false) {
  const button = modalEl.querySelector("#btn-import-backup");
  const status = modalEl.querySelector("#backup-import-status");
  button.disabled = true;
  status.textContent = "Validating and importing backup…";
  try {
    await api.importBackup(file, allowEmpty);
    closeDataModal();
    showToast("Backup imported successfully", "success");
    await refreshApp();
  } catch (err) {
    if (err.requiresEmptyConfirmation) {
      button.disabled = false;
      status.textContent = "";
      const confirmed = window.confirm("This ZIP backup contains no saved processes. Importing it will replace the current data with an empty workspace. Are you sure you want to continue?");
      if (confirmed) await importBackup(file, true);
      return;
    }
    status.textContent = err.message || "Failed to import backup";
    button.disabled = false;
  }
}

/** Closes the active modal or view menu in response to Escape. */
function handleEscape(event) {
  if (event.key !== "Escape") return false;
  event.preventDefault();
  if (backdropEl.classList.contains("active")) {
    if (modalEl.classList.contains("data-transfer-modal")) closeTransferModal();
    else closeModal();
    return true;
  }
  if (filterMenu && !filterMenu.hidden) {
    closeFilterMenu(true);
    return true;
  }
  if (moreActionsMenu && !moreActionsMenu.hidden) {
    closeMoreActionsMenu(true);
    return true;
  }
  return false;
}

/** Applies one global keyboard shortcut to the application. */
function handleGlobalKeydown(event) {
  if (backdropEl.classList.contains("active") && trapModalTab(event, modalEl)) return;
  const typing = isTypingTarget();
  if (handleEscape(event)) return;
  if (backdropEl.classList.contains("active")) return;
  if (requestsNewProcess(event, typing)) {
    event.preventDefault();
    openNewProcessModal();
    return;
  }
  if (requestsSearch(event, typing)) {
    event.preventDefault();
    searchInput.focus();
    searchInput.select();
    return;
  }
  const filter = filterFromShortcut(event, typing);
  if (filter) setFilter(filter);
}

/**
 * Opens modal with the New Selection Process creation form.
 */
function openNewProcessModal() {
  cancelPendingFlipClose(true);
  resetModalAnimation();
  backdropEl.classList.add("active");
  modalEl.innerHTML = `
    <div class="modal-header">
      <h2 class="modal-title inline-icon-text">${icon("plus", 16)} Add New Selection Process</h2>
      <button class="modal-close-btn">${icon("close", 14)}</button>
    </div>
    <form id="new-process-form" class="process-form">
      <div class="form-grid form-grid-two">
        <div>
          <label class="meta-label">Company Name</label>
          <input type="text" name="company_name" placeholder="Company Name (Leave blank if Unknown)" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
          <span style="font-size: 11px; color: var(--text-muted);">Defaults to 'Unknown' if external recruiter hasn't disclosed client</span>
        </div>
        <div>
          <label class="meta-label">Position Title *</label>
          <input type="text" name="position_title" placeholder="e.g. Senior AI Systems Engineer" required style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        </div>
      </div>

      <div class="form-grid form-grid-three">
        <div>
          <label class="meta-label">Salary Type</label>
          <select name="salary_type" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);">
            <option value="limited">Limited Range (Min - Max)</option>
            <option value="no_min">Up to Max (No Minimum)</option>
            <option value="no_max">From Min (No Maximum)</option>
            <option value="unknown" selected>Unknown / Undisclosed</option>
          </select>
        </div>
        <div>
          <label class="meta-label">Minimum Salary (€)</label>
          <input type="number" name="salary_min" placeholder="e.g. 80000" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        </div>
        <div>
          <label class="meta-label">Maximum Salary (€)</label>
          <input type="number" name="salary_max" placeholder="e.g. 110000" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        </div>
      </div>

      <div class="form-grid form-grid-three">
        <div>
          <label class="meta-label">Recruiter Source</label>
          <select name="recruiter_type" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);">
            <option value="none">Direct / No Recruiter</option>
            <option value="internal">Internal Recruiter / Talent</option>
            <option value="external">External Agency Recruiter</option>
          </select>
        </div>
        <div>
          <label class="meta-label">Recruiter Name (Optional)</label>
          <input type="text" name="recruiter_name" placeholder="e.g. Alex Rivera" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        </div>
        <div>
          <label class="meta-label">Recruiting Agency (If External)</label>
          <input type="text" name="recruiter_agency" placeholder="e.g. CyberTalent Search" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        </div>
      </div>

      <div class="form-grid form-grid-two">
        <div>
          <label class="meta-label">Job Post Link (LinkedIn or Company Careers)</label>
          <input type="url" name="job_post_url" placeholder="https://linkedin.com/jobs/view/... or leave empty if recruiter reachout" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        </div>
        <div>
          <label class="meta-label">Company Domain (For Logo)</label>
          <input type="text" name="company_domain" placeholder="example.com" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        </div>
      </div>

      <h3 class="edit-section-heading">Job details</h3>
      <div>
        <label class="meta-label">Keywords & Things to Remember (MAX 100 CHARACTERS)</label>
        <input type="text" name="keyword_note" maxlength="100" placeholder="e.g. Remote EU, Seed startup, Rust+Go, async-first culture" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary); font-family: var(--font-mono);" />
        <div style="font-size: 11px; color: var(--text-muted); text-align: right; margin-top: 2px;">
          <span id="keyword-char-count">0</span> / 100 chars
        </div>
      </div>

      <div>
        <label class="meta-label inline-icon-text">${icon("building", 13)} Company & Role Overview</label>
        <textarea name="company_overview" rows="3" placeholder="Summarize what the company does and what the role involves." style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary); resize: vertical; line-height: 1.4;"></textarea>
      </div>

      <h3 class="edit-section-heading">My notes</h3>
      <div>
        <label class="meta-label inline-icon-text">${icon("compass", 13)} Why I Want to Change Company (Top FAQ & Motivations)</label>
        <textarea name="reasons_to_change" rows="3" placeholder="Why do you want to change company? Key motivations, strategic rationale, growth drivers..." style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary); resize: vertical; line-height: 1.4;"></textarea>
      </div>

      <div>
        <label class="meta-label inline-icon-text">${icon("user", 13)} Relevant Experience</label>
        <textarea name="experience_notes" rows="3" placeholder="Experience and examples that match this role..." style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary); resize: vertical; line-height: 1.4;"></textarea>
      </div>

      <div>
        <label class="meta-label inline-icon-text">${icon("euro", 13)} Expected Salary for This Offer</label>
        <input type="text" name="expected_salary" placeholder="e.g. €85k base plus equity; flexible depending on the offer" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
      </div>

      <div>
        <label class="meta-label inline-icon-text">${icon("fileText", 13)} My Notes / Anything Else (Optional)</label>
        <textarea name="interview_notes" rows="3" placeholder="Write anything else you want to remember about this selection process..." style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary); resize: vertical; line-height: 1.4;"></textarea>
      </div>

      <div style="display: flex; align-items: center; gap: 8px; margin-top: 4px;">
        <input type="checkbox" name="is_referral" id="create-referral-cb" style="accent-color: #c084fc; width: 16px; height: 16px; cursor: pointer;" />
        <label for="create-referral-cb" style="font-size: 13px; color: var(--text-secondary); cursor: pointer; display: flex; align-items: center; gap: 6px;">
          ${icon("userCheck", 13)} Direct Candidate Referral (Check if this opportunity is from a referral)
        </label>
      </div>

      <div style="display: flex; align-items: center; gap: 8px; margin-top: 4px;">
        <input type="checkbox" name="create_default_stages" id="create-stages-cb" checked style="accent-color: var(--accent-blue);" />
        <label for="create-stages-cb" style="font-size: 13px; color: var(--text-secondary); cursor: pointer;">
          Automatically initialize standard stages (HR Screen, Technical, Cultural, Offer & Decision)
        </label>
      </div>

      <div class="form-actions">
        <button type="button" class="btn-secondary btn-cancel">Cancel</button>
        <button type="submit" class="btn-primary">Create Selection Process</button>
      </div>
    </form>
  `;

  const form = modalEl.querySelector("#new-process-form");
  activateModal(modalEl, 'input[name="company_name"]');
  const keywordInput = form.querySelector('input[name="keyword_note"]');
  const countEl = modalEl.querySelector("#keyword-char-count");
  const closeBtn = modalEl.querySelector(".modal-close-btn");
  const cancelBtn = modalEl.querySelector(".btn-cancel");

  keywordInput.addEventListener("input", () => {
    countEl.textContent = keywordInput.value.length;
  });

  const closeForm = () => {
    closeWithFlip(modalEl, backdropEl, () => {
      modalEl.innerHTML = "";
      restoreModalFocus();
    });
  };

  closeBtn.addEventListener("click", closeForm);
  cancelBtn.addEventListener("click", closeForm);

  const minInput = form.querySelector('input[name="salary_min"]');
  const maxInput = form.querySelector('input[name="salary_max"]');
  const salaryTypeSelect = form.querySelector('select[name="salary_type"]');
  const updateSalaryTypeFromInputs = () => {
    const minVal = minInput?.value ? parseInt(minInput.value, 10) : null;
    const maxVal = maxInput?.value ? parseInt(maxInput.value, 10) : null;
    const salaryType = resolveSalaryType(minVal, maxVal);
    if (salaryType && salaryTypeSelect) salaryTypeSelect.value = salaryType;
  };
  minInput?.addEventListener("input", updateSalaryTypeFromInputs);
  maxInput?.addEventListener("input", updateSalaryTypeFromInputs);
  form.addEventListener("submit", (event) => submitNewJobForm(event, form, closeForm));
}

filterTabs.forEach((tab) => {
  tab.addEventListener("click", () => {
    setFilter(tab.getAttribute("data-filter"));
    closeMobileMenu();
    if (window.matchMedia("(max-width: 800px)").matches) menuToggle?.focus();
  });
});
scheduleButton?.addEventListener("click", () => {
  setFilter("schedule");
  closeMobileMenu();
  if (window.matchMedia("(max-width: 800px)").matches) menuToggle?.focus();
});

filterCurrentAction?.addEventListener("click", () => {
  setFilter(currentStatusFilter);
  closeMobileMenu();
  if (window.matchMedia("(max-width: 800px)").matches) menuToggle?.focus();
});

processFilterTrigger?.addEventListener("click", () => {
  if (!processFilterPanel) return;
  const willOpen = processFilterPanel.hidden;
  processFilterPanel.hidden = !willOpen;
  processFilterTrigger.setAttribute("aria-expanded", String(willOpen));
});

[...processFilterArrangements, processFilterCurrency, processFilterAnyReferral, processFilterReferral, processFilterNoReferral].forEach((control) => control?.addEventListener("change", saveProcessFilters));
[processFilterPostedSalaryMin, processFilterPostedSalaryMax].forEach((control) => control?.addEventListener("change", saveProcessFilters));
processFilterExpectedSalary?.addEventListener("change", saveProcessFilters);
processFilterTechnologySearch?.addEventListener("input", renderTechnologyFilterOptions);
processFilterTechnologyOptions?.addEventListener("change", saveProcessFilters);

processFilterChips?.addEventListener("click", (event) => {
  const chip = event.target.closest("[data-remove-filter]");
  if (chip) clearProcessFilterCategory(chip.dataset.removeFilter, chip.dataset.filterValue);
});

function clearProcessFilterCategory(category, value = "") {
  switch (category) {
    case "arrangement": clearArrangementFilter(value); break;
    case "referral": clearReferralFilter(); break;
    case "expectedSalary": clearExpectedSalaryFilter(); break;
    case "postingSalary": clearPostingSalaryFilter(); break;
    case "technology": {
      processFilters.technologies = processFilters.technologies.filter((id) => id !== value);
      renderTechnologyFilterOptions();
      break;
    }
    default: return;
  }
  saveProcessFilters();
}

function clearArrangementFilter(value = "") {
  processFilterArrangements.forEach((control) => {
    if (!value || control.value === value) control.checked = false;
  });
}

function clearReferralFilter() {
  if (processFilterReferral) processFilterReferral.checked = false;
  if (processFilterNoReferral) processFilterNoReferral.checked = false;
  if (processFilterAnyReferral) processFilterAnyReferral.checked = true;
}

function clearExpectedSalaryFilter() {
  if (processFilterExpectedSalary) processFilterExpectedSalary.value = "";
}

function clearPostingSalaryFilter() {
  if (processFilterPostedSalaryMin) processFilterPostedSalaryMin.value = "";
  if (processFilterPostedSalaryMax) processFilterPostedSalaryMax.value = "";
  if (processFilterCurrency) processFilterCurrency.value = "";
}

processFilterClear?.addEventListener("click", () => {
  processFilterArrangements.forEach((control) => { control.checked = false; });
  if (processFilterExpectedSalary) processFilterExpectedSalary.value = "";
  if (processFilterPostedSalaryMin) processFilterPostedSalaryMin.value = "";
  if (processFilterPostedSalaryMax) processFilterPostedSalaryMax.value = "";
  if (processFilterCurrency) processFilterCurrency.value = "";
  if (processFilterReferral) processFilterReferral.checked = false;
  if (processFilterNoReferral) processFilterNoReferral.checked = false;
  if (processFilterTechnologySearch) processFilterTechnologySearch.value = "";
  processFilters.technologies = [];
  renderTechnologyFilterOptions();
  saveProcessFilters();
});

filterMenuTrigger?.addEventListener("click", () => {
  const willOpen = filterMenu?.hidden;
  if (!filterMenu) return;
  filterMenu.hidden = !willOpen;
  filterMenuTrigger.setAttribute("aria-expanded", String(willOpen));
});

filterMenuTrigger?.addEventListener("keydown", (event) => {
  if (!filterMenu || !["ArrowDown", "ArrowUp"].includes(event.key)) return;
  event.preventDefault();
  filterMenu.hidden = false;
  filterMenuTrigger.setAttribute("aria-expanded", "true");
  const options = [...filterTabs];
  (event.key === "ArrowDown" ? options[0] : options.at(-1))?.focus();
});

filterMenu?.addEventListener("keydown", (event) => {
  const options = [...filterTabs];
  const index = options.indexOf(document.activeElement);
  if (index < 0) return;
  if (event.key === "ArrowDown" || event.key === "ArrowUp") {
    event.preventDefault();
    const direction = event.key === "ArrowDown" ? 1 : -1;
    options[(index + direction + options.length) % options.length]?.focus();
  }
});

moreActionsTrigger?.addEventListener("click", () => {
  if (!moreActionsMenu) return;
  const willOpen = moreActionsMenu.hidden;
  moreActionsMenu.hidden = !willOpen;
  moreActionsTrigger.setAttribute("aria-expanded", String(willOpen));
});

moreActionsTrigger?.addEventListener("keydown", (event) => {
  if (!moreActionsMenu || !["ArrowDown", "ArrowUp"].includes(event.key)) return;
  event.preventDefault();
  moreActionsMenu.hidden = false;
  moreActionsTrigger.setAttribute("aria-expanded", "true");
  const options = [...moreActionsMenu.querySelectorAll('[role="menuitem"]')];
  (event.key === "ArrowDown" ? options[0] : options.at(-1))?.focus();
});

moreActionsMenu?.addEventListener("keydown", (event) => {
  const options = [...moreActionsMenu.querySelectorAll('[role="menuitem"]')];
  const index = options.indexOf(document.activeElement);
  if (index < 0 || !["ArrowDown", "ArrowUp"].includes(event.key)) return;
  event.preventDefault();
  const direction = event.key === "ArrowDown" ? 1 : -1;
  options[(index + direction + options.length) % options.length]?.focus();
});

document.addEventListener("click", (event) => {
  if (!event.target.closest(".filter-tabs")) closeFilterMenu();
  if (!event.target.closest(".more-actions-menu")) closeMoreActionsMenu();
  if (!event.target.closest(".process-filter-control") && processFilterPanel && !processFilterPanel.hidden) {
    processFilterPanel.hidden = true;
    processFilterTrigger?.setAttribute("aria-expanded", "false");
  }
  if (!event.target.closest(".process-sort-control")) closeSortMenu();
});

searchInput.addEventListener("input", () => {
  clearTimeout(searchDebounceTimer);
  searchDebounceTimer = setTimeout(() => {
    currentSearch = searchInput.value;
    refreshApp();
  }, 200);
});

searchFocusButton?.addEventListener("click", () => searchInput.focus());

newProcessButtons.forEach((button) => button.addEventListener("click", openNewProcessModal));
dataManagementButton?.addEventListener("click", openDataModal);
cvLibraryButton?.addEventListener("click", openCvLibraryModal);
technologyLibraryButton?.addEventListener("click", openTechnologyLibraryModal);
menuToggle?.addEventListener("click", () => {
  const isOpen = menuToggle.getAttribute("aria-expanded") === "true";
  menuToggle.setAttribute("aria-expanded", String(!isOpen));
  menuToggle.setAttribute("aria-label", isOpen ? "Open navigation menu" : "Close navigation menu");
  headerControls?.classList.toggle("is-open", !isOpen);
});
backdropEl.addEventListener("click", (e) => {
  if (e.target === backdropEl) {
    if (modalEl.classList.contains("data-transfer-modal")) closeTransferModal();
    else closeWithFlip(modalEl, backdropEl, () => {
      modalEl.innerHTML = "";
      restoreModalFocus();
    });
  }
});

window.addEventListener("keydown", handleGlobalKeydown);

filterTabs.forEach((tab) => {
  const isActive = tab.getAttribute("data-filter") === currentStatusFilter;
  tab.classList.toggle("active", isActive);
  tab.setAttribute("aria-checked", String(isActive));
});

initializeProcessFilterControls();
scheduleButton?.classList.toggle("active", currentFilter === "schedule");
scheduleButton?.setAttribute("aria-pressed", String(currentFilter === "schedule"));
updateFilterSummary();

await refreshApp();
document.body.dataset.appReady = "true";
