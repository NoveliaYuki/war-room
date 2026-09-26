/**
 * @fileoverview Main Client Application Entry Point.
 * Coordinates global state, filter tabs, instant search, and keyboard shortcuts.
 */

import { api } from "./api.js";
import { renderCardGrid } from "./components/cardGrid.js";
import { renderScheduleView } from "./components/scheduleView.js";
import { closeWithFlip, cancelPendingFlipClose } from "./flip.js";
import { icon } from "./icons.js";
import { showToast } from "./utils/toast.js";

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
const validFilters = new Set(["ongoing", "accepted", "rejected", "all", "schedule"]);

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
let currentSearch = "";
let searchDebounceTimer = null;

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
const filterTabs = document.querySelectorAll(".filter-tab");
const btnNewProcess = document.querySelector("#btn-new-process");
const menuToggle = document.querySelector("#btn-menu-toggle");
const headerControls = document.querySelector("#header-controls");

/** Closes the compact navigation menu after choosing an action. */
function closeMobileMenu() {
  if (!menuToggle || !headerControls) return;
  menuToggle.setAttribute("aria-expanded", "false");
  menuToggle.setAttribute("aria-label", "Open navigation menu");
  headerControls.classList.remove("is-open");
}

/**
 * Updates filter counters and re-renders cards.
 */
async function refreshApp() {
  try {
    const counts = await api.getJobCounts();
    document.querySelector("#count-all").textContent = counts.all;
    document.querySelector("#count-ongoing").textContent = counts.ongoing;
    document.querySelector("#count-accepted").textContent = counts.accepted;
    document.querySelector("#count-rejected").textContent = counts.rejected;

    const meetings = await api.getMeetings();
    const countMeetingsEl = document.querySelector("#count-meetings");
    if (countMeetingsEl) {
      countMeetingsEl.textContent = meetings.length;
    }

    if (currentFilter === "schedule") {
      cardGridEl.classList.add("schedule-mode");
      await renderScheduleView(cardGridEl, modalEl, backdropEl, refreshApp);
      return;
    }

    cardGridEl.classList.remove("schedule-mode");
    const jobs = await api.getJobs(currentFilter, currentSearch);
    renderCardGrid(cardGridEl, jobs, modalEl, backdropEl, refreshApp);
  } catch (err) {
    console.error("Failed to load jobs data:", err);
  }
}

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
  try {
    window.localStorage.setItem(FILTER_STORAGE_KEY, filter);
  } catch {
    // Keep navigation working when browser storage is unavailable.
  }
  filterTabs.forEach((tab) => {
    tab.classList.toggle("active", tab.getAttribute("data-filter") === filter);
  });
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
  const filters = { "1": "ongoing", "2": "accepted", "3": "rejected", "4": "all", "5": "schedule" };
  return filters[event.key] || null;
}

/** Closes the modal and clears its content. */
function closeModal() {
  closeWithFlip(modalEl, backdropEl, () => { modalEl.innerHTML = ""; });
}

/** Applies one global keyboard shortcut to the application. */
function handleGlobalKeydown(event) {
  const typing = isTypingTarget();
  if (event.key === "Escape" && backdropEl.classList.contains("active")) {
    closeModal();
    return;
  }
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
  cancelPendingFlipClose();
  backdropEl.classList.add("active");
  modalEl.innerHTML = `
    <div class="modal-header">
      <h2 class="modal-title inline-icon-text">${icon("plus", 16)} Add New Selection Process</h2>
      <button class="modal-close-btn">${icon("close", 14)}</button>
    </div>
    <form id="new-process-form" style="padding: 24px 28px; display: flex; flex-direction: column; gap: 16px; overflow-y: auto;">
      <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 16px;">
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

      <div style="display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 16px;">
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

      <div style="display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 16px;">
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

      <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 16px;">
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

      <div style="display: flex; justify-content: flex-end; gap: 10px; margin-top: 12px;">
        <button type="button" class="btn-secondary btn-cancel">Cancel</button>
        <button type="submit" class="btn-primary">Create Selection Process</button>
      </div>
    </form>
  `;

  const form = modalEl.querySelector("#new-process-form");
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
  });
});

searchInput.addEventListener("input", () => {
  clearTimeout(searchDebounceTimer);
  searchDebounceTimer = setTimeout(() => {
    currentSearch = searchInput.value;
    refreshApp();
  }, 200);
});

btnNewProcess.addEventListener("click", openNewProcessModal);
menuToggle?.addEventListener("click", () => {
  const isOpen = menuToggle.getAttribute("aria-expanded") === "true";
  menuToggle.setAttribute("aria-expanded", String(!isOpen));
  menuToggle.setAttribute("aria-label", isOpen ? "Open navigation menu" : "Close navigation menu");
  headerControls?.classList.toggle("is-open", !isOpen);
});
searchInput.addEventListener("focus", closeMobileMenu);

backdropEl.addEventListener("click", (e) => {
  if (e.target === backdropEl) {
    closeWithFlip(modalEl, backdropEl, () => {
      modalEl.innerHTML = "";
    });
  }
});

window.addEventListener("keydown", handleGlobalKeydown);

filterTabs.forEach((tab) => {
  tab.classList.toggle("active", tab.getAttribute("data-filter") === currentFilter);
});

document.body.dataset.appReady = "true";
refreshApp();
