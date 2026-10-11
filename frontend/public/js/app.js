/**
 * @fileoverview Main Client Application Entry Point.
 * Coordinates global state, filter tabs, instant search, and keyboard shortcuts.
 */

import { api } from "./api.js";
import { renderCardGrid } from "./components/cardGrid.js";
import { renderScheduleView } from "./components/scheduleView.js";
import { openDetailModal } from "./components/detailModal.js";
import { openCvLibrary } from "./components/cvLibrary.js";
import { openTechnologyLibrary } from "./components/technologyLibrary.js";
import { closeWithFlip, cancelPendingFlipClose } from "./flip.js";
import { icon } from "./icons.js";
import { activateModal, restoreModalFocus, trapModalTab } from "./modalA11y.js";
import { showToast } from "./utils/toast.js";
import { confirmAction } from "./utils/confirmDialog.js";
import { escapeHtml } from "./utils/sanitize.js";
import { renderMarkdown } from "./utils/markdown.js";
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
let searchPeriods = [];

/** Infers the salary type from entered range bounds. */
function resolveSalaryType(minimum, maximum) {
  if (minimum && maximum) return "limited";
  if (minimum) return "no_max";
  if (maximum) return "no_min";
  return null;
}

const cardGridEl = document.querySelector("#cards-grid");
const appLoadErrorEl = document.querySelector("#app-load-error");
const appLoadRetryButton = document.querySelector("#app-load-retry");
const processToolbarEl = document.querySelector("#process-toolbar");
const processSortControlEl = document.querySelector(".process-sort-control");
const modalEl = document.querySelector("#detail-modal");
const backdropEl = document.querySelector("#modal-backdrop");
const searchInput = document.querySelector("#search-input");
const searchFocusButton = document.querySelector(".search-focus");
const filterTabs = document.querySelectorAll(".filter-tab");
const scheduleButton = document.querySelector("#tab-schedule");
const filterMenu = document.querySelector("#filter-menu");
const filterMenuTrigger = document.querySelector("#filter-menu-trigger");
const filterCurrentAction = document.querySelector("#filter-current-action");
const newProcessButtons = document.querySelectorAll(".new-process-trigger");
const menuToggle = document.querySelector("#btn-menu-toggle");
const headerControls = document.querySelector("#header-controls");
const dataManagementButton = document.querySelector("#btn-data-management");

const technologyLibraryButton = document.querySelector("#btn-technology-library");
const themeToggle = document.querySelector("#btn-theme-toggle");
const processFilterTrigger = document.querySelector("#process-filter-trigger");
const searchPeriodFilter = document.querySelector("#search-period-filter");
const searchPeriodTrigger = document.querySelector("#search-period-trigger");
const searchPeriodCurrent = document.querySelector("#search-period-current");
const searchPeriodMenu = document.querySelector("#search-period-menu");
const searchPeriodSelectWrap = document.querySelector(".search-period-select-wrap");
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
const processFilterPanelFooter = document.querySelector("#process-filter-panel-footer");
const processFilterChips = document.querySelector("#process-filter-chips");
const processToolbarActiveState = document.querySelector("#process-toolbar-active-state");
const processSortTrigger = document.querySelector("#process-sort-trigger");
const processSortCurrent = document.querySelector("#process-sort-current");
const processSortMenu = document.querySelector("#process-sort-menu");
const processSortOptions = document.querySelectorAll("[data-sort-mode]");
const processFilterAnyReferral = document.querySelector("#process-filter-any-referral");
let managementReturnJobId = "";

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
  filterMenu.hidden = true;
  filterMenuTrigger?.setAttribute("aria-expanded", "false");
  if (restoreFocus) filterMenuTrigger?.focus();
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
    const boardData = await refreshBoardCounts();
    await renderSelectedBoardView(boardData);
    clearAppLoadError();
  } catch (err) {
    console.error("Failed to load jobs data:", err);
    showAppLoadError();
  }
}

async function refreshBoardCounts() {
  const allPeriodCounts = await api.getJobCounts();
  const meetings = await api.getMeetings();
  const countMeetingsEl = document.querySelector("#count-meetings");
  if (countMeetingsEl) countMeetingsEl.textContent = meetings.length;
  updateFilterSummary();
  updateSortControl();
  await loadAvailableTechnologies();
  searchPeriods = await api.getSearchPeriods();
  renderSearchPeriodOptions();
  const scopedCounts = await getCountsForSelectedPeriod(allPeriodCounts);
  updateBoardCountLabels(scopedCounts);
  updateFilterSummary();
  updateProcessFilterSummary();
  return { allPeriodCounts, scopedCounts };
}

function updateBoardCountLabels(counts) {
  document.querySelector("#count-all").textContent = counts.all;
  document.querySelector("#count-waiting").textContent = counts.waiting;
  document.querySelector("#count-ongoing").textContent = counts.ongoing;
  document.querySelector("#count-accepted").textContent = counts.accepted;
  document.querySelector("#count-rejected").textContent = counts.rejected;
}

async function renderSelectedBoardView({ allPeriodCounts, scopedCounts }) {
  if (currentFilter === "schedule") {
    cardGridEl.classList.add("schedule-mode");
    await renderScheduleView(cardGridEl, modalEl, backdropEl, refreshApp);
    return;
  }
  cardGridEl.classList.remove("schedule-mode");
  await renderProcessCards(allPeriodCounts, scopedCounts);
}

async function renderProcessCards(allPeriodCounts, scopedCounts) {
  const jobs = await api.getJobs(currentFilter, currentSearch);
  const filteredJobs = filterJobs(jobs, processFilters);
  const sortMode = currentFilter === "all" ? "manual" : readSavedSortMode(window.localStorage, currentFilter);
  renderCardGrid(cardGridEl, sortJobsForDisplay(filteredJobs, currentFilter, sortMode), modalEl, backdropEl, refreshApp, sortMode, {
    filter: currentFilter,
    totalCount: allPeriodCounts.all,
    statusCount: scopedCounts[currentFilter] ?? scopedCounts.all,
    hasCustomView: hasActiveCustomView(),
    onResetView: resetSearchAndFilters,
  });
}

function hasActiveCustomView() {
  return Boolean(currentSearch.trim()) || processFilters.searchPeriod !== "all"
    || processFilters.arrangements.length > 0 || Boolean(processFilters.expectedSalaryQuery)
    || processFilters.postedSalaryMin !== null || processFilters.postedSalaryMax !== null
    || Boolean(processFilters.currency) || Boolean(processFilters.referral)
    || processFilters.technologies.length > 0;
}

function clearAppLoadError() {
  if (!appLoadErrorEl) return;
  appLoadErrorEl.hidden = true;
  if (appLoadRetryButton) {
    appLoadRetryButton.disabled = false;
    appLoadRetryButton.textContent = "Try again";
  }
}

function showAppLoadError() {
  if (!appLoadErrorEl) return;
  appLoadErrorEl.hidden = false;
  if (appLoadRetryButton) {
    appLoadRetryButton.disabled = false;
    appLoadRetryButton.textContent = "Try again";
  }
}

async function getCountsForSelectedPeriod(allPeriodCounts) {
  if (processFilters.searchPeriod === "all") return allPeriodCounts;
  const jobs = await api.getJobs("all");
  const counts = { all: 0, waiting: 0, ongoing: 0, accepted: 0, rejected: 0 };
  for (const job of jobs) {
    const matchesPeriod = processFilters.searchPeriod === "unassigned"
      ? !job.search_period_id
      : job.search_period_id === processFilters.searchPeriod;
    if (!matchesPeriod) continue;
    counts.all += 1;
    if (Object.hasOwn(counts, job.status) && job.status !== "all") counts[job.status] += 1;
  }
  return counts;
}

/** Restores validated card filter choices from browser storage. */
function loadProcessFilters() {
  try {
    const saved = JSON.parse(window.localStorage.getItem(PROCESS_FILTER_STORAGE_KEY) || "{}");
    return {
      arrangements: readSavedArrangements(saved),
      searchPeriod: typeof saved.searchPeriod === "string" ? saved.searchPeriod : "all",
      expectedSalaryQuery: typeof saved.expectedSalaryQuery === "string" ? saved.expectedSalaryQuery : "",
      postedSalaryMin: parseFilterNumber(saved.postedSalaryMin ?? saved.salaryMin),
      postedSalaryMax: parseFilterNumber(saved.postedSalaryMax ?? saved.salaryMax),
      currency: readSavedCurrency(saved.currency),
      referral: readSavedReferral(saved.referral),
      technologies: readSavedTechnologies(saved.technologies),
    };
  } catch {
    return { arrangements: [], searchPeriod: "all", expectedSalaryQuery: "", postedSalaryMin: null, postedSalaryMax: null, currency: "", referral: "", technologies: [] };
  }
}

function renderSearchPeriodOptions() {
  if (!searchPeriodFilter || !searchPeriodMenu || !searchPeriodCurrent || !searchPeriodTrigger) return;
  const options = [
    { value: "all", label: "All periods", menuLabel: "All search periods" },
    { value: "unassigned", label: "Unassigned", menuLabel: "Unassigned" },
    ...searchPeriods.map((period) => ({ value: period.id, label: period.name, menuLabel: `${period.name} · ${period.start_date} – ${period.end_date || "Present"}` })),
  ];
  const selected = searchPeriods.some((period) => period.id === processFilters.searchPeriod)
    || ["all", "unassigned"].includes(processFilters.searchPeriod) ? processFilters.searchPeriod : "all";
  processFilters.searchPeriod = selected;
  searchPeriodFilter.value = selected;
  const selectedOption = options.find((option) => option.value === selected);
  searchPeriodCurrent.textContent = selectedOption.label;
  searchPeriodCurrent.dataset.compactLabel = selected === "all" ? "All"
    : selected === "unassigned" ? "No period" : searchPeriodCurrent.textContent;
  searchPeriodTrigger.title = selectedOption.menuLabel;
  searchPeriodTrigger.dataset.selected = String(selected !== "all");
  searchPeriodTrigger.setAttribute("aria-label", `Search period: ${selectedOption.menuLabel}`);
  searchPeriodMenu.innerHTML = `
    <div class="search-period-menu-options" role="presentation">${options.map((option) => `<button type="button" role="menuitemradio" aria-checked="${option.value === selected}" data-search-period="${escapeHtml(option.value)}">${escapeHtml(option.menuLabel)}</button>`).join("")}</div>
    <div class="search-period-menu-footer"><button type="button" role="menuitem" data-manage-search-periods>Manage search periods</button></div>`;
}

function periodForToday() {
  const now = new Date();
  const today = [now.getFullYear(), String(now.getMonth() + 1).padStart(2, "0"), String(now.getDate()).padStart(2, "0")].join("-");
  return searchPeriods.find((period) => period.start_date <= today && (!period.end_date || period.end_date >= today));
}

function searchPeriodOptions(selectedID) {
  return [
    `<option value="">Unassigned</option>`,
    ...searchPeriods.map((period) => `<option value="${escapeHtml(period.id)}" ${period.id === selectedID ? "selected" : ""}>${escapeHtml(period.name)} · ${escapeHtml(period.start_date)} – ${escapeHtml(period.end_date || "Present")}</option>`),
  ].join("");
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

function resizeRoleDescription(textarea) {
  textarea.style.height = "auto";
  const maxHeight = Number.parseFloat(window.getComputedStyle(textarea).maxHeight);
  const contentHeight = textarea.scrollHeight;
  const height = Number.isFinite(maxHeight) ? Math.min(contentHeight, maxHeight) : contentHeight;
  textarea.style.height = `${height}px`;
  textarea.style.overflowY = contentHeight > height ? "auto" : "hidden";
}

document.addEventListener("input", (event) => {
  if (event.target instanceof HTMLTextAreaElement && event.target.matches(".role-description-textarea")) {
    resizeRoleDescription(event.target);
  }
});

document.addEventListener("focusin", (event) => {
  if (event.target instanceof HTMLTextAreaElement && event.target.matches(".role-description-textarea")) {
    resizeRoleDescription(event.target);
  }
});

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
  const count = getActiveProcessFilterCount();
  const badge = document.querySelector("#process-filter-count");
  const summary = document.querySelector("#process-filter-summary");
  if (badge) {
    badge.textContent = String(count);
    badge.hidden = count === 0;
  }
  updateProcessFilterTriggerLabel(count);
  const chips = getProcessFilterChips();
  if (processFilterChips) processFilterChips.innerHTML = chips.map(renderProcessFilterChip).join("");
  if (processToolbarActiveState) processToolbarActiveState.hidden = chips.length === 0;
  if (processFilterPanelFooter) processFilterPanelFooter.hidden = count === 0;
  if (summary) summary.textContent = "";
}

function renderProcessFilterChip({ category, label, value = "" }) {
  const valueAttribute = value ? `data-filter-value="${escapeHtml(value)}"` : "";
  const accessibleLabel = category === "searchPeriod" ? label.replace(/^Period: /, "") : label;
  const periodIcon = category === "searchPeriod" ? '<svg aria-hidden="true" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="5" width="18" height="16" rx="2"/><path d="M16 3v4M8 3v4M3 10h18"/></svg>' : "";
  return `<button type="button" class="process-filter-chip${category === "searchPeriod" ? " search-period-scope-chip" : ""}" data-remove-filter="${category}" ${valueAttribute} aria-label="Clear ${category === "searchPeriod" ? "search period" : "filter"}: ${escapeHtml(accessibleLabel)}" title="${escapeHtml(label)}">${periodIcon}<span>${escapeHtml(label)}</span><svg aria-hidden="true" width="11" height="11" viewBox="0 0 12 12" fill="none"><path d="m3 3 6 6m0-6L3 9" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg></button>`;
}

function getActiveProcessFilterCount() {
  return Number(processFilters.arrangements.length > 0)
    + Number(Boolean(processFilters.expectedSalaryQuery))
    + Number(processFilters.postedSalaryMin !== null || processFilters.postedSalaryMax !== null)
    + Number(Boolean(processFilters.currency))
    + Number(Boolean(processFilters.referral))
    + Number(processFilters.technologies.length > 0);
}

function updateProcessFilterTriggerLabel(activeFilterCount) {
  if (!processFilterTrigger) return;
  const count = activeFilterCount ?? getActiveProcessFilterCount();
  const filterDescription = count ? `; ${count} active ${count === 1 ? "filter" : "filters"}` : "; no active filters";
  processFilterTrigger.setAttribute("aria-label", `Filters${filterDescription}`);
}

function getProcessFilterChips() {
  return [
    ...processFilters.arrangements.map((value) => ({ category: "arrangement", value, label: ({ remote: "Remote", hybrid: "Hybrid", on_site: "On-site" })[value] || value })),
    ...getReferralFilterChips(),
    ...getSalaryFilterChips(),
    ...getTechnologyFilterChips(),
  ];
}

function getReferralFilterChips() {
  if (!processFilters.referral) return [];
  return [{ category: "referral", label: processFilters.referral === "yes" ? "Referral" : "No referral" }];
}

function getSalaryFilterChips() {
  const chips = [];
  if (processFilters.expectedSalaryQuery) chips.push({ category: "expectedSalary", label: `Target: ${processFilters.expectedSalaryQuery}` });
  if (processFilters.postedSalaryMin !== null || processFilters.postedSalaryMax !== null || processFilters.currency) {
    const lower = processFilters.postedSalaryMin === null ? "Any" : processFilters.postedSalaryMin.toLocaleString();
    const upper = processFilters.postedSalaryMax === null ? "Any" : processFilters.postedSalaryMax.toLocaleString();
    chips.push({ category: "postingSalary", label: `Posting ${lower}–${upper}${processFilters.currency ? ` ${processFilters.currency}` : ""}` });
  }
  return chips;
}

function getTechnologyFilterChips() {
  const chips = [];
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
    searchPeriod: searchPeriodFilter?.value || "all",
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
  if (!processToolbarEl || !processSortControlEl || !processSortTrigger || !processSortCurrent) return;
  const isStatusTab = ["waiting", "ongoing", "accepted", "rejected"].includes(currentFilter);
  processToolbarEl.hidden = currentFilter === "schedule";
  processSortControlEl.hidden = !isStatusTab;
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
processSortTrigger?.addEventListener("keydown", (event) => {
  if (event.key === "Escape" && processSortMenu && !processSortMenu.hidden) {
    event.preventDefault();
    closeSortMenu();
    processSortTrigger.focus();
    return;
  }
  if (!processSortMenu || !["ArrowDown", "ArrowUp"].includes(event.key)) return;
  event.preventDefault();
  processSortMenu.hidden = false;
  processSortTrigger.setAttribute("aria-expanded", "true");
  const selected = processSortMenu.querySelector('[aria-checked="true"]');
  (selected || processSortOptions[0])?.focus();
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
  processSortTrigger?.focus();
  void refreshApp();
});

processSortMenu?.addEventListener("keydown", (event) => {
  if (event.key === "Escape") {
    event.stopPropagation();
    closeSortMenu();
    processSortTrigger?.focus();
    return;
  }
  const options = [...processSortOptions];
  const index = options.indexOf(event.target);
  if (index < 0) return;
  let nextIndex = null;
  if (event.key === "ArrowDown") nextIndex = (index + 1) % options.length;
  if (event.key === "ArrowUp") nextIndex = (index - 1 + options.length) % options.length;
  if (event.key === "Home") nextIndex = 0;
  if (event.key === "End") nextIndex = options.length - 1;
  if (nextIndex === null) return;
  event.preventDefault();
  options[nextIndex]?.focus();
});
processSortControlEl?.addEventListener("focusout", (event) => {
  if (event.relatedTarget instanceof Node && processSortControlEl.contains(event.relatedTarget)) return;
  closeSortMenu();
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
    status: "waiting",
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
    search_period_id: data.get("search_period_id") || "",
    create_default_stages: false,
  };
}

/** Persists a submitted job form and reports the result to the user. */
async function submitNewJobForm(event, form, closeForm) {
  try {
    event.preventDefault();
    await api.createJob(buildJobPayload(form));
    showToast("Selection process created successfully!", "success");
    closeForm();
    setFilter("waiting");
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
    modalEl.classList.remove("new-process-modal");
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
  focusTarget?.focus();
}

/** Closes the data transfer dialog and returns focus to its trigger. */
function closeDataModal() {
  closeManagementModal(dataManagementButton);
}

function defaultSearchPeriodName(startDate, endDate) {
  if (!startDate) return "";
  const formatMonth = (value) => new Intl.DateTimeFormat(undefined, { month: "long", year: "numeric", timeZone: "UTC" }).format(new Date(`${value}T00:00:00Z`));
  const startLabel = formatMonth(startDate);
  if (!endDate) return startLabel;
  const endLabel = formatMonth(endDate);
  return startLabel === endLabel ? startLabel : `${startLabel} – ${endLabel}`;
}

async function openSearchPeriodsModal() {
  closeMobileMenu();
  cancelPendingFlipClose(true);
  resetModalAnimation();
  modalEl.classList.add("data-transfer-modal");
  backdropEl.classList.add("active");
  modalEl.innerHTML = `
    <div class="modal-header data-transfer-header">
      <div><div class="modal-title">Search Periods</div><div class="modal-company">Unassigned cards added within a period’s date range are grouped automatically. Leave the end date blank while the search is active.</div></div>
      <button class="btn-secondary" id="btn-close-search-periods" type="button" aria-label="Close">${icon("close", 18)}</button>
    </div>
    <div class="search-period-manager">
      <form id="search-period-form" class="search-period-form">
        <input type="hidden" name="id" />
        <label>Name<input name="name" maxlength="100" required placeholder="March 2026" /></label>
        <div class="form-grid form-grid-two">
          <label>Start date<input name="start_date" type="date" required /></label>
          <label>End date<input name="end_date" type="date" /><span class="search-period-optional">Optional · leave blank while the search is active</span></label>
        </div>
        <div class="search-period-form-actions">
          <button class="btn-primary" type="submit">Create period</button>
          <button class="btn-secondary" id="btn-cancel-search-period-edit" type="button" hidden>Cancel editing</button>
        </div>
      </form>
      <div id="search-period-list" class="search-period-list"></div>
    </div>`;
  activateModal(modalEl, '[name="name"]');
  const closeButton = modalEl.querySelector("#btn-close-search-periods");
  const form = modalEl.querySelector("#search-period-form");
  const list = modalEl.querySelector("#search-period-list");
  const idField = form.elements.id;
  const nameField = form.elements.name;
  const startField = form.elements.start_date;
  const endField = form.elements.end_date;
  const submitButton = form.querySelector('[type="submit"]');
  const cancelEdit = modalEl.querySelector("#btn-cancel-search-period-edit");
  let autoName = true;

  const resetForm = () => {
    form.reset();
    idField.value = "";
    autoName = true;
    submitButton.textContent = "Create period";
    cancelEdit.hidden = true;
  };
  const renderList = () => {
    list.innerHTML = searchPeriods.length ? searchPeriods.map((period) => `
      <article class="search-period-row">
        <div><strong>${escapeHtml(period.name)}</strong><span>${escapeHtml(period.start_date)} – ${escapeHtml(period.end_date || "Present")}</span><small>${period.job_count} job${period.job_count === 1 ? "" : "s"}</small></div>
        <div class="search-period-actions">
          <button class="btn-secondary" type="button" data-edit-period="${escapeHtml(period.id)}">Edit</button>
          <button class="btn-secondary" type="button" data-delete-period="${escapeHtml(period.id)}">Delete</button>
        </div>
      </article>`).join("") : '<div class="search-period-empty"><strong>No search periods yet</strong><span>Create one above. Unassigned cards added within its date range will be grouped automatically.</span></div>';
  };
  const focusPeriodAfterDelete = (id, fallbackIndex) => {
    const deleteButtons = [...list.querySelectorAll("[data-delete-period]")];
    const fallback = deleteButtons[Math.max(0, Math.min(fallbackIndex, deleteButtons.length - 1))];
    (deleteButtons.find((button) => button.dataset.deletePeriod === id) || fallback || nameField)?.focus();
  };
  const reload = async () => {
    searchPeriods = await api.getSearchPeriods();
    renderList();
    renderSearchPeriodOptions();
  };
  closeButton.addEventListener("click", () => closeManagementModal(searchPeriodTrigger));
  cancelEdit.addEventListener("click", resetForm);
  nameField.addEventListener("input", () => { autoName = false; });
  [startField, endField].forEach((input) => input.addEventListener("change", () => {
    if (autoName || !nameField.value) nameField.value = defaultSearchPeriodName(startField.value, endField.value);
  }));
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const payload = { name: nameField.value, start_date: startField.value, end_date: endField.value };
    try {
      const isEditing = Boolean(idField.value);
      let createdPeriod;
      if (isEditing) await api.updateSearchPeriod(idField.value, payload);
      else createdPeriod = await api.createSearchPeriod(payload);
      resetForm();
      await reload();
      if (createdPeriod) {
        processFilters.searchPeriod = createdPeriod.id;
        if (searchPeriodFilter) searchPeriodFilter.value = createdPeriod.id;
        renderSearchPeriodOptions();
        try { window.localStorage.setItem(PROCESS_FILTER_STORAGE_KEY, JSON.stringify(processFilters)); } catch { /* Keep the new period selected until this page closes. */ }
      }
      await refreshApp();
      showToast("Search period saved", "success");
    } catch (error) { showToast(error.message, "error"); }
  });
  list.addEventListener("click", async (event) => {
    const editButton = event.target.closest("[data-edit-period]");
    const deleteButton = event.target.closest("[data-delete-period]");
    if (editButton) {
      const period = searchPeriods.find((item) => item.id === editButton.dataset.editPeriod);
      if (!period) return;
      idField.value = period.id;
      nameField.value = period.name;
      startField.value = period.start_date;
      endField.value = period.end_date;
      autoName = false;
      submitButton.textContent = "Save changes";
      cancelEdit.hidden = false;
      nameField.focus();
    }
    if (deleteButton) {
      const period = searchPeriods.find((item) => item.id === deleteButton.dataset.deletePeriod);
      if (!period) return;
      const fallbackIndex = [...list.querySelectorAll("[data-delete-period]")]
        .findIndex((button) => button.dataset.deletePeriod === period.id);
      const confirmed = await confirmAction({
        title: "Delete search period?",
        message: `Its ${period.job_count} job${period.job_count === 1 ? "" : "s"} will become unassigned.`,
        confirmLabel: "Delete period",
        danger: true,
      });
      if (!confirmed) return;
      try {
        await api.deleteSearchPeriod(period.id);
        if (processFilters.searchPeriod === period.id) {
          processFilters.searchPeriod = "all";
          try { window.localStorage.setItem(PROCESS_FILTER_STORAGE_KEY, JSON.stringify(processFilters)); } catch { /* Keep the filter reset for this session. */ }
        }
        await reload();
        focusPeriodAfterDelete(period.id, fallbackIndex);
        await refreshApp();
        showToast("Search period deleted; its jobs are unassigned", "success");
      } catch (error) { showToast(error.message, "error"); }
    }
  });
  try {
    await reload();
  } catch (error) {
    showToast(error.message, "error");
  }
}

/** Closes the CV library and returns to the job where it was opened. */
function closeCvLibraryModal() {
  const jobId = managementReturnJobId;
  managementReturnJobId = "";
  closeManagementModal(dataManagementButton);
  if (jobId) void openDetailModal(modalEl, backdropEl, jobId, refreshApp);
}

/** Closes the technology catalog and returns to its launch context. */
function closeTechnologyLibraryModal() {
  const jobId = managementReturnJobId;
  managementReturnJobId = "";
  closeManagementModal(jobId ? technologyLibraryButton : processFilterTrigger);
  if (jobId) void openDetailModal(modalEl, backdropEl, jobId, refreshApp);
  else {
    technologiesLoaded = false;
    void loadAvailableTechnologies();
  }
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
          <input id="backup-import-file" class="data-transfer-file-input" type="file" accept=".zip,application/zip" aria-label="Choose backup file" tabindex="-1" />
          <button id="backup-import-choose" class="data-transfer-choose" type="button">Choose file</button>
          <span id="backup-import-file-name" class="data-transfer-file-name" role="status" aria-live="polite" aria-atomic="true">No file selected</span>
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

/** Opens the CV library from the active job's CV section. */
function openCvLibraryModal(event) {
  managementReturnJobId = event?.detail?.jobId || "";
  closeMobileMenu();
  cancelPendingFlipClose(true);
  resetModalAnimation();
  modalEl.classList.add("data-transfer-modal");
  backdropEl.classList.add("active");
  openCvLibrary(modalEl, closeCvLibraryModal);
}

/** Opens the shared technology catalog from the filter panel or a job detail. */
function openTechnologyLibraryModal(event) {
  managementReturnJobId = event?.detail?.jobId || "";
  if (processFilterPanel) processFilterPanel.hidden = true;
  processFilterTrigger?.setAttribute("aria-expanded", "false");
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
      const confirmed = await confirmAction({
        title: "Replace this workspace with an empty backup?",
        message: "This ZIP contains no saved processes. Continuing will remove the processes currently in your workspace.",
        confirmLabel: "Replace workspace",
        danger: true,
      });
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
  if (menuToggle?.getAttribute("aria-expanded") === "true") {
    closeMobileMenu();
    menuToggle.focus();
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
  modalEl.classList.remove("data-transfer-modal");
  modalEl.classList.add("new-process-modal");
  backdropEl.classList.add("active");
  modalEl.innerHTML = `
    <div class="modal-header">
      <h2 class="modal-title inline-icon-text">${icon("plus", 16)} Add New Selection Process</h2>
      <button class="modal-close-btn" type="button" aria-label="Close dialog">${icon("close", 14)}</button>
    </div>
    <form id="new-process-form" class="process-form">
      <div class="form-grid form-grid-two">
        <div>
          <label class="meta-label" for="create-position-title">Position Title *</label>
          <input id="create-position-title" type="text" name="position_title" placeholder="e.g. Senior AI Systems Engineer" autocomplete="organization-title" required style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        </div>
        <div>
          <label class="meta-label" for="create-company-name">Company Name (optional)</label>
          <input id="create-company-name" type="text" name="company_name" placeholder="e.g. Acme" autocomplete="organization" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
          <span class="process-form-helper">If the client is not disclosed, leave this blank; the card will use “Unknown.”</span>
        </div>
      </div>

      ${searchPeriods.length ? `<div>
        <label class="meta-label" for="create-search-period">Search Period</label>
        <select id="create-search-period" name="search_period_id">
          ${searchPeriodOptions(periodForToday()?.id || "")}
        </select>
      </div>` : ""}

      <h3 class="edit-section-heading">Job posting</h3>
      <div>
        <label class="meta-label" for="create-job-post-url">Job Post Link (Optional)</label>
        <input id="create-job-post-url" type="url" name="job_post_url" placeholder="https://company.com/jobs/..." style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        <span class="process-form-helper">Leave blank for recruiter-only opportunities</span>
      </div>

      <div>
        <label class="meta-label inline-icon-text" for="create-company-overview">${icon("building", 13)} Company & Role Overview</label>
        <div class="create-overview-editor">
          <div class="markdown-editor-tabs" role="tablist" aria-label="Company and role overview editor mode">
            <button id="create-overview-tab-source" type="button" class="markdown-editor-tab is-active" role="tab" aria-selected="true" aria-controls="create-overview-source-panel" tabindex="0">Markdown</button>
            <button id="create-overview-tab-preview" type="button" class="markdown-editor-tab" role="tab" aria-selected="false" aria-controls="create-overview-preview" tabindex="-1">Preview</button>
          </div>
          <div id="create-overview-source-panel" role="tabpanel" aria-labelledby="create-overview-tab-source">
            <textarea id="create-company-overview" class="role-description-textarea" name="company_overview" rows="4" placeholder="Paste the full role description from the job posting." aria-describedby="create-company-overview-help" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary); resize: vertical; line-height: 1.4;"></textarea>
          </div>
          <div id="create-overview-preview" class="markdown-field-preview create-overview-preview markdown-preview" role="tabpanel" aria-labelledby="create-overview-tab-preview" tabindex="0" hidden></div>
        </div>
        <span id="create-company-overview-help" class="process-form-helper">Paste the complete role description; use Preview to check its Markdown formatting.</span>
      </div>

      <details class="process-form-disclosure">
        <summary class="process-form-disclosure-summary">
          <span class="process-form-disclosure-copy"><strong>Additional job details</strong><span>Optional salary, recruiter, company domain and keywords</span></span>
          ${icon("chevronDown", 16)}
        </summary>
        <div class="process-form-disclosure-content">
          <div class="form-grid form-grid-three">
            <div>
              <label class="meta-label" for="create-salary-type">Salary Type</label>
              <select id="create-salary-type" name="salary_type" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);">
                <option value="limited">Limited Range (Min - Max)</option>
                <option value="no_min">Up to Max (No Minimum)</option>
                <option value="no_max">From Min (No Maximum)</option>
                <option value="unknown" selected>Unknown / Undisclosed</option>
              </select>
            </div>
            <div>
              <label class="meta-label" for="create-salary-min">Minimum Salary (€)</label>
              <input id="create-salary-min" type="number" name="salary_min" placeholder="e.g. 80000" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
            </div>
            <div>
              <label class="meta-label" for="create-salary-max">Maximum Salary (€)</label>
              <input id="create-salary-max" type="number" name="salary_max" placeholder="e.g. 110000" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
            </div>
          </div>

          <div class="form-grid form-grid-three">
            <div>
              <label class="meta-label" for="create-recruiter-type">Recruiter Source</label>
              <select id="create-recruiter-type" name="recruiter_type" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);">
                <option value="none">Direct / No Recruiter</option>
                <option value="internal">Internal Recruiter / Talent</option>
                <option value="external">External Agency Recruiter</option>
              </select>
            </div>
            <div>
              <label class="meta-label" for="create-recruiter-name">Recruiter Name (Optional)</label>
              <input id="create-recruiter-name" type="text" name="recruiter_name" autocomplete="name" placeholder="e.g. Alex Rivera" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
            </div>
            <div>
              <label class="meta-label" for="create-recruiter-agency">Recruiting Agency (If External)</label>
              <input id="create-recruiter-agency" type="text" name="recruiter_agency" autocomplete="organization" placeholder="e.g. CyberTalent Search" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
            </div>
          </div>

          <div>
            <label class="meta-label" for="create-company-domain">Company Domain (For Logo)</label>
            <input id="create-company-domain" type="text" name="company_domain" placeholder="example.com" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
          </div>

          <div>
            <label class="meta-label" for="create-keyword-note">Keywords and notes</label>
            <input id="create-keyword-note" type="text" name="keyword_note" maxlength="100" placeholder="e.g. Remote EU, Seed startup, Rust+Go, async-first culture" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary); font-family: var(--font-mono);" />
            <div style="font-size: 11px; color: var(--text-muted); text-align: right; margin-top: 2px;">
              <span id="keyword-char-count">0</span> / 100 chars
            </div>
          </div>
        </div>
      </details>

      <details class="process-form-disclosure">
        <summary class="process-form-disclosure-summary">
          <span class="process-form-disclosure-copy"><strong>Interview preparation</strong><span>Optional motivations, experience, salary target and notes</span></span>
          ${icon("chevronDown", 16)}
        </summary>
        <div class="process-form-disclosure-content">
          <div>
            <label class="meta-label inline-icon-text" for="create-reasons-to-change">${icon("compass", 13)} Why I Want to Change Company (Top FAQ & Motivations)</label>
            <textarea id="create-reasons-to-change" name="reasons_to_change" rows="3" placeholder="Why do you want to change company? Key motivations, strategic rationale, growth drivers..." style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary); resize: vertical; line-height: 1.4;"></textarea>
          </div>

          <div>
            <label class="meta-label inline-icon-text" for="create-relevant-experience">${icon("user", 13)} Relevant Experience</label>
            <textarea id="create-relevant-experience" name="experience_notes" rows="3" placeholder="Experience and examples that match this role..." style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary); resize: vertical; line-height: 1.4;"></textarea>
          </div>

          <div>
            <label class="meta-label inline-icon-text" for="create-expected-salary">${icon("euro", 13)} Expected Salary for This Offer</label>
            <input id="create-expected-salary" type="text" name="expected_salary" placeholder="e.g. €85k base plus equity; flexible depending on the offer" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
          </div>

          <div>
            <label class="meta-label inline-icon-text" for="create-interview-notes">${icon("fileText", 13)} My Notes / Anything Else (Optional)</label>
            <textarea id="create-interview-notes" name="interview_notes" rows="3" placeholder="Write anything else you want to remember about this selection process..." style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary); resize: vertical; line-height: 1.4;"></textarea>
          </div>
        </div>
      </details>

      <div class="process-form-referral-choice">
        <input type="checkbox" name="is_referral" id="create-referral-cb" style="accent-color: #c084fc; width: 16px; height: 16px; cursor: pointer;" />
        <label for="create-referral-cb" class="process-form-referral-label">
          ${icon("userCheck", 13)} Direct Candidate Referral (Check if this opportunity is from a referral)
        </label>
      </div>

      <p class="process-stage-creation-note">Interview stages start empty. Add only stages confirmed by the employer, or add them as the process unfolds.</p>

      <div class="form-actions">
        <button type="button" class="btn-secondary btn-cancel">Cancel</button>
        <button type="submit" class="btn-primary">Create Selection Process</button>
      </div>
    </form>
  `;

  const form = modalEl.querySelector("#new-process-form");
  activateModal(modalEl, "#create-position-title");
  const keywordInput = form.querySelector('input[name="keyword_note"]');
  const countEl = modalEl.querySelector("#keyword-char-count");
  const closeBtn = modalEl.querySelector(".modal-close-btn");
  const cancelBtn = modalEl.querySelector(".btn-cancel");
  const overviewInput = form.querySelector("#create-company-overview");
  const overviewSource = form.querySelector("#create-overview-source-panel");
  const overviewPreview = form.querySelector("#create-overview-preview");
  const overviewTabs = [...form.querySelectorAll('[role="tablist"] .markdown-editor-tab')];

  const activateOverviewTab = (index) => {
    const isPreview = index === 1;
    overviewSource.hidden = isPreview;
    overviewPreview.hidden = !isPreview;
    if (isPreview) overviewPreview.innerHTML = renderMarkdown(overviewInput.value) || '<span class="empty-note">Nothing to preview yet.</span>';
    overviewTabs.forEach((tab, tabIndex) => {
      tab.classList.toggle("is-active", tabIndex === index);
      tab.setAttribute("aria-selected", String(tabIndex === index));
      tab.tabIndex = tabIndex === index ? 0 : -1;
    });
  };

  overviewTabs.forEach((tab, index) => {
    tab.addEventListener("click", () => activateOverviewTab(index));
    tab.addEventListener("keydown", (event) => {
      const nextIndex = event.key === "ArrowRight" ? (index + 1) % overviewTabs.length
        : event.key === "ArrowLeft" ? (index + overviewTabs.length - 1) % overviewTabs.length
          : event.key === "Home" ? 0 : event.key === "End" ? overviewTabs.length - 1 : -1;
      if (nextIndex < 0) return;
      event.preventDefault();
      activateOverviewTab(nextIndex);
      overviewTabs[nextIndex].focus();
    });
  });

  keywordInput.addEventListener("input", () => {
    countEl.textContent = keywordInput.value.length;
  });

  const closeForm = () => {
    closeWithFlip(modalEl, backdropEl, () => {
      modalEl.innerHTML = "";
      modalEl.classList.remove("new-process-modal");
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
processFilterTrigger?.addEventListener("keydown", (event) => {
  if (event.key !== "Escape" || !processFilterPanel || processFilterPanel.hidden) return;
  event.preventDefault();
  closeProcessFilterPanel(true);
});
processFilterPanel?.addEventListener("keydown", (event) => {
  if (event.key !== "Escape") return;
  event.preventDefault();
  event.stopPropagation();
  closeProcessFilterPanel(true);
});
processFilterPanel?.addEventListener("focusout", (event) => {
  if (event.relatedTarget instanceof Node && processFilterPanel.contains(event.relatedTarget)) return;
  setTimeout(() => {
    if (!processFilterPanel.contains(document.activeElement)) closeProcessFilterPanel();
  }, 0);
});

function closeProcessFilterPanel(restoreFocus = false) {
  if (!processFilterPanel) return;
  processFilterPanel.hidden = true;
  processFilterTrigger?.setAttribute("aria-expanded", "false");
  if (restoreFocus) processFilterTrigger?.focus();
}

[...processFilterArrangements, processFilterCurrency, processFilterAnyReferral, processFilterReferral, processFilterNoReferral].forEach((control) => control?.addEventListener("change", saveProcessFilters));
[processFilterPostedSalaryMin, processFilterPostedSalaryMax].forEach((control) => control?.addEventListener("change", saveProcessFilters));
processFilterExpectedSalary?.addEventListener("change", saveProcessFilters);
processFilterTechnologySearch?.addEventListener("input", renderTechnologyFilterOptions);
processFilterTechnologyOptions?.addEventListener("change", saveProcessFilters);
searchPeriodFilter?.addEventListener("change", saveProcessFilters);
searchPeriodTrigger?.addEventListener("click", () => {
  if (!searchPeriodMenu) return;
  const willOpen = searchPeriodMenu.hidden;
  searchPeriodMenu.hidden = !willOpen;
  searchPeriodTrigger.setAttribute("aria-expanded", String(willOpen));
});
searchPeriodTrigger?.addEventListener("keydown", (event) => {
  if (!searchPeriodMenu || !["ArrowDown", "ArrowUp"].includes(event.key)) return;
  event.preventDefault();
  searchPeriodMenu.hidden = false;
  searchPeriodTrigger.setAttribute("aria-expanded", "true");
  const selected = searchPeriodMenu.querySelector('[aria-checked="true"]');
  (selected || searchPeriodMenu.querySelector("button"))?.focus();
});
searchPeriodMenu?.addEventListener("click", (event) => {
  event.stopPropagation();
  if (event.target.closest("[data-manage-search-periods]")) {
    searchPeriodMenu.hidden = true;
    searchPeriodTrigger?.setAttribute("aria-expanded", "false");
    openSearchPeriodsModal();
    return;
  }
  const option = event.target.closest("[data-search-period]");
  if (!option || !searchPeriodFilter || !searchPeriodTrigger || !searchPeriodMenu) return;
  searchPeriodFilter.value = option.dataset.searchPeriod;
  processFilters.searchPeriod = option.dataset.searchPeriod;
  renderSearchPeriodOptions();
  searchPeriodTrigger.focus();
  searchPeriodMenu.hidden = true;
  searchPeriodTrigger.setAttribute("aria-expanded", "false");
  saveProcessFilters();
});
searchPeriodMenu?.addEventListener("keydown", (event) => {
  if (event.key === "Escape") {
    event.stopPropagation();
    searchPeriodMenu.hidden = true;
    searchPeriodTrigger?.setAttribute("aria-expanded", "false");
    searchPeriodTrigger?.focus();
    return;
  }
  const options = [...searchPeriodMenu.querySelectorAll('button[role="menuitemradio"], button[role="menuitem"]')];
  const index = options.indexOf(event.target);
  if (index < 0 || !options.length) return;
  let nextIndex = null;
  if (event.key === "ArrowDown") nextIndex = (index + 1) % options.length;
  if (event.key === "ArrowUp") nextIndex = (index - 1 + options.length) % options.length;
  if (event.key === "Home") nextIndex = 0;
  if (event.key === "End") nextIndex = options.length - 1;
  if (nextIndex === null) return;
  event.preventDefault();
  options[nextIndex]?.focus();
});
searchPeriodSelectWrap?.addEventListener("focusout", (event) => {
  if (event.relatedTarget instanceof Node && searchPeriodSelectWrap.contains(event.relatedTarget)) return;
  if (searchPeriodMenu) searchPeriodMenu.hidden = true;
  searchPeriodTrigger?.setAttribute("aria-expanded", "false");
});

processFilterChips?.addEventListener("click", (event) => {
  const chip = event.target.closest("[data-remove-filter]");
  if (!chip) return;
  const keyboardActivated = event.detail === 0;
  const chipIndex = [...processFilterChips.querySelectorAll("[data-remove-filter]")].indexOf(chip);
  clearProcessFilterCategory(chip.dataset.removeFilter, chip.dataset.filterValue);
  if (!keyboardActivated) return;
  const remainingChips = [...processFilterChips.querySelectorAll("[data-remove-filter]")];
  const nextChip = remainingChips[Math.min(chipIndex, remainingChips.length - 1)];
  (nextChip || (processFilterPanelFooter && !processFilterPanelFooter.hidden ? processFilterClear : processFilterTrigger))?.focus();
});

function clearProcessFilterCategory(category, value = "") {
  switch (category) {
    case "arrangement": clearArrangementFilter(value); break;
    case "searchPeriod":
      processFilters.searchPeriod = "all";
      if (searchPeriodFilter) searchPeriodFilter.value = "all";
      renderSearchPeriodOptions();
      break;
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

processFilterClear?.addEventListener("click", (event) => {
  processFilterArrangements.forEach((control) => { control.checked = false; });
  if (processFilterExpectedSalary) processFilterExpectedSalary.value = "";
  if (processFilterPostedSalaryMin) processFilterPostedSalaryMin.value = "";
  if (processFilterPostedSalaryMax) processFilterPostedSalaryMax.value = "";
  if (processFilterCurrency) processFilterCurrency.value = "";
  clearReferralFilter();
  if (processFilterTechnologySearch) processFilterTechnologySearch.value = "";
  processFilters.technologies = [];
  renderTechnologyFilterOptions();
  saveProcessFilters();
  closeProcessFilterPanel(true);
});

function resetSearchAndFilters() {
  clearTimeout(searchDebounceTimer);
  currentSearch = "";
  if (searchInput) searchInput.value = "";
  processFilters = {
    arrangements: [], searchPeriod: "all", expectedSalaryQuery: "",
    postedSalaryMin: null, postedSalaryMax: null, currency: "", referral: "", technologies: [],
  };
  if (searchPeriodFilter) searchPeriodFilter.value = "all";
  initializeProcessFilterControls();
  renderTechnologyFilterOptions();
  renderSearchPeriodOptions();
  searchInput?.focus();
  saveProcessFilters();
}

document.addEventListener("click", (event) => {
  if (!searchPeriodMenu || searchPeriodMenu.hidden || !(event.target instanceof Element) || event.target.closest(".search-period-select-wrap")) return;
  searchPeriodMenu.hidden = true;
  searchPeriodTrigger?.setAttribute("aria-expanded", "false");
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
  if (event.key === "Escape") {
    event.preventDefault();
    event.stopPropagation();
    closeFilterMenu(true);
    return;
  }
  const options = [...filterTabs];
  const index = options.indexOf(document.activeElement);
  if (index < 0) return;
  if (event.key === "ArrowDown" || event.key === "ArrowUp") {
    event.preventDefault();
    const direction = event.key === "ArrowDown" ? 1 : -1;
    options[(index + direction + options.length) % options.length]?.focus();
  }
});
filterMenu?.addEventListener("focusout", (event) => {
  if (event.relatedTarget instanceof Element && event.relatedTarget.closest(".filter-tabs")) return;
  closeFilterMenu();
});

document.addEventListener("click", (event) => {
  if (!(event.target instanceof Element)) return;
  if (!event.target.closest(".filter-tabs")) closeFilterMenu();
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
appLoadRetryButton?.addEventListener("click", () => {
  appLoadRetryButton.disabled = true;
  appLoadRetryButton.textContent = "Retrying…";
  void refreshApp();
});

newProcessButtons.forEach((button) => button.addEventListener("click", openNewProcessModal));
dataManagementButton?.addEventListener("click", openDataModal);
technologyLibraryButton?.addEventListener("click", openTechnologyLibraryModal);
window.addEventListener("war-room:open-cv-library", openCvLibraryModal);
window.addEventListener("war-room:open-technology-library", openTechnologyLibraryModal);
menuToggle?.addEventListener("click", () => {
  const isOpen = menuToggle.getAttribute("aria-expanded") === "true";
  menuToggle.setAttribute("aria-expanded", String(!isOpen));
  menuToggle.setAttribute("aria-label", isOpen ? "Open navigation menu" : "Close navigation menu");
  headerControls?.classList.toggle("is-open", !isOpen);
});
backdropEl.addEventListener("click", (e) => {
  if (e.target === backdropEl) {
    if (modalEl.classList.contains("data-transfer-modal")) closeTransferModal();
    else closeModal();
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
