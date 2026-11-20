/**
 * @fileoverview The Data & Backups import section. One file picker takes an edited board spreadsheet or an encrypted
 * backup and recognizes which it is: a spreadsheet is previewed and only its changes are saved, while a backup
 * replaces all saved data once the user confirms. Nothing is saved until the user confirms.
 */

import { api } from "../api.js";
import { t } from "../i18n.js";
import { icon } from "../icons.js";
import { confirmAction } from "../utils/confirmDialog.js";
import { applyImportPlan, buildImportPlan, planSize } from "../utils/importPlan.js";
import { companyText, stageName } from "../utils/labels.js";
import { escapeAttr, escapeHtml } from "../utils/sanitize.js";
import { readSpreadsheet } from "../utils/spreadsheetReader.js";
import { showToast } from "../utils/toast.js";

const ACCEPTED_TYPES = [
  ".xlsx", ".csv", ".tsv", ".txt", ".wrbackup",
  "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "text/csv", "text/tab-separated-values", "application/octet-stream",
].join(",");
const BACKUP_EXTENSION = ".wrbackup";
// Both the current chunked backup format and the older single-shot one start with this marker.
const BACKUP_MAGIC = "WRBACKUP";
const LIST_LIMIT = 50;
const SHEET_KEYS = { board: "workbook.sheetBoard", questions: "workbook.sheetQuestions" };
const READ_ERRORS = {
  unreadable: "spreadsheetImport.error.unreadable",
  tooLarge: "spreadsheetImport.error.tooLarge",
  noSheets: "spreadsheetImport.error.noSheets",
};
const COUNT_KEYS = [
  ["processUpdates", "spreadsheetImport.processUpdates"],
  ["processCreates", "spreadsheetImport.processCreates"],
  ["questionUpdates", "spreadsheetImport.questionUpdates"],
  ["questionCreates", "spreadsheetImport.questionCreates"],
];

/**
 * Renders the import section.
 *
 * @returns {string} The section HTML.
 */
export function renderDataImport() {
  return `
      <section class="data-transfer-section" id="data-import-section">
        <h3>${escapeHtml(t("data.importTitle"))}</h3>
        <p>${escapeHtml(t("data.importBody"))}</p>
        <label class="data-transfer-file-label" for="data-import-file">${escapeHtml(t("data.importFile"))}</label>
        <div class="data-transfer-file-picker">
          <input id="data-import-file" class="data-transfer-file-input" type="file" accept="${escapeAttr(ACCEPTED_TYPES)}" aria-label="${escapeAttr(t("data.chooseImportFile"))}" tabindex="-1" />
          <button id="data-import-choose" class="data-transfer-choose" type="button">${escapeHtml(t("common.chooseFile"))}</button>
          <span id="data-import-file-name" class="data-transfer-file-name" role="status" aria-live="polite" aria-atomic="true">${escapeHtml(t("common.noFileSelected"))}</span>
        </div>
        <div class="data-import-preview" id="data-import-preview" aria-live="polite"></div>
        <div class="data-import-backup" id="data-import-backup" hidden>
          <div class="data-transfer-warning">${escapeHtml(t("data.importWarning"))}</div>
          <label class="data-transfer-confirm"><input id="backup-import-confirm" type="checkbox" /> ${escapeHtml(t("data.importConfirm"))}</label>
          <button class="btn-danger" id="btn-import-backup" type="button" disabled>${escapeHtml(t("data.importButton"))}</button>
        </div>
        <button class="btn-primary" id="btn-import-spreadsheet" type="button" hidden disabled>${icon("upload", 15)} ${escapeHtml(t("spreadsheetImport.button"))}</button>
        <div class="data-transfer-status" id="data-import-status" aria-live="polite"></div>
      </section>`;
}

/**
 * Tells whether a file is an encrypted War Room backup, by its extension or its first bytes.
 *
 * @param {Blob & {name?: string}} file - The chosen file.
 * @returns {Promise<boolean>} True for a backup; anything else is read as a spreadsheet.
 */
export async function isBackupFile(file) {
  if (String(file.name || "").toLowerCase().endsWith(BACKUP_EXTENSION)) return true;
  const head = new Uint8Array(await file.slice(0, BACKUP_MAGIC.length).arrayBuffer());
  return String.fromCharCode(...head) === BACKUP_MAGIC;
}

function processName(job) {
  return t("card.processAt", { position: job.position_title || "", company: companyText(job.company_name || "Unknown") });
}

function columnList(keys) {
  return keys.map((key) => t(key)).join(", ");
}

/** Describes each record the plan changes, in the order they will be saved. */
function changeItems(plan) {
  return [
    ...plan.processUpdates.map(({ job, columns }) => t("spreadsheetImport.processChanged", { name: processName(job), columns: columnList(columns) })),
    ...plan.processCreates.map(({ values }) => t("spreadsheetImport.processAdded", { name: processName(values) })),
    ...plan.questionUpdates.map(({ entry, columns }) => t("spreadsheetImport.questionChanged", { name: processName(entry.job), stage: stageName(entry.stage), columns: columnList(columns) })),
    ...plan.questionCreates.map(({ target, values }) => t("spreadsheetImport.questionAdded", { name: processName(target.job), stage: stageName(target.stage), question: values.question })),
  ];
}

function rowMessage(sheet, row, message) {
  return row ? t("spreadsheetImport.rowMessage", { sheet: t(SHEET_KEYS[sheet]), row, message }) : message;
}

/**
 * Describes why a row or cell is not imported, in the selected language.
 *
 * @param {import("../utils/importPlan.js").ImportPlan["warnings"][number]} warning - A plan warning.
 * @returns {string} The message.
 */
export function warningText(warning) {
  const params = { ...warning.params };
  if (warning.column) params.column = t(warning.column);
  if (warning.columns) params.columns = columnList(warning.columns);
  return rowMessage(warning.sheet, warning.row, t(`spreadsheetImport.problem.${warning.problem}`, params));
}

function listHtml(items) {
  const shown = items.slice(0, LIST_LIMIT).map((item) => `<li>${escapeHtml(item)}</li>`);
  if (items.length > LIST_LIMIT) shown.push(`<li class="spreadsheet-import-more">${escapeHtml(t("spreadsheetImport.more", { count: items.length - LIST_LIMIT }))}</li>`);
  return `<ul class="spreadsheet-import-list">${shown.join("")}</ul>`;
}

function noticeHtml(heading, items) {
  return `<div class="data-transfer-warning spreadsheet-import-notice"><strong>${escapeHtml(heading)}</strong>${listHtml(items)}</div>`;
}

function summaryHtml(plan) {
  const counts = COUNT_KEYS.filter(([list]) => plan[list].length).map(([list, key]) => `<li>${escapeHtml(t(key, { count: plan[list].length }))}</li>`);
  if (!counts.length) return `<p class="spreadsheet-import-empty">${escapeHtml(t("spreadsheetImport.noChanges"))}</p>`;
  return `<ul class="spreadsheet-import-counts">${counts.join("")}</ul>
    <details class="spreadsheet-import-changes" open><summary>${escapeHtml(t("spreadsheetImport.changesHeading"))}</summary>${listHtml(changeItems(plan))}</details>`;
}

function previewHtml(plan) {
  const warnings = plan.warnings.length ? noticeHtml(t("spreadsheetImport.warningsHeading", { count: plan.warnings.length }), plan.warnings.map(warningText)) : "";
  return summaryHtml(plan) + warnings;
}

function errorHtml(message) {
  return `<p class="spreadsheet-import-error" role="alert">${escapeHtml(message)}</p>`;
}

function readFailure(error) {
  return t(READ_ERRORS[error?.code] || "spreadsheetImport.error.failed");
}

/** Forgets the previous choice and hides both import actions. */
function resetChoice(view, state, file) {
  state.plan = null;
  state.backup = null;
  view.fileName.textContent = file?.name || t("common.noFileSelected");
  view.preview.innerHTML = "";
  view.status.textContent = "";
  view.backupPanel.hidden = true;
  view.confirm.checked = false;
  view.backupButton.disabled = true;
  view.sheetButton.hidden = true;
  view.sheetButton.disabled = true;
}

/** Shows the replace warning and confirmation for a backup; the browser demo cannot restore backups. */
function showBackupChoice(view, state, file) {
  if (state.demo) {
    view.preview.innerHTML = `<p>${escapeHtml(t("data.importDemo"))}</p>`;
    return;
  }
  state.backup = file;
  view.backupPanel.hidden = false;
}

/** Reads a spreadsheet and shows what importing it would change. */
async function previewSpreadsheet(view, state, file, request) {
  view.preview.innerHTML = `<p>${escapeHtml(t("spreadsheetImport.reading"))}</p>`;
  view.sheetButton.hidden = false;
  try {
    const plan = await buildImportPlan(await readSpreadsheet(file), api);
    if (request !== state.request) return;
    state.plan = plan;
    view.preview.innerHTML = previewHtml(plan);
    view.sheetButton.disabled = planSize(plan) === 0;
  } catch (error) {
    if (request !== state.request) return;
    view.preview.innerHTML = errorHtml(readFailure(error));
    view.sheetButton.hidden = true;
  }
}

/** Recognizes the chosen file and offers the matching import; a newer choice replaces an older one still loading. */
async function chooseFile(view, state) {
  const file = view.input.files[0];
  const request = ++state.request;
  resetChoice(view, state, file);
  if (!file) return;
  const backup = await isBackupFile(file).catch(() => false);
  if (request !== state.request) return;
  if (backup) showBackupChoice(view, state, file);
  else await previewSpreadsheet(view, state, file, request);
}

function setBusy(view, busy) {
  view.input.disabled = busy;
  view.chooser.disabled = busy;
  view.confirm.disabled = busy;
  view.sheetButton.disabled = true;
  view.backupButton.disabled = busy || !view.confirm.checked;
}

/** Clears the picker after a spreadsheet import so the next choice starts fresh. */
function clearPicker(view) {
  view.input.value = "";
  view.fileName.textContent = t("common.noFileSelected");
  view.status.textContent = "";
  view.sheetButton.hidden = true;
}

/** Saves the previewed spreadsheet changes, then lists any record that could not be saved. */
async function applySpreadsheet(view, state, onImported) {
  const plan = state.plan;
  if (!plan) return;
  state.plan = null;
  setBusy(view, true);
  const result = await applyImportPlan(plan, api, (done, total) => {
    view.status.textContent = t("spreadsheetImport.applying", { done, total });
  });
  setBusy(view, false);
  clearPicker(view);
  const failures = result.failures.map(({ sheet, row, message }) => rowMessage(sheet, row, message || t("spreadsheetImport.error.saveFailed")));
  view.preview.innerHTML = failures.length ? noticeHtml(t("spreadsheetImport.failuresHeading", { count: failures.length }), failures) : "";
  if (failures.length) showToast(t("spreadsheetImport.partial", { failed: failures.length, total: result.total }), "error");
  else showToast(t("spreadsheetImport.imported", { count: result.total }), "success");
  await onImported({ close: !failures.length && view.section.isConnected });
}

/** Asks before restoring a backup that has no processes, since that empties the workspace. */
async function confirmEmptyBackup(view, state, onImported) {
  const confirmed = await confirmAction({
    title: t("data.emptyBackupTitle"),
    message: t("data.emptyBackupMessage"),
    confirmLabel: t("data.emptyBackupConfirm"),
    danger: true,
  });
  if (confirmed) await importBackup(view, state, onImported, true);
}

/** Replaces the saved data with the chosen backup after the server validates and decrypts it. */
async function importBackup(view, state, onImported, allowEmpty = false) {
  const file = state.backup;
  if (!file) return;
  setBusy(view, true);
  view.status.textContent = t("data.importing");
  try {
    await api.importBackup(file, allowEmpty);
    view.status.textContent = "";
    showToast(t("data.imported"), "success");
    await onImported({ close: view.section.isConnected });
  } catch (error) {
    setBusy(view, false);
    view.status.textContent = "";
    if (error.requiresEmptyConfirmation) await confirmEmptyBackup(view, state, onImported);
    else view.status.textContent = error.message || t("data.importFailed");
  }
}

/**
 * Wires the import section.
 *
 * @param {ParentNode} root - Element that contains the rendered section.
 * @param {{demo?: boolean, onImported: (result: {close: boolean}) => Promise<void>|void}} options - Whether this is the
 *   browser demo, which cannot restore backups, and the callback that refreshes the app after saving; close is true when
 *   everything was saved and the dialog is still open.
 */
export function bindDataImport(root, { demo = false, onImported }) {
  const view = {
    section: root.querySelector("#data-import-section"),
    input: root.querySelector("#data-import-file"),
    chooser: root.querySelector("#data-import-choose"),
    fileName: root.querySelector("#data-import-file-name"),
    preview: root.querySelector("#data-import-preview"),
    backupPanel: root.querySelector("#data-import-backup"),
    confirm: root.querySelector("#backup-import-confirm"),
    backupButton: root.querySelector("#btn-import-backup"),
    sheetButton: root.querySelector("#btn-import-spreadsheet"),
    status: root.querySelector("#data-import-status"),
  };
  const state = { demo, plan: null, backup: null, request: 0 };
  view.chooser.addEventListener("click", () => view.input.click());
  view.input.addEventListener("change", () => chooseFile(view, state));
  view.confirm.addEventListener("change", () => { view.backupButton.disabled = !view.confirm.checked || !state.backup; });
  view.sheetButton.addEventListener("click", () => applySpreadsheet(view, state, onImported));
  view.backupButton.addEventListener("click", () => importBackup(view, state, onImported));
}
