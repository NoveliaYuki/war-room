/**
 * @fileoverview Main Client Application Entry Point.
 * Coordinates global state, filter tabs, instant search, and keyboard shortcuts.
 */

import { api } from "./api.js";
import { LANGUAGE_CHANGE_EVENT, getLocale, setTranslatedAttribute, setTranslatedText, t } from "./i18n.js";
import { renderCardGrid } from "./components/cardGrid.js";
import { renderScheduleView } from "./components/scheduleView.js";
import { renderScheduleLoadingState } from "./components/loadingViews.js";
import { captureLoadingView, revealLoadedContent } from "./utils/loadingTransition.js";
import { openDetailModal } from "./components/detailModal.js";
import { openCvLibrary } from "./components/cvLibrary.js";
import { openTechnologyLibrary } from "./components/technologyLibrary.js";
import { bindJobImport, renderJobImportStatus, renderProcessEntry, renderTechnologySuggestions } from "./components/jobImport.js";
import { closeWithFlip, cancelPendingFlipClose } from "./flip.js";
import { icon } from "./icons.js";
import { activateModal, restoreModalFocus, setModalBackgroundScrollPosition, trapModalTab } from "./modalA11y.js";
import { applyLogoFit } from "./utils/logoFit.js";
import { showToast } from "./utils/toast.js";
import { showBackupProgress } from "./utils/backupProgress.js";
import { confirmAction } from "./utils/confirmDialog.js";
import { escapeAttr, escapeHtml } from "./utils/sanitize.js";
import { renderMarkdown } from "./utils/markdown.js";
import { filterJobs } from "./utils/processFilters.js";
import { arrangementLabel, statusLabel } from "./utils/labels.js";
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

/** Fits a company logo to its tile once its pixels are known. */
function handleAvatarImageLoad(event) {
  const image = event.target;
  if (image instanceof HTMLImageElement && image.hasAttribute("data-avatar-fallback")) applyLogoFit(image);
}

document.addEventListener("error", handleAvatarImageError, true);
document.addEventListener("load", handleAvatarImageLoad, true);

const FILTER_STORAGE_KEY = "war-room.active-filter";
const OPEN_JOB_STORAGE_KEY = "war-room.open-job";
const SCROLL_POSITION_STORAGE_KEY = "war-room.window-scroll";
const STATUS_FILTER_STORAGE_KEY = "war-room.status-filter";
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

function getSavedStatusFilter() {
  try {
    const saved = window.localStorage.getItem(STATUS_FILTER_STORAGE_KEY);
    return ["waiting", "ongoing", "accepted", "rejected", "all"].includes(saved) ? saved : "ongoing";
  } catch {
    return "ongoing";
  }
}

let currentFilter = getSavedFilter();
let currentStatusFilter = currentFilter === "schedule" ? getSavedStatusFilter() : currentFilter;
const viewScrollPositions = { board: 0, schedule: 0 };
let restoringPageState = true;
let appRefreshRequestId = 0;
if (currentFilter !== "schedule") {
  try { window.localStorage.setItem(STATUS_FILTER_STORAGE_KEY, currentStatusFilter); } catch { /* Keep the restored status filter for this page. */ }
}
let currentSearch = "";
let searchDebounceTimer = null;
let processFilters = loadProcessFilters();
let availableTechnologies = [];
let technologiesLoaded = false;
let jobImportEnabled = true;
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
const statusFilterControls = document.querySelectorAll('input[name="process-status-filter"]');
const boardButton = document.querySelector("#tab-board");
const scheduleButton = document.querySelector("#tab-schedule");
const newProcessButtons = document.querySelectorAll(".new-process-trigger");
const menuToggle = document.querySelector("#btn-menu-toggle");
const headerControls = document.querySelector("#header-controls");
const dataManagementButton = document.querySelector("#btn-data-management-menu");

const technologyLibraryButton = document.querySelector("#btn-technology-library");
const themeToggle = document.querySelector("#btn-theme-toggle");
const accountMenuButton = document.querySelector("#btn-account-menu");
const accountMenuPanel = document.querySelector("#account-menu-panel");
const accountAvatarImg = document.querySelector("#account-avatar-img");
const accountAvatarFallback = document.querySelector("#account-avatar-fallback");
const accountMenuLabel = document.querySelector("#account-menu-label");
const accountIdentityBlock = document.querySelector("#account-identity");
const accountIdentityAvatarImg = document.querySelector("#account-identity-avatar-img");
const accountIdentityAvatarFallback = document.querySelector("#account-identity-avatar-fallback");
const accountIdentityName = document.querySelector("#account-identity-name");
const accountIdentityEmail = document.querySelector("#account-identity-email");
const changeAvatarButton = document.querySelector("#btn-change-avatar");
const removeAvatarButton = document.querySelector("#btn-remove-avatar");
const avatarFileInput = document.querySelector("#avatar-upload-file");
const signOutButton = document.querySelector("#btn-sign-out");
const googleAccountSection = document.querySelector("#google-account-section");
const themeCurrentLabel = document.querySelector("#theme-current-label");
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
const mobileProcessFilterChips = document.querySelector("[data-mobile-filter-chips]");
const processToolbarActiveState = document.querySelector("#process-toolbar-active-state");
const processSortTrigger = document.querySelector("#process-sort-trigger");
const processSortCurrent = document.querySelector("#process-sort-current");
const processSortMenu = document.querySelector("#process-sort-menu");
const processSortOptions = document.querySelectorAll("[data-sort-mode]");
const processFilterAnyReferral = document.querySelector("#process-filter-any-referral");
let managementReturnJobId = "";

/** Stores the page scroll position so a reload returns to the same content. */
function saveWindowScrollPosition() {
  if (restoringPageState || backdropEl?.classList.contains("active")) return;
  try { window.localStorage.setItem(SCROLL_POSITION_STORAGE_KEY, String(window.scrollY)); } catch { /* Keep the current page usable if storage is unavailable. */ }
}

/** Restores a valid saved window scroll position after the selected view renders. */
function getSavedWindowScrollPosition() {
  try {
    const position = Number(window.localStorage.getItem(SCROLL_POSITION_STORAGE_KEY));
    return Number.isFinite(position) && position >= 0 ? position : 0;
  } catch {
    return 0;
  }
}

/** Remembers which job detail is open across reloads. */
function saveOpenJob(jobId) {
  try {
    if (jobId) window.localStorage.setItem(OPEN_JOB_STORAGE_KEY, String(jobId));
    else window.localStorage.removeItem(OPEN_JOB_STORAGE_KEY);
  } catch { /* Keep job details usable if storage is unavailable. */ }
}

function getSavedOpenJob() {
  try { return window.localStorage.getItem(OPEN_JOB_STORAGE_KEY) || ""; } catch { return ""; }
}

/** Returns the selected theme, falling back to the current system preference. */
function getActiveTheme() {
  const selected = document.documentElement.dataset.theme;
  if (selected === "dark" || selected === "light") return selected;
  return window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
}

/** Updates the theme toggle's accessible label for the next action. */
function updateThemeToggle() {
  if (!themeToggle) return;
  const labelKey = getActiveTheme() === "dark" ? "theme.switchToLight" : "theme.switchToDark";
  setTranslatedAttribute(themeToggle, "aria-label", labelKey);
  setTranslatedAttribute(themeToggle, "title", labelKey);
  if (themeCurrentLabel) setTranslatedText(themeCurrentLabel, getActiveTheme() === "dark" ? "theme.currentDark" : "theme.currentLight");
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

const isDemoMode = document.body?.dataset?.demo === "true";
let googleStatus = null;
let googleBackupInFlight = false;

if (signOutButton) signOutButton.hidden = isDemoMode;
signOutButton?.addEventListener("click", () => void handleSignOut());

/** Signs out of the current device and reloads so boot.js re-shows the sign-in gate. */
async function handleSignOut() {
  try {
    await api.signOut();
  } catch { /* Reload regardless; the goal is to drop the local session either way. */ }
  window.location.reload();
}

/** Returns a short display name for the header pill: first name, email local-part, or "" when unknown. */
function simplifiedAccountName(accountInfo) {
  const name = accountInfo?.name?.trim();
  if (name) return name.split(/\s+/)[0];
  const email = accountInfo?.email?.trim();
  if (email) return email.split("@")[0];
  return "";
}

/** Shows a person's name, or a translated fallback that follows language changes. */
function setAccountName(element, name) {
  if (!element) return;
  if (!name) {
    setTranslatedText(element, "account.fallbackName");
    return;
  }
  delete element.dataset.i18n;
  element.textContent = name;
}

/** Fetches the signed-in account once and applies its name/photo everywhere the account menu shows them. */
async function loadAccountInfo() {
  if (isDemoMode) return;
  let accountInfo = null;
  try {
    accountInfo = await api.getAuthMe();
  } catch { accountInfo = null; }
  jobImportEnabled = accountInfo?.job_import_enabled !== false;
  renderAccountAvatar(accountInfo);
  renderAccountIdentity(accountInfo);
}

/** Shows the account's custom or Google photo on the small header pill, and sets the pill's name label. */
function renderAccountAvatar(accountInfo) {
  setAccountName(accountMenuLabel, simplifiedAccountName(accountInfo));
  applyAvatarPhoto(accountAvatarImg, accountAvatarFallback, accountInfo);
}

/** Populates the full-detail identity block at the top of the opened account panel (also the only identity UI visible on mobile). */
function renderAccountIdentity(accountInfo) {
  if (!accountIdentityBlock) return;
  if (!accountInfo) { accountIdentityBlock.hidden = true; return; }
  accountIdentityBlock.hidden = false;
  setAccountName(accountIdentityName, accountInfo.name?.trim() || accountInfo.email || "");
  if (accountIdentityEmail) accountIdentityEmail.textContent = accountInfo.email || "";
  applyAvatarPhoto(accountIdentityAvatarImg, accountIdentityAvatarFallback, accountInfo);
  if (removeAvatarButton) removeAvatarButton.hidden = !accountInfo.avatar_url;
}

/** Shared avatar rendering: a custom avatar wins over the Google photo, which wins over the fallback initial. */
function applyAvatarPhoto(imgEl, fallbackEl, accountInfo) {
  if (fallbackEl) fallbackEl.textContent = accountInfo?.email ? accountInfo.email.charAt(0).toUpperCase() : "?";
  const photoUrl = accountInfo?.avatar_url || accountInfo?.picture_url || "";
  if (!imgEl) return;
  if (!photoUrl) {
    imgEl.hidden = true;
    if (fallbackEl) fallbackEl.hidden = false;
    return;
  }
  imgEl.onerror = () => {
    if (accountInfo.avatar_url && accountInfo.picture_url && imgEl.src !== new URL(accountInfo.picture_url, window.location.href).href) {
      imgEl.src = accountInfo.picture_url;
      return;
    }
    imgEl.hidden = true;
    if (fallbackEl) fallbackEl.hidden = false;
  };
  imgEl.src = photoUrl;
  imgEl.hidden = false;
  if (fallbackEl) fallbackEl.hidden = true;
}

/** Returns to the sign-in gate when the session expires while the page is open. */
function reloadOnExpiredSession() {
  const nativeFetch = window.fetch.bind(window);
  window.fetch = async (...args) => {
    const response = await nativeFetch(...args);
    if (response.status === 401) window.location.reload();
    return response;
  };
}

if (!isDemoMode) reloadOnExpiredSession();
void loadAccountInfo();

changeAvatarButton?.addEventListener("click", () => avatarFileInput?.click());
avatarFileInput?.addEventListener("change", async () => {
  const file = avatarFileInput.files?.[0];
  if (!file) return;
  try {
    const user = await api.uploadAvatar(file);
    renderAccountAvatar(user);
    renderAccountIdentity(user);
    showToast(t("account.photoUpdated"), "success");
  } catch (error) {
    showToast(error.message || t("account.photoUpdateFailed"), "error");
  } finally {
    avatarFileInput.value = "";
  }
});
removeAvatarButton?.addEventListener("click", async () => {
  try {
    const user = await api.removeAvatar();
    renderAccountAvatar(user);
    renderAccountIdentity(user);
    showToast(t("account.photoReverted"), "success");
  } catch (error) {
    showToast(error.message || t("account.photoRemoveFailed"), "error");
  }
});

/** On narrow viewports the account panel is flattened into the hamburger menu and always shown. */
function isMobileAccountLayout() {
  return window.matchMedia?.("(max-width: 640px)").matches ?? false;
}

function setAccountMenuOpen(open) {
  if (!accountMenuPanel || !accountMenuButton) return;
  if (isMobileAccountLayout()) {
    accountMenuPanel.hidden = false;
    accountMenuButton.setAttribute("aria-expanded", "true");
    return;
  }
  accountMenuPanel.hidden = !open;
  accountMenuButton.setAttribute("aria-expanded", String(open));
}

setAccountMenuOpen(false);
window.addEventListener("resize", () => setAccountMenuOpen(accountMenuPanel?.hidden === false));

accountMenuButton?.addEventListener("click", () => {
  const open = accountMenuPanel?.hidden === false;
  setAccountMenuOpen(!open);
  if (!open) void refreshGoogleStatus();
});
document.addEventListener("click", (event) => {
  if (!accountMenuPanel || accountMenuPanel.hidden) return;
  if (!event.target.closest?.(".account-menu")) setAccountMenuOpen(false);
});
document.addEventListener("keydown", (event) => {
  if (event.key === "Escape") setAccountMenuOpen(false);
});
document.querySelector("#btn-data-management-menu")?.addEventListener("click", () => {
  setAccountMenuOpen(false);
  openDataModal();
});

/** Renders the Google account section of the account menu. */
function renderGoogleAccount() {
  if (!googleAccountSection) return;
  if (isDemoMode) {
    googleAccountSection.innerHTML = `<p class="account-note">${escapeHtml(t("google.demoUnavailable"))}</p>`;
    return;
  }
  if (!googleStatus) {
    googleAccountSection.innerHTML = `<p class="account-note">${escapeHtml(t("google.loadingStatus"))}</p>`;
    return;
  }
  if (!googleStatus.configured) {
    googleAccountSection.innerHTML = `<p class="account-heading">${icon("cloud", 15)} ${escapeHtml(t("google.heading"))}</p>
      <p class="account-note">${escapeHtml(t("google.notConfigured"))}</p>`;
    return;
  }
  if (!googleStatus.connected) {
    googleAccountSection.innerHTML = `<p class="account-heading">${icon("cloud", 15)} ${escapeHtml(t("google.heading"))}</p>
      <p class="account-note">${escapeHtml(t("google.autoBackupNote"))}</p>
      <a class="btn-primary account-connect" href="/api/google/connect">${escapeHtml(t("google.connectAccount"))}</a>`;
    return;
  }
  renderConnectedGoogleAccount();
}

/** Renders the connected Google account with folder and backup actions. */
function renderConnectedGoogleAccount() {
  const folder = googleStatus.folder_name || t("google.rootFolder");
  const last = googleStatus.last_backup_at ? t("google.lastBackup", { date: new Date(googleStatus.last_backup_at).toLocaleString(getLocale()) }) : t("google.noBackup");
  const failed = googleStatus.last_status === "error" ? `<p class="account-note account-error">${escapeHtml(t("google.lastBackupFailed"))}</p>` : "";
  const cleanupPending = googleStatus.last_status === "cleanup_pending" ? `<p class="account-note">${escapeHtml(t("google.cleanupPending"))}</p>` : "";
  googleAccountSection.innerHTML = `<div class="account-status-card">
      <p class="account-heading">${icon("cloud", 15)} ${escapeHtml(t("google.heading"))}</p>
      <p class="account-note">${icon("folder", 13)} ${escapeHtml(t("google.folder", { folder }))}</p>
      <p class="account-note">${escapeHtml(last)}</p>${failed}${cleanupPending}
    </div>
    <div class="account-actions">
      <button class="account-menu-item" id="google-choose-folder" type="button">${icon("folder", 16)}<span>${escapeHtml(t("google.chooseFolder"))}</span></button>
      <button class="account-menu-item" id="google-backup-now" type="button">${icon("upload", 16)}<span>${escapeHtml(t("google.backupNow"))}</span></button>
    </div>
    <p id="google-folder-status" class="account-note account-error" role="status" aria-live="polite" hidden></p>`;
  googleAccountSection.querySelector("#google-choose-folder")?.addEventListener("click", (event) => {
    void openFolderPicker(event.currentTarget, googleAccountSection.querySelector("#google-folder-status"));
  });
  googleAccountSection.querySelector("#google-backup-now")?.addEventListener("click", () => {
    setAccountMenuOpen(false);
    void runGoogleBackupNow();
  });
}

async function refreshGoogleStatus() {
  if (isDemoMode) { renderGoogleAccount(); return; }
  try {
    googleStatus = await api.getGoogleStatus();
  } catch { googleStatus = null; }
  renderGoogleAccount();
}

async function openFolderPicker(button, message) {
  if (!googleStatus?.picker_api_key) {
    setFolderPickerMessage(message, t("google.pickerNotConfigured"));
    showToast(t("google.pickerKeyRequired"), "error");
    return;
  }
  if (!googleStatus?.picker_app_id) {
    setFolderPickerMessage(message, t("google.pickerProjectMissing"));
    return;
  }
  setFolderPickerMessage(message, "");
  await showGoogleDrivePicker(button, message);
}

function setFolderPickerMessage(element, text) {
  if (!element) return;
  element.textContent = text;
  element.hidden = !text;
}

let pickerScriptPromise;
function loadGooglePicker() {
  if (window.gapi?.load) return Promise.resolve();
  if (!pickerScriptPromise) pickerScriptPromise = new Promise((resolve, reject) => {
    const script = document.createElement("script");
    script.src = "https://apis.google.com/js/api.js";
    script.async = true;
    script.onload = resolve;
    script.onerror = () => reject(new Error(t("google.pickerLoadFailed")));
    document.head.append(script);
  });
  return pickerScriptPromise;
}

function handleGooglePickerResult(data) {
  if (data.action !== window.google.picker.Action.PICKED) return;
  const folder = data.docs?.[0];
  if (folder?.id) void chooseGoogleFolder(folder.id, folder.name || t("google.defaultFolderName"));
}

async function showGoogleDrivePicker(button, message) {
  if (!button || !googleStatus?.picker_api_key || !googleStatus?.picker_app_id) return;
  button.disabled = true;
  setFolderPickerMessage(message, t("google.openingPicker"));
  try {
    await loadGooglePicker();
    await new Promise((resolve, reject) => window.gapi.load("picker", { callback: resolve, onerror: reject }));
    const { access_token: accessToken } = await api.getGooglePickerToken();
    const view = new window.google.picker.DocsView(window.google.picker.ViewId.FOLDERS)
      .setIncludeFolders(true)
      .setSelectFolderEnabled(true)
      .setMimeTypes("application/vnd.google-apps.folder");
    const picker = new window.google.picker.PickerBuilder()
      .addView(view)
      .setOAuthToken(accessToken)
      .setDeveloperKey(googleStatus.picker_api_key)
      .setAppId(googleStatus.picker_app_id)
      .setOrigin(window.location.origin)
      .setCallback(handleGooglePickerResult)
      .build();
    picker.setVisible(true);
    setFolderPickerMessage(message, "");
  } catch (err) {
    setFolderPickerMessage(message, err?.message || t("google.pickerOpenFailed"));
  } finally {
    button.disabled = false;
  }
}

async function chooseGoogleFolder(id, name) {
  try {
    googleStatus = await api.setGoogleFolder(id || "", name || "");
    renderGoogleAccount();
    renderGoogleBackupOption();
    showToast(id ? t("google.folderSet", { name }) : t("google.folderRootSet"), "success");
  } catch (err) {
    showToast(err.message || t("google.folderSaveFailed"), "error");
  }
}

async function runGoogleBackupNow() {
  if (googleBackupInFlight) return;
  googleBackupInFlight = true;
  const progress = showBackupProgress(t("google.backingUp"));
  try {
    googleStatus = await api.backupNowGoogle();
    renderGoogleAccount();
    const status = modalEl.querySelector("#google-drive-backup-status");
    if (status) status.textContent = t("google.backupUploadedStatus");
    progress.success(t("google.backupUploaded"));
  } catch (err) {
    const status = modalEl.querySelector("#google-drive-backup-status");
    if (status) status.textContent = err.message || t("google.backupFailed");
    progress.error(err.message || t("google.backupFailed"));
    void refreshGoogleStatus();
  } finally {
    googleBackupInFlight = false;
  }
}

function renderGoogleBackupOption() {
  const option = modalEl.querySelector("#google-drive-backup-option");
  if (!option) return;
  if (isDemoMode) {
    option.innerHTML = `<p>${escapeHtml(t("google.dataDemoUnavailable"))}</p>`;
    return;
  }
  if (!googleStatus) {
    option.innerHTML = `<p>${escapeHtml(t("google.checkingConnection"))}</p>`;
    return;
  }
  if (!googleStatus.configured) {
    option.innerHTML = `<p>${escapeHtml(t("google.dataNotConfigured"))}</p>`;
    return;
  }
  if (!googleStatus.connected) {
    option.innerHTML = `<p>${escapeHtml(t("google.signInToUpload"))}</p><a class="btn-primary" id="btn-google-drive-backup" href="/api/google/connect">${escapeHtml(t("google.connectDrive"))}</a>`;
    return;
  }
  const folder = escapeHtml(googleStatus.folder_name || t("google.rootFolder"));
  option.innerHTML = `<p>${escapeHtml(t("google.backupFolder"))} <strong id="google-drive-backup-folder">${folder}</strong></p>
    <button class="btn-secondary" id="btn-google-drive-choose-folder" type="button">${escapeHtml(t("google.chooseDriveFolder"))}</button>
    <div class="data-transfer-status" id="google-drive-folder-status" role="status" aria-live="polite"></div>
    <p>${escapeHtml(t("google.uploadNowNote"))}</p>
    <button class="btn-primary" id="btn-google-drive-backup" type="button">${escapeHtml(t("google.backUpToDrive"))}</button>
    <div class="data-transfer-status" id="google-drive-backup-status" role="status" aria-live="polite"></div>
    <p>${escapeHtml(t("google.revokeNote"))}</p>
    <button class="btn-danger" id="btn-google-drive-revoke" type="button">${escapeHtml(t("google.revoke"))}</button>`;
  option.querySelector("#btn-google-drive-choose-folder").addEventListener("click", (event) => {
    void openFolderPicker(event.currentTarget, option.querySelector("#google-drive-folder-status"));
  });
  option.querySelector("#btn-google-drive-backup").addEventListener("click", () => {
    const button = option.querySelector("#btn-google-drive-backup");
    button.disabled = true;
    button.textContent = t("common.uploading");
    void runGoogleBackupNow().finally(() => {
      if (!button.isConnected) return;
      button.disabled = false;
      button.textContent = t("google.backUpToDrive");
    });
  });
  option.querySelector("#btn-google-drive-revoke").addEventListener("click", () => void revokeGoogleDriveAccess());
}

async function refreshDataGoogleStatus() {
  renderGoogleBackupOption();
  if (isDemoMode) return;
  try {
    googleStatus = await api.getGoogleStatus();
  } catch {
    googleStatus = null;
    const option = modalEl.querySelector("#google-drive-backup-option");
    if (option) option.innerHTML = `<p>${escapeHtml(t("google.statusCheckFailed"))}</p>`;
    return;
  }
  renderGoogleBackupOption();
}

/** Revokes the Google Drive grant; this also signs out every session for the account. */
async function revokeGoogleDriveAccess() {
  const confirmed = await confirmAction({
    title: t("google.revokeTitle"),
    message: t("google.revokeMessage"),
    confirmLabel: t("google.revokeConfirm"),
    danger: true,
  });
  if (!confirmed) return;
  try {
    await api.revokeGoogleDrive();
    window.location.reload();
  } catch (err) {
    showToast(err.message || t("google.revokeFailed"), "error");
  }
}

(function handleGoogleReturn() {
  const params = new URLSearchParams(window.location.search);
  const result = params.get("google");
  if (!result) { void refreshGoogleStatus(); return; }
  params.delete("google");
  const rest = params.toString();
  window.history.replaceState(null, "", window.location.pathname + (rest ? `?${rest}` : "") + window.location.hash);
  showToast(result === "connected" ? t("google.connected") : t("google.signInFailed"), result === "connected" ? "success" : "error");
  void refreshGoogleStatus();
})();

/** Closes the compact navigation menu after choosing an action. */
function closeMobileMenu() {
  if (!menuToggle || !headerControls) return;
  menuToggle.setAttribute("aria-expanded", "false");
  setTranslatedAttribute(menuToggle, "aria-label", "header.openMenu");
  headerControls.classList.remove("is-open");
}

/** Updates the selected status filter and top-level board view. */
function updateFilterSummary() {
  statusFilterControls.forEach((control) => {
    control.checked = control.value === "all"
      ? !processFilters.statuses.length && currentFilter === "all"
      : processFilters.statuses.length
        ? processFilters.statuses.includes(control.value)
        : control.value === currentStatusFilter;
  });
  boardButton?.classList.toggle("active", currentFilter !== "schedule");
  boardButton?.setAttribute("aria-pressed", String(currentFilter !== "schedule"));
  scheduleButton?.classList.toggle("active", currentFilter === "schedule");
  scheduleButton?.setAttribute("aria-pressed", String(currentFilter === "schedule"));
}

/**
 * Updates filter counters and re-renders cards.
 */
async function refreshApp({ restoreScrollY } = {}) {
  const requestId = ++appRefreshRequestId;
  setViewLoading(false);
  const loadingTimer = window.setTimeout(() => {
    if (requestId === appRefreshRequestId && !restoringPageState) setViewLoading(true);
  }, 300);
  try {
    if (restoringPageState && currentFilter === "schedule") renderScheduleLoadingState(cardGridEl);
    const boardData = await refreshBoardCounts();
    if (requestId !== appRefreshRequestId) return;
    await renderSelectedBoardView(boardData, requestId);
    if (requestId !== appRefreshRequestId) return;
    if (Number.isFinite(restoreScrollY)) window.scrollTo(0, restoreScrollY);
    clearAppLoadError();
  } catch (err) {
    if (requestId !== appRefreshRequestId) return;
    console.error("Failed to load jobs data:", err);
    showAppLoadError();
  } finally {
    window.clearTimeout(loadingTimer);
    if (requestId === appRefreshRequestId) setViewLoading(false);
  }
}

/** Shows a quiet status only when loading takes long enough to be noticeable. */
function setViewLoading(visible) {
  const status = document.querySelector("#view-loading-status");
  if (status) status.hidden = !visible;
}

async function refreshBoardCounts() {
  const allPeriodCounts = await api.getJobCounts();
  const meetings = await api.getMeetings();
  const countMeetingsEl = document.querySelector("#count-meetings");
  const today = new Date();
  const todayKey = `${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, "0")}-${String(today.getDate()).padStart(2, "0")}`;
  const ongoingMeetings = meetings.filter((meeting) => !meeting.meeting_date || meeting.meeting_date >= todayKey);
  if (countMeetingsEl) countMeetingsEl.textContent = ongoingMeetings.length;
  updateFilterSummary();
  updateSortControl();
  await loadAvailableTechnologies();
  searchPeriods = await loadSearchPeriods();
  renderSearchPeriodOptions();
  const scopedCounts = await getCountsForSelectedPeriod(allPeriodCounts);
  updateBoardCountLabels(scopedCounts);
  updateFilterSummary();
  updateProcessFilterSummary();
  return { allPeriodCounts, scopedCounts, meetings };
}

/** Loads optional search period data without blocking the main process board. */
async function loadSearchPeriods() {
  try {
    const getSearchPeriods = api.getSearchPeriods;
    return typeof getSearchPeriods === "function" ? await getSearchPeriods() : [];
  } catch (error) {
    console.warn("Search periods are unavailable:", error);
    return [];
  }
}

function updateBoardCountLabels(counts) {
  document.querySelector("#count-all").textContent = counts.all;
  document.querySelector("#count-waiting").textContent = counts.waiting;
  document.querySelector("#count-ongoing").textContent = counts.ongoing;
  document.querySelector("#count-accepted").textContent = counts.accepted;
  document.querySelector("#count-rejected").textContent = counts.rejected;
}

async function renderSelectedBoardView({ allPeriodCounts, scopedCounts, meetings }, requestId) {
  if (currentFilter === "schedule") {
    cardGridEl.classList.add("schedule-mode");
    await renderScheduleView(cardGridEl, modalEl, backdropEl, refreshApp, {
      showLoading: restoringPageState, meetings,
    });
  } else {
    await renderProcessCards(allPeriodCounts, scopedCounts, requestId);
  }
  if (requestId !== appRefreshRequestId) return;
  cardGridEl.querySelector(":scope > .board-loading-state")?.remove();
  cardGridEl.setAttribute("aria-busy", "false");
}

async function renderProcessCards(allPeriodCounts, scopedCounts, requestId) {
  const jobs = await api.getJobs(processFilters.statuses.length ? "all" : currentFilter, currentSearch);
  if (requestId !== appRefreshRequestId) return;
  const filteredJobs = filterJobs(jobs, processFilters);
  const sortMode = readSavedSortMode(window.localStorage, currentFilter);
  const loadingView = restoringPageState ? captureLoadingView(cardGridEl) : null;
  cardGridEl.classList.remove("schedule-mode");
  renderCardGrid(cardGridEl, sortJobsForDisplay(filteredJobs, currentFilter, sortMode), modalEl, backdropEl, refreshApp, sortMode, {
    filter: currentFilter,
    totalCount: allPeriodCounts.all,
    statusCount: scopedCounts[currentFilter] ?? scopedCounts.all,
    hasCustomView: hasActiveCustomView(),
    groupByStatus: processFilters.statuses.length > 1,
    onResetView: resetSearchAndFilters,
  });
  revealLoadedContent(cardGridEl, loadingView);
}

function hasActiveProcessFilters() {
  return [
    processFilters.statuses.length > 0,
    processFilters.searchPeriod !== "all",
    processFilters.arrangements.length > 0,
    Boolean(processFilters.expectedSalaryQuery),
    processFilters.postedSalaryMin !== null,
    processFilters.postedSalaryMax !== null,
    Boolean(processFilters.currency),
    Boolean(processFilters.referral),
    processFilters.technologies.length > 0,
  ].some(Boolean);
}

function hasActiveCustomView() {
  return Boolean(currentSearch.trim())
    || (currentFilter !== "all" && currentFilter !== "schedule")
    || hasActiveProcessFilters();
}

function clearAppLoadError() {
  if (!appLoadErrorEl) return;
  appLoadErrorEl.hidden = true;
  if (appLoadRetryButton) {
    appLoadRetryButton.disabled = false;
    setTranslatedText(appLoadRetryButton, "common.tryAgain");
  }
}

function showAppLoadError() {
  if (!appLoadErrorEl) return;
  cardGridEl.querySelector(".board-loading-state")?.remove();
  cardGridEl.setAttribute("aria-busy", "false");
  appLoadErrorEl.hidden = false;
  if (appLoadRetryButton) {
    appLoadRetryButton.disabled = false;
    setTranslatedText(appLoadRetryButton, "common.tryAgain");
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
      statuses: readSavedStatuses(saved.statuses),
      searchPeriod: typeof saved.searchPeriod === "string" ? saved.searchPeriod : "all",
      expectedSalaryQuery: typeof saved.expectedSalaryQuery === "string" ? saved.expectedSalaryQuery : "",
      postedSalaryMin: parseFilterNumber(saved.postedSalaryMin ?? saved.salaryMin),
      postedSalaryMax: parseFilterNumber(saved.postedSalaryMax ?? saved.salaryMax),
      currency: readSavedCurrency(saved.currency),
      referral: readSavedReferral(saved.referral),
      technologies: readSavedTechnologies(saved.technologies),
    };
  } catch {
    return { arrangements: [], statuses: [], searchPeriod: "all", expectedSalaryQuery: "", postedSalaryMin: null, postedSalaryMax: null, currency: "", referral: "", technologies: [] };
  }
}

function readSavedStatuses(statuses) {
  const validStatuses = ["waiting", "ongoing", "accepted", "rejected"];
  return Array.isArray(statuses) ? [...new Set(statuses.filter((status) => validStatuses.includes(status)))] : [];
}

function renderSearchPeriodOptions() {
  if (!searchPeriodFilter || !searchPeriodMenu || !searchPeriodCurrent || !searchPeriodTrigger) return;
  const options = [
    { value: "all", labelKey: "searchPeriods.all", menuLabel: t("searchPeriods.all") },
    { value: "unassigned", labelKey: "searchPeriods.none", menuLabel: t("searchPeriods.none") },
    ...searchPeriods.map((period) => ({ value: period.id, label: period.name, menuLabel: formatSearchPeriodLabel(period) })),
  ];
  const selected = searchPeriods.some((period) => period.id === processFilters.searchPeriod)
    || ["all", "unassigned"].includes(processFilters.searchPeriod) ? processFilters.searchPeriod : "all";
  processFilters.searchPeriod = selected;
  searchPeriodFilter.value = selected;
  const selectedOption = options.find((option) => option.value === selected);
  showSearchPeriodName(selectedOption);
  searchPeriodCurrent.dataset.compactLabel = selected === "all" ? t("searchPeriods.compactAll") : searchPeriodCurrent.textContent;
  searchPeriodTrigger.dataset.selected = String(selected !== "all");
  setTranslatedAttribute(searchPeriodTrigger, "title", "searchPeriods.trigger", { period: selectedOption.menuLabel });
  setTranslatedAttribute(searchPeriodTrigger, "aria-label", "searchPeriods.trigger", { period: selectedOption.menuLabel });
  searchPeriodMenu.innerHTML = `
    <div class="search-period-menu-options" role="presentation">${options.map((option) => `<button type="button" role="menuitemradio" aria-checked="${option.value === selected}" data-search-period="${escapeHtml(option.value)}">${escapeHtml(option.menuLabel)}</button>`).join("")}</div>
    <div class="search-period-menu-footer"><button type="button" role="menuitem" data-manage-search-periods>${escapeHtml(t("searchPeriods.manage"))}</button></div>`;
}

/** Labels a saved period with its name and date range. */
function formatSearchPeriodLabel(period) {
  return `${period.name} · ${period.start_date} – ${period.end_date || t("searchPeriods.present")}`;
}

/** Shows the selected period; built-in choices keep following language changes. */
function showSearchPeriodName(option) {
  if (option.labelKey) {
    setTranslatedText(searchPeriodCurrent, option.labelKey);
    return;
  }
  delete searchPeriodCurrent.dataset.i18n;
  searchPeriodCurrent.textContent = option.label;
}

function periodForToday() {
  const now = new Date();
  const today = [now.getFullYear(), String(now.getMonth() + 1).padStart(2, "0"), String(now.getDate()).padStart(2, "0")].join("-");
  return searchPeriods.find((period) => period.start_date <= today && (!period.end_date || period.end_date >= today));
}

function searchPeriodOptions(selectedID) {
  return [
    `<option value="">${escapeHtml(t("searchPeriods.none"))}</option>`,
    ...searchPeriods.map((period) => `<option value="${escapeHtml(period.id)}" ${period.id === selectedID ? "selected" : ""}>${escapeHtml(formatSearchPeriodLabel(period))}</option>`),
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
  if (mobileProcessFilterChips) mobileProcessFilterChips.innerHTML = chips.map(renderProcessFilterChip).join("");
  const mobileActiveFilters = document.querySelector(".process-filter-active");
  if (mobileActiveFilters) mobileActiveFilters.hidden = chips.length === 0;
  if (processToolbarActiveState) processToolbarActiveState.hidden = chips.length === 0;
  if (processFilterClear) processFilterClear.hidden = count === 0;
  if (processFilterPanelFooter) processFilterPanelFooter.hidden = count === 0 && !window.matchMedia("(max-width: 640px)").matches;
  if (summary) summary.textContent = "";
}

function renderProcessFilterChip({ category, label, value = "" }) {
  const valueAttribute = value ? `data-filter-value="${escapeHtml(value)}"` : "";
  const clearLabel = t(category === "searchPeriod" ? "filters.clearSearchPeriod" : "filters.clearFilter", { label });
  const periodIcon = category === "searchPeriod" ? '<svg aria-hidden="true" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="5" width="18" height="16" rx="2"/><path d="M16 3v4M8 3v4M3 10h18"/></svg>' : "";
  return `<button type="button" class="process-filter-chip${category === "searchPeriod" ? " search-period-scope-chip" : ""}" data-remove-filter="${category}" ${valueAttribute} aria-label="${escapeHtml(clearLabel)}" title="${escapeHtml(label)}">${periodIcon}<span>${escapeHtml(label)}</span><svg aria-hidden="true" width="11" height="11" viewBox="0 0 12 12" fill="none"><path d="m3 3 6 6m0-6L3 9" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg></button>`;
}

function getActiveProcessFilterCount() {
  return Number(currentFilter !== "all" && currentFilter !== "schedule")
    + Number(processFilters.statuses.length > 0)
    + Number(processFilters.searchPeriod !== "all")
    + Number(processFilters.arrangements.length > 0)
    + Number(Boolean(processFilters.expectedSalaryQuery))
    + Number(processFilters.postedSalaryMin !== null || processFilters.postedSalaryMax !== null)
    + Number(Boolean(processFilters.currency))
    + Number(Boolean(processFilters.referral))
    + Number(processFilters.technologies.length > 0);
}

function updateProcessFilterTriggerLabel(activeFilterCount) {
  if (!processFilterTrigger) return;
  const count = activeFilterCount ?? getActiveProcessFilterCount();
  if (count) setTranslatedAttribute(processFilterTrigger, "aria-label", "filters.triggerActive", { count });
  else setTranslatedAttribute(processFilterTrigger, "aria-label", "filters.triggerNone");
}

function getProcessFilterChips() {
  return [
    ...(processFilters.statuses.length ? [{ category: "statuses", label: t("filters.statusChip", { statuses: processFilters.statuses.map(statusLabel).join(", ") }) }] : []),
    ...processFilters.arrangements.map((value) => ({ category: "arrangement", value, label: arrangementLabel(value) || value })),
    ...getReferralFilterChips(),
    ...getSalaryFilterChips(),
    ...getTechnologyFilterChips(),
  ];
}

function getReferralFilterChips() {
  if (!processFilters.referral) return [];
  return [{ category: "referral", label: t(processFilters.referral === "yes" ? "referral.yes" : "referral.none") }];
}

function getSalaryFilterChips() {
  const chips = [];
  if (processFilters.expectedSalaryQuery) chips.push({ category: "expectedSalary", label: t("filters.targetChip", { query: processFilters.expectedSalaryQuery }) });
  if (processFilters.postedSalaryMin !== null || processFilters.postedSalaryMax !== null || processFilters.currency) {
    const lower = processFilters.postedSalaryMin === null ? t("filters.any") : processFilters.postedSalaryMin.toLocaleString(getLocale());
    const upper = processFilters.postedSalaryMax === null ? t("filters.any") : processFilters.postedSalaryMax.toLocaleString(getLocale());
    const range = `${lower}–${upper}${processFilters.currency ? ` ${processFilters.currency}` : ""}`;
    chips.push({ category: "postingSalary", label: t("filters.postingChip", { range }) });
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
    statuses: processFilters.statuses,
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
  const isStatusTab = ["all", "waiting", "ongoing", "accepted", "rejected"].includes(currentFilter);
  processToolbarEl.hidden = currentFilter === "schedule";
  processSortControlEl.hidden = !isStatusTab;
  if (!isStatusTab) return;
  const mode = readSavedSortMode(window.localStorage, currentFilter);
  setTranslatedText(processSortCurrent, `sort.short.${mode}`);
  setTranslatedAttribute(processSortTrigger, "aria-label", `sort.full.${mode}`);
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

/** Returns the work details an import found, which have no visible form field. */
function importedClassification(data) {
  return Object.fromEntries(["work_arrangement", "employment_type"]
    .map((name) => [name, data.get(name)])
    .filter(([, value]) => value));
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
    salary_currency: data.get("salary_currency") || "EUR",
    ...importedClassification(data),
    recruiter_type: data.get("recruiter_type"),
    recruiter_name: data.get("recruiter_name"),
    recruiter_agency: data.get("recruiter_agency"),
    job_post_url: data.get("job_post_url"),
    application_url: data.get("application_url"),
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
async function submitNewJobForm(event, form, closeForm, technologyIds) {
  try {
    event.preventDefault();
    const payload = buildJobPayload(form);
    await api.createJob({ ...payload, technology_ids: await technologyIds() });
    showToast(t("newProcess.created"), "success");
    closeForm();
    setFilter("waiting");
    refreshApp();
  } catch (err) {
    showToast(err.message, "error");
  }
}

/**
 * Sets the active status filter or top-level view.
 */
function setFilter(filter, { preserveStatusSelection = false } = {}) {
  if (!validFilters.has(filter)) return;
  const restoreScrollY = getNextViewScrollPosition(filter);
  currentFilter = filter;
  if (filter !== "all" && !preserveStatusSelection) processFilters.statuses = [];
  if (filter !== "schedule") {
    currentStatusFilter = filter;
    try { window.localStorage.setItem(STATUS_FILTER_STORAGE_KEY, filter); } catch { /* Keep the status filter active for this page. */ }
  }
  try {
    const saved = JSON.parse(window.localStorage.getItem(PROCESS_FILTER_STORAGE_KEY) || "{}");
    window.localStorage.setItem(PROCESS_FILTER_STORAGE_KEY, JSON.stringify({ ...saved, statuses: processFilters.statuses }));
  } catch { /* Keep the status selection active for this page. */ }
  try {
    window.localStorage.setItem(FILTER_STORAGE_KEY, filter);
  } catch {
    // Keep navigation working when browser storage is unavailable.
  }
  updateFilterSummary();
  void refreshApp(restoreScrollY === undefined ? undefined : { restoreScrollY });
}

function getNextViewScrollPosition(filter) {
  const previousView = currentFilter === "schedule" ? "schedule" : "board";
  const nextView = filter === "schedule" ? "schedule" : "board";
  if (previousView === nextView) return undefined;
  viewScrollPositions[previousView] = window.scrollY;
  return viewScrollPositions[nextView];
}

function syncBoardViewSwitchOffset() {
  const header = document.querySelector(".app-header");
  document.documentElement.style.setProperty("--board-view-switch-top", `${header.getBoundingClientRect().height}px`);
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
  saveOpenJob("");
  closeWithFlip(modalEl, backdropEl, () => {
    modalEl.innerHTML = "";
    modalEl.classList.remove("new-process-modal");
    restoreModalFocus();
  });
}

/** Closes the data transfer dialog and returns focus to its trigger. */
function closeManagementModal(focusTarget) {
  backdropEl.classList.remove("active");
  backdropEl.classList.remove("search-period-manager-backdrop");
  modalEl.classList.remove("data-transfer-modal");
  modalEl.classList.remove("search-period-manager-modal");
  modalEl.innerHTML = "";
  resetModalAnimation();
  restoreModalFocus();
  focusTarget?.focus();
}

/** Closes the data transfer dialog and returns focus to its trigger. */
function closeDataModal() {
  closeManagementModal(accountMenuButton || dataManagementButton);
}

function defaultSearchPeriodName(startDate, endDate) {
  if (!startDate) return "";
  const formatMonth = (value) => new Intl.DateTimeFormat(getLocale(), { month: "long", year: "numeric", timeZone: "UTC" }).format(new Date(`${value}T00:00:00Z`));
  const startLabel = formatMonth(startDate);
  if (!endDate) return startLabel;
  const endLabel = formatMonth(endDate);
  return startLabel === endLabel ? startLabel : `${startLabel} – ${endLabel}`;
}

async function openSearchPeriodsModal() {
  closeMobileMenu();
  cancelPendingFlipClose(true);
  resetModalAnimation();
  const now = new Date();
  const currentMonth = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-01`;
  const suggestedPeriodName = defaultSearchPeriodName(currentMonth, "");
  modalEl.classList.add("data-transfer-modal");
  modalEl.classList.add("search-period-manager-modal");
  backdropEl.classList.add("search-period-manager-backdrop");
  backdropEl.classList.add("active");
  modalEl.innerHTML = `
    <div class="modal-header data-transfer-header">
      <div><div class="modal-title">${escapeHtml(t("searchPeriods.title"))}</div><div class="modal-company">${escapeHtml(t("searchPeriods.intro"))}</div></div>
      <button class="btn-secondary" id="btn-close-search-periods" type="button" aria-label="${escapeAttr(t("common.close"))}">${icon("close", 18)}</button>
    </div>
    <div class="search-period-manager">
      <form id="search-period-form" class="search-period-form">
        <input type="hidden" name="id" />
        <label>${escapeHtml(t("searchPeriods.name"))}<input name="name" maxlength="100" required placeholder="${escapeAttr(suggestedPeriodName)}" /></label>
        <div class="form-grid form-grid-two">
          <label>${escapeHtml(t("searchPeriods.startDate"))}<input name="start_date" type="date" required /></label>
          <label>${escapeHtml(t("searchPeriods.endDate"))}<input name="end_date" type="date" /><span class="search-period-optional">${escapeHtml(t("searchPeriods.endDateHint"))}</span></label>
        </div>
        <div class="search-period-form-actions">
          <button class="btn-primary" type="submit">${escapeHtml(t("searchPeriods.create"))}</button>
          <button class="btn-secondary" id="btn-cancel-search-period-edit" type="button" hidden>${escapeHtml(t("searchPeriods.cancelEdit"))}</button>
        </div>
      </form>
      <div id="search-period-list" class="search-period-list"></div>
    </div>`;
  const closeButton = modalEl.querySelector("#btn-close-search-periods");
  const initialFocus = window.matchMedia?.("(max-width: 600px)").matches ? "#btn-close-search-periods" : '[name="name"]';
  activateModal(modalEl, initialFocus);
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
    submitButton.textContent = t("searchPeriods.create");
    cancelEdit.hidden = true;
  };
  const renderList = () => {
    list.innerHTML = searchPeriods.length ? searchPeriods.map((period) => `
      <article class="search-period-row">
        <div><strong>${escapeHtml(period.name)}</strong><span>${escapeHtml(period.start_date)} – ${escapeHtml(period.end_date || t("searchPeriods.present"))}</span><small>${escapeHtml(t("searchPeriods.jobCount", { count: period.job_count }))}</small></div>
        <div class="search-period-actions">
          <button class="btn-secondary" type="button" data-edit-period="${escapeHtml(period.id)}">${escapeHtml(t("common.edit"))}</button>
          <button class="btn-secondary" type="button" data-delete-period="${escapeHtml(period.id)}">${escapeHtml(t("common.delete"))}</button>
        </div>
      </article>`).join("") : `<div class="search-period-empty"><strong>${escapeHtml(t("searchPeriods.emptyTitle"))}</strong><span>${escapeHtml(t("searchPeriods.emptyBody"))}</span></div>`;
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
      showToast(t("searchPeriods.saved"), "success");
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
      submitButton.textContent = t("common.saveChanges");
      cancelEdit.hidden = false;
      nameField.focus();
    }
    if (deleteButton) {
      const period = searchPeriods.find((item) => item.id === deleteButton.dataset.deletePeriod);
      if (!period) return;
      const fallbackIndex = [...list.querySelectorAll("[data-delete-period]")]
        .findIndex((button) => button.dataset.deletePeriod === period.id);
      const confirmed = await confirmAction({
        title: t("searchPeriods.deleteTitle"),
        message: t("searchPeriods.deleteMessage", { count: period.job_count }),
        confirmLabel: t("searchPeriods.deleteConfirm"),
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
        showToast(t("searchPeriods.deleted"), "success");
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
      <div><div class="modal-title">${escapeHtml(t("data.title"))}</div><div class="modal-company">${escapeHtml(t("data.subtitle"))}</div></div>
      <button class="btn-secondary" id="btn-close-data-modal" type="button" aria-label="${escapeAttr(t("common.close"))}">${icon("close", 18)}</button>
    </div>
    <div class="data-transfer-content">
      <section class="data-transfer-section">
        <h3>${escapeHtml(t("data.exportTitle"))}</h3>
        <p>${escapeHtml(t(isDemoMode ? "data.exportDemo" : "data.exportBody"))}</p>
        <button class="btn-primary" id="btn-export-backup" type="button" ${isDemoMode ? "disabled" : ""}>${icon("download", 15)} ${escapeHtml(t("data.exportButton"))}</button>
      </section>
      <section class="data-transfer-section">
        <h3>${escapeHtml(t("google.heading"))}</h3>
        <div id="google-drive-backup-option" aria-live="polite"></div>
      </section>
      <section class="data-transfer-section">
        <h3>${escapeHtml(t("data.importTitle"))}</h3>
        <p>${escapeHtml(t(isDemoMode ? "data.importDemo" : "data.importBody"))}</p>
        <label class="data-transfer-file-label" for="backup-import-file">${escapeHtml(t("data.backupFile"))}</label>
        <div class="data-transfer-file-picker">
          <input id="backup-import-file" class="data-transfer-file-input" type="file" accept=".wrbackup,application/octet-stream" aria-label="${escapeAttr(t("data.chooseBackupFile"))}" tabindex="-1" ${isDemoMode ? "disabled" : ""} />
          <button id="backup-import-choose" class="data-transfer-choose" type="button" ${isDemoMode ? "disabled" : ""}>${escapeHtml(t("common.chooseFile"))}</button>
          <span id="backup-import-file-name" class="data-transfer-file-name" role="status" aria-live="polite" aria-atomic="true">${escapeHtml(t("common.noFileSelected"))}</span>
        </div>
        <div class="data-transfer-warning">${escapeHtml(t("data.importWarning"))}</div>
        <label class="data-transfer-confirm"><input id="backup-import-confirm" type="checkbox" ${isDemoMode ? "disabled" : ""} /> ${escapeHtml(t("data.importConfirm"))}</label>
        <button class="btn-danger" id="btn-import-backup" type="button" disabled>${escapeHtml(t("data.importButton"))}</button>
        <div class="data-transfer-status" id="backup-import-status" aria-live="polite"></div>
      </section>
    </div>`;
  backdropEl.classList.add("active");
  activateModal(modalEl, "#btn-close-data-modal");
  void refreshDataGoogleStatus();
  modalEl.querySelector("#btn-close-data-modal").addEventListener("click", closeDataModal);
  modalEl.querySelector("#btn-export-backup").addEventListener("click", downloadBackup);
  const fileInput = modalEl.querySelector("#backup-import-file");
  const confirmation = modalEl.querySelector("#backup-import-confirm");
  const importButton = modalEl.querySelector("#btn-import-backup");
  const updateImportButton = () => { importButton.disabled = !fileInput.files.length || !confirmation.checked; };
  modalEl.querySelector("#backup-import-choose").addEventListener("click", () => fileInput.click());
  fileInput.addEventListener("change", () => {
    modalEl.querySelector("#backup-import-file-name").textContent = fileInput.files[0]?.name || t("common.noFileSelected");
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
  closeProcessFilterPanel();
  closeMobileMenu();
  cancelPendingFlipClose(true);
  resetModalAnimation();
  modalEl.classList.add("data-transfer-modal");
  backdropEl.classList.add("active");
  openTechnologyLibrary(modalEl, closeTechnologyLibraryModal);
}

/** Downloads the encrypted portable backup. */
async function downloadBackup(event) {
  const button = event.currentTarget;
  button.disabled = true;
  try {
    const blob = await api.exportBackup();
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = `war-room-backup-${new Date().toISOString().slice(0, 10)}.wrbackup`;
    document.body.append(anchor);
    anchor.click();
    anchor.remove();
    window.setTimeout(() => URL.revokeObjectURL(url), 1000);
    showToast(t("data.downloaded"), "success");
  } catch (err) {
    showToast(err.message || t("data.exportFailed"), "error");
  } finally {
    button.disabled = false;
  }
}

/** Replaces the local process data with the selected validated backup. */
async function importBackup(file, allowEmpty = false) {
  const button = modalEl.querySelector("#btn-import-backup");
  const status = modalEl.querySelector("#backup-import-status");
  button.disabled = true;
  status.textContent = t("data.importing");
  try {
    await api.importBackup(file, allowEmpty);
    closeDataModal();
    showToast(t("data.imported"), "success");
    await refreshApp();
  } catch (err) {
    if (err.requiresEmptyConfirmation) {
      button.disabled = false;
      status.textContent = "";
      const confirmed = await confirmAction({
        title: t("data.emptyBackupTitle"),
        message: t("data.emptyBackupMessage"),
        confirmLabel: t("data.emptyBackupConfirm"),
        danger: true,
      });
      if (confirmed) await importBackup(file, true);
      return;
    }
    status.textContent = err.message || t("data.importFailed");
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
      <h2 class="modal-title inline-icon-text">${icon("plus", 16)} ${escapeHtml(t("newProcess.heading"))}</h2>
      <button class="modal-close-btn" type="button" aria-label="${escapeAttr(t("common.closeDialog"))}">${icon("close", 14)}</button>
    </div>
    ${jobImportEnabled ? renderProcessEntry() : ""}
    <form id="new-process-form" class="process-form" ${jobImportEnabled ? "hidden" : ""}>
      <input type="hidden" name="work_arrangement" />
      <input type="hidden" name="employment_type" />
      ${renderJobImportStatus()}
      <div class="process-form-defaults" role="note">
        <span class="process-form-defaults-status status-pill waiting">${escapeHtml(t("status.waiting"))}</span>
        <span>${escapeHtml(t("newProcess.defaultsNote"))}</span>
      </div>
      <div class="form-grid form-grid-two">
        <div>
          <label class="meta-label" for="create-position-title">${escapeHtml(t("form.positionTitleRequired"))}</label>
          <input id="create-position-title" type="text" name="position_title" placeholder="${escapeAttr(t("form.positionTitlePlaceholder"))}" autocomplete="organization-title" required style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        </div>
        <div>
          <label class="meta-label" for="create-company-name">${escapeHtml(t("form.companyNameOptional"))}</label>
          <input id="create-company-name" type="text" name="company_name" placeholder="${escapeAttr(t("form.companyNamePlaceholder"))}" autocomplete="organization" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
          <span class="process-form-helper">${escapeHtml(t("newProcess.companyHelp"))}</span>
        </div>
      </div>

      ${searchPeriods.length ? `<div>
        <label class="meta-label" for="create-search-period">${escapeHtml(t("form.searchPeriod"))}</label>
        <select id="create-search-period" name="search_period_id">
          ${searchPeriodOptions(periodForToday()?.id || "")}
        </select>
      </div>` : ""}

      <h3 class="edit-section-heading">${escapeHtml(t("newProcess.linksHeading"))}</h3>
      <div class="form-grid form-grid-two">
        <div>
          <label class="meta-label" for="create-job-post-url">${escapeHtml(t("form.jobPostUrlOptional"))}</label>
          <input id="create-job-post-url" type="url" name="job_post_url" placeholder="https://..." style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        </div>
        <div>
          <label class="meta-label" for="create-application-url">${escapeHtml(t("form.applicationPageOptional"))}</label>
          <input id="create-application-url" type="url" name="application_url" placeholder="${escapeAttr(t("form.applicationUrlPlaceholder"))}" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        </div>
      </div>
      <span class="process-form-helper">${escapeHtml(t("newProcess.linksHelp"))}</span>

      <div>
        <label class="meta-label inline-icon-text" for="create-company-overview">${icon("building", 13)} ${escapeHtml(t("form.companyOverview"))}</label>
        <div class="create-overview-editor">
          <div class="markdown-editor-tabs" role="tablist" aria-label="${escapeAttr(t("markdown.editorMode", { field: t("markdown.field.companyOverview") }))}">
            <button id="create-overview-tab-source" type="button" class="markdown-editor-tab is-active" role="tab" aria-selected="true" aria-controls="create-overview-source-panel" tabindex="0">${escapeHtml(t("markdown.source"))}</button>
            <button id="create-overview-tab-preview" type="button" class="markdown-editor-tab" role="tab" aria-selected="false" aria-controls="create-overview-preview" tabindex="-1">${escapeHtml(t("markdown.preview"))}</button>
          </div>
          <div id="create-overview-source-panel" role="tabpanel" aria-labelledby="create-overview-tab-source">
            <textarea id="create-company-overview" class="role-description-textarea" name="company_overview" rows="4" placeholder="${escapeAttr(t("form.companyOverviewPlaceholder"))}" aria-describedby="create-company-overview-help" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary); resize: vertical; line-height: 1.4;"></textarea>
          </div>
          <div id="create-overview-preview" class="markdown-field-preview create-overview-preview markdown-preview" role="tabpanel" aria-labelledby="create-overview-tab-preview" tabindex="0" hidden></div>
        </div>
        <span id="create-company-overview-help" class="process-form-helper">${escapeHtml(t("newProcess.overviewHelp"))}</span>
      </div>

      ${renderTechnologySuggestions()}

      <details class="process-form-disclosure">
        <summary class="process-form-disclosure-summary">
          <span class="process-form-disclosure-copy"><strong>${escapeHtml(t("newProcess.additionalDetails"))}</strong><span>${escapeHtml(t("newProcess.additionalDetailsHint"))}</span></span>
          ${icon("chevronDown", 16)}
        </summary>
        <div class="process-form-disclosure-content">
          <div class="form-grid form-grid-salary">
            <div>
              <label class="meta-label" for="create-salary-type">${escapeHtml(t("form.salaryType"))}</label>
              <select id="create-salary-type" name="salary_type" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);">
                <option value="limited">${escapeHtml(t("salaryType.limited"))}</option>
                <option value="no_min">${escapeHtml(t("salaryType.noMin"))}</option>
                <option value="no_max">${escapeHtml(t("salaryType.noMax"))}</option>
                <option value="unknown" selected>${escapeHtml(t("salaryType.unknown"))}</option>
              </select>
            </div>
            <div>
              <label class="meta-label" for="create-salary-currency">${escapeHtml(t("form.currency"))}</label>
              <select id="create-salary-currency" name="salary_currency" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);">
                <option value="EUR" selected>EUR</option>
                <option value="USD">USD</option>
                <option value="GBP">GBP</option>
              </select>
            </div>
            <div>
              <label class="meta-label" for="create-salary-min">${escapeHtml(t("form.minimumSalary"))}</label>
              <input id="create-salary-min" type="number" name="salary_min" placeholder="${escapeAttr(t("form.minimumSalaryPlaceholder"))}" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
            </div>
            <div>
              <label class="meta-label" for="create-salary-max">${escapeHtml(t("form.maximumSalary"))}</label>
              <input id="create-salary-max" type="number" name="salary_max" placeholder="${escapeAttr(t("form.maximumSalaryPlaceholder"))}" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
            </div>
          </div>

          <div class="form-grid form-grid-three">
            <div>
              <label class="meta-label" for="create-recruiter-type">${escapeHtml(t("form.recruiterSource"))}</label>
              <select id="create-recruiter-type" name="recruiter_type" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);">
                <option value="none">${escapeHtml(t("recruiterSource.none"))}</option>
                <option value="internal">${escapeHtml(t("recruiterSource.internal"))}</option>
                <option value="external">${escapeHtml(t("recruiterSource.external"))}</option>
              </select>
            </div>
            <div>
              <label class="meta-label" for="create-recruiter-name">${escapeHtml(t("form.recruiterNameOptional"))}</label>
              <input id="create-recruiter-name" type="text" name="recruiter_name" autocomplete="name" placeholder="${escapeAttr(t("form.recruiterNamePlaceholder"))}" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
            </div>
            <div>
              <label class="meta-label" for="create-recruiter-agency">${escapeHtml(t("form.recruitingAgency"))}</label>
              <input id="create-recruiter-agency" type="text" name="recruiter_agency" autocomplete="organization" placeholder="${escapeAttr(t("form.recruitingAgencyPlaceholder"))}" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
            </div>
          </div>

          <div>
            <label class="meta-label" for="create-company-domain">${escapeHtml(t("form.companyDomain"))}</label>
            <input id="create-company-domain" type="text" name="company_domain" placeholder="example.com" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
          </div>

          <div>
            <label class="meta-label" for="create-keyword-note">${escapeHtml(t("form.keywords"))}</label>
            <input id="create-keyword-note" type="text" name="keyword_note" maxlength="100" placeholder="${escapeAttr(t("form.keywordsPlaceholder"))}" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary); font-family: var(--font-mono);" />
            <div style="font-size: 11px; color: var(--text-muted); text-align: right; margin-top: 2px;">
              <span id="keyword-char-count">0</span> ${escapeHtml(t("form.characterLimit", { limit: 100 }))}
            </div>
          </div>
        </div>
      </details>

      <details class="process-form-disclosure">
        <summary class="process-form-disclosure-summary">
          <span class="process-form-disclosure-copy"><strong>${escapeHtml(t("newProcess.preparation"))}</strong><span>${escapeHtml(t("newProcess.preparationHint"))}</span></span>
          ${icon("chevronDown", 16)}
        </summary>
        <div class="process-form-disclosure-content">
          <div>
            <label class="meta-label inline-icon-text" for="create-reasons-to-change">${icon("compass", 13)} ${escapeHtml(t("newProcess.reasonsToChange"))}</label>
            <textarea id="create-reasons-to-change" name="reasons_to_change" rows="3" placeholder="${escapeAttr(t("form.reasonsToChangePlaceholder"))}" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary); resize: vertical; line-height: 1.4;"></textarea>
          </div>

          <div>
            <label class="meta-label inline-icon-text" for="create-relevant-experience">${icon("user", 13)} ${escapeHtml(t("form.relevantExperience"))}</label>
            <textarea id="create-relevant-experience" name="experience_notes" rows="3" placeholder="${escapeAttr(t("form.relevantExperiencePlaceholder"))}" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary); resize: vertical; line-height: 1.4;"></textarea>
          </div>

          <div>
            <label class="meta-label inline-icon-text" for="create-expected-salary">${icon("euro", 13)} ${escapeHtml(t("form.expectedSalary"))}</label>
            <input id="create-expected-salary" type="text" name="expected_salary" placeholder="${escapeAttr(t("form.expectedSalaryPlaceholder"))}" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
          </div>

          <div>
            <label class="meta-label inline-icon-text" for="create-interview-notes">${icon("fileText", 13)} ${escapeHtml(t("newProcess.myNotes"))}</label>
            <textarea id="create-interview-notes" name="interview_notes" rows="3" placeholder="${escapeAttr(t("form.myNotesPlaceholder"))}" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary); resize: vertical; line-height: 1.4;"></textarea>
          </div>
        </div>
      </details>

      <div class="process-form-referral-choice">
        <input type="checkbox" name="is_referral" id="create-referral-cb" style="accent-color: #c084fc; width: 16px; height: 16px; cursor: pointer;" />
        <label for="create-referral-cb" class="process-form-referral-label">
          ${icon("userCheck", 13)} ${escapeHtml(t("newProcess.referral"))}
        </label>
      </div>

      <p class="process-stage-creation-note">${escapeHtml(t("newProcess.stagesNote"))}</p>

      <div class="form-actions">
        <button type="button" class="btn-secondary btn-cancel">${escapeHtml(t("common.cancel"))}</button>
        <button type="submit" class="btn-primary">${escapeHtml(t("newProcess.submit"))}</button>
      </div>
    </form>
  `;

  const form = modalEl.querySelector("#new-process-form");
  const initialFocus = window.matchMedia?.("(max-width: 600px)").matches ? ".modal-close-btn" : jobImportEnabled ? "#job-import-url" : "#create-position-title";
  activateModal(modalEl, initialFocus);
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
    if (isPreview) overviewPreview.innerHTML = renderMarkdown(overviewInput.value) || `<span class="empty-note">${escapeHtml(t("markdown.nothingToPreview"))}</span>`;
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
  const jobImport = bindJobImport(form);
  form.addEventListener("submit", (event) => submitNewJobForm(event, form, closeForm, jobImport.technologyIds));
}

statusFilterControls.forEach((control) => control.addEventListener("change", () => {
  if (control.value === "all") {
    processFilters.statuses = [];
    statusFilterControls.forEach((option) => { if (option.value !== "all") option.checked = false; });
    setFilter("all");
    return;
  }
  processFilters.statuses = [...statusFilterControls]
    .filter((option) => option.value !== "all" && option.checked)
    .map((option) => option.value);
  setFilter(processFilters.statuses.length === 1 ? processFilters.statuses[0] : "all", { preserveStatusSelection: true });
}));
boardButton?.addEventListener("click", () => setFilter(currentStatusFilter, { preserveStatusSelection: true }));
scheduleButton?.addEventListener("click", () => {
  setFilter("schedule", { preserveStatusSelection: true });
});

processFilterTrigger?.addEventListener("click", () => {
  if (!processFilterPanel) return;
  const willOpen = processFilterPanel.hidden;
  if (willOpen) openProcessFilterPanel();
  else closeProcessFilterPanel();
});
document.querySelector(".process-filter-close")?.addEventListener("click", () => closeProcessFilterPanel(true));
document.querySelector(".process-filter-done")?.addEventListener("click", () => closeProcessFilterPanel(true));
processFilterTrigger?.addEventListener("keydown", (event) => {
  if (event.key !== "Escape" || !processFilterPanel || processFilterPanel.hidden) return;
  event.preventDefault();
  closeProcessFilterPanel(true);
});
processFilterPanel?.addEventListener("keydown", (event) => {
  if (processFilterPanel.getAttribute("role") === "dialog" && trapModalTab(event, processFilterPanel)) return;
  if (event.key !== "Escape") return;
  event.preventDefault();
  event.stopPropagation();
  closeProcessFilterPanel(true);
});
function openProcessFilterPanel() {
  if (!processFilterPanel) return;
  processFilterPanel.hidden = false;
  processFilterTrigger?.setAttribute("aria-expanded", "true");
  const isMobile = window.matchMedia("(max-width: 640px)").matches;
  if (processFilterPanelFooter) processFilterPanelFooter.hidden = !isMobile && getActiveProcessFilterCount() === 0;
  processFilterPanel.setAttribute("role", isMobile ? "dialog" : "region");
  if (isMobile) {
    processFilterPanel.setAttribute("aria-modal", "true");
    document.body.classList.add("process-filter-open");
    document.querySelector(".process-filter-close")?.focus();
  } else {
    processFilterPanel.removeAttribute("aria-modal");
  }
}

function closeProcessFilterPanel(restoreFocus = false) {
  if (!processFilterPanel) return;
  processFilterPanel.hidden = true;
  processFilterPanel.removeAttribute("aria-modal");
  document.body.classList.remove("process-filter-open");
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
  setTimeout(() => {
    if (searchPeriodSelectWrap.contains(document.activeElement)) return;
    if (searchPeriodMenu) searchPeriodMenu.hidden = true;
    searchPeriodTrigger?.setAttribute("aria-expanded", "false");
  }, 0);
});

function handleProcessFilterChipClick(event, chipContainer) {
  const chip = event.target.closest("[data-remove-filter]");
  if (!chip) return;
  event.stopPropagation();
  const keyboardActivated = event.detail === 0;
  const chipIndex = [...chipContainer.querySelectorAll("[data-remove-filter]")].indexOf(chip);
  clearProcessFilterCategory(chip.dataset.removeFilter, chip.dataset.filterValue);
  if (!keyboardActivated) return;
  const remainingChips = [...chipContainer.querySelectorAll("[data-remove-filter]")];
  const nextChip = remainingChips[Math.min(chipIndex, remainingChips.length - 1)];
  (nextChip || (chipContainer === mobileProcessFilterChips ? document.querySelector(".process-filter-done") : processFilterTrigger))?.focus();
}

processFilterChips?.addEventListener("click", (event) => handleProcessFilterChipClick(event, processFilterChips));
mobileProcessFilterChips?.addEventListener("click", (event) => handleProcessFilterChipClick(event, mobileProcessFilterChips));

function clearProcessFilterCategory(category, value = "") {
  switch (category) {
    case "arrangement": clearArrangementFilter(value); break;
    case "statuses": processFilters.statuses = []; setFilter("all"); break;
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
  currentFilter = "all";
  currentStatusFilter = "all";
  try {
    window.localStorage.setItem(FILTER_STORAGE_KEY, "all");
    window.localStorage.setItem(STATUS_FILTER_STORAGE_KEY, "all");
  } catch { /* Keep the reset status active for this page. */ }
  updateFilterSummary();
  if (searchPeriodFilter) searchPeriodFilter.value = "all";
  processFilters.searchPeriod = "all";
  processFilters.statuses = [];
  renderSearchPeriodOptions();
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
  currentFilter = "all";
  currentStatusFilter = "all";
  try {
    window.localStorage.setItem(FILTER_STORAGE_KEY, "all");
    window.localStorage.setItem(STATUS_FILTER_STORAGE_KEY, "all");
  } catch { /* Keep the reset view active for this page. */ }
  updateFilterSummary();
  processFilters = {
    arrangements: [], statuses: [], searchPeriod: "all", expectedSalaryQuery: "",
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

document.addEventListener("click", (event) => {
  if (!(event.target instanceof Element)) return;
  if (!event.target.closest(".process-filter-control, .process-toolbar-active-state, .process-filter-panel") && processFilterPanel && !processFilterPanel.hidden) {
    closeProcessFilterPanel();
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
  setTranslatedText(appLoadRetryButton, "common.retrying");
  void refreshApp();
});

newProcessButtons.forEach((button) => button.addEventListener("click", openNewProcessModal));
technologyLibraryButton?.addEventListener("click", openTechnologyLibraryModal);
window.addEventListener("war-room:open-cv-library", openCvLibraryModal);
window.addEventListener("war-room:open-technology-library", openTechnologyLibraryModal);
menuToggle?.addEventListener("click", () => {
  const isOpen = menuToggle.getAttribute("aria-expanded") === "true";
  menuToggle.setAttribute("aria-expanded", String(!isOpen));
  setTranslatedAttribute(menuToggle, "aria-label", isOpen ? "header.openMenu" : "header.closeMenu");
  headerControls?.classList.toggle("is-open", !isOpen);
  syncBoardViewSwitchOffset();
});
backdropEl.addEventListener("click", (e) => {
  if (e.target === backdropEl) {
    if (modalEl.classList.contains("data-transfer-modal")) closeTransferModal();
    else closeModal();
  }
});

/**
 * Re-renders language-dependent views from current state. Keyed elements update
 * themselves; modals cannot be open while the account menu changes the language.
 */
function handleLanguageChange() {
  renderGoogleAccount();
  renderSearchPeriodOptions();
  updateProcessFilterSummary();
  void refreshApp({ restoreScrollY: window.scrollY });
}

window.addEventListener(LANGUAGE_CHANGE_EVENT, handleLanguageChange);
window.addEventListener("keydown", handleGlobalKeydown);
window.addEventListener("scroll", saveWindowScrollPosition, { passive: true });
window.addEventListener("pagehide", saveWindowScrollPosition);

initializeProcessFilterControls();
updateFilterSummary();

syncBoardViewSwitchOffset();
window.addEventListener("resize", syncBoardViewSwitchOffset, { passive: true });

if ("scrollRestoration" in window.history) window.history.scrollRestoration = "manual";
const savedScrollPosition = getSavedWindowScrollPosition();
const savedOpenJob = getSavedOpenJob();
const detailReady = savedOpenJob
  ? openDetailModal(modalEl, backdropEl, savedOpenJob, refreshApp)
  : Promise.resolve();
await refreshApp();
if (savedOpenJob && backdropEl.classList.contains("active")) {
  setModalBackgroundScrollPosition(savedScrollPosition);
} else {
  window.scrollTo(0, savedScrollPosition);
}
restoringPageState = false;
await detailReady;
document.body.dataset.appReady = "true";
