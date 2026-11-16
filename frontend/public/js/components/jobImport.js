import { api } from "../api.js";
import { icon } from "../icons.js";
import { escapeAttr, escapeHtml } from "../utils/sanitize.js";
import { htmlToMarkdown } from "../utils/clipboardMarkdown.js";
import { t } from "../i18n.js";
import { companyText } from "../utils/labels.js";
import { serverMessage } from "../utils/serverMessages.js";

const FILLED_FIELDS = ["position_title", "company_name", "company_overview", "job_post_url", "application_url", "company_domain", "keyword_note"];
const PASTE_CODES = new Set(["paste_required", "blocked", "unsupported", "timeout"]);

/** Renders the first step of the new-process modal: import from a link or fill in by hand. */
export function renderProcessEntry() {
  return `
    <div id="process-entry" class="process-entry">
      <section class="process-entry-option" aria-labelledby="process-entry-link-title">
        <h3 id="process-entry-link-title" class="process-entry-title inline-icon-text">${icon("download", 16)} ${escapeHtml(t("jobImport.linkTitle"))}</h3>
        <p class="process-entry-copy">${escapeHtml(t("jobImport.linkCopy"))}</p>
        <label class="sr-only" for="job-import-url">${escapeHtml(t("jobImport.linkLabel"))}</label>
        <div class="job-import-row">
          <input id="job-import-url" type="url" inputmode="url" autocomplete="off" placeholder="${escapeAttr(t("jobImport.urlPlaceholder"))}" />
          <button id="job-import-submit" type="button" class="btn-primary">${escapeHtml(t("jobImport.import"))}</button>
        </div>
        <div id="job-import-entry-status" class="job-import-status" role="status" aria-live="polite" hidden></div>
      </section>
      <div class="process-entry-divider"><span>${escapeHtml(t("jobImport.or"))}</span></div>
      <section class="process-entry-option" aria-labelledby="process-entry-manual-title">
        <h3 id="process-entry-manual-title" class="process-entry-title inline-icon-text">${icon("edit", 16)} ${escapeHtml(t("jobImport.manualTitle"))}</h3>
        <p class="process-entry-copy">${escapeHtml(t("jobImport.manualCopy"))}</p>
        <button id="process-entry-manual" type="button" class="btn-secondary">${escapeHtml(t("jobImport.manualTitle"))}</button>
      </section>
    </div>`;
}

/** Renders the import result shown above the reviewed form. */
export function renderJobImportStatus() {
  return `<div id="job-import-status" class="job-import-status" role="status" aria-live="polite" hidden></div>`;
}

/** Renders the Tech Stack suggestions, hidden until a posting provides some. */
export function renderTechnologySuggestions() {
  return `<fieldset id="job-import-technologies" class="job-import-technologies" hidden>
    <legend class="meta-label">${escapeHtml(t("jobImport.suggestedStack"))}</legend>
    <div class="job-import-technology-list"></div>
    <span class="process-form-helper">${escapeHtml(t("jobImport.suggestedStackHelp"))}</span>
  </fieldset>`;
}

/** Writes a value only into an empty field, so imports never replace typed text. */
function fillEmpty(field, value) {
  if (!field || !value || field.value) return false;
  field.value = value;
  field.dispatchEvent(new Event("input", { bubbles: true }));
  return true;
}

/** Fills the salary as one unit, so a currency never pairs with typed amounts. */
function fillSalary(form, draft) {
  const minimum = form.elements.salary_min;
  const maximum = form.elements.salary_max;
  if (minimum.value || maximum.value) return;
  if (draft.salary_min == null && draft.salary_max == null) return;
  fillEmpty(minimum, draft.salary_min == null ? "" : String(draft.salary_min));
  fillEmpty(maximum, draft.salary_max == null ? "" : String(draft.salary_max));
  form.elements.salary_currency.value = draft.salary_currency;
}

/** Applies a reviewed-before-saving draft to the form. */
export function applyDraft(form, draft) {
  FILLED_FIELDS.forEach((name) => fillEmpty(form.elements[name], draft[name]));
  fillSalary(form, draft);
  form.elements.work_arrangement.value = draft.work_arrangement || "";
  form.elements.employment_type.value = draft.employment_type || "";
  const details = form.querySelector("details");
  if (details && (draft.salary_min != null || draft.salary_max != null || draft.company_domain || draft.keyword_note)) details.open = true;
}

/** Shows one status message; every value is set as text because it may come from a remote page. */
function showStatus(element, tone, lines) {
  element.hidden = false;
  element.dataset.tone = tone;
  element.replaceChildren(...lines.map((line) => {
    const paragraph = document.createElement("p");
    paragraph.textContent = line;
    return paragraph;
  }));
}

function duplicateLine(duplicate) {
  return duplicate ? t("jobImport.duplicate", { position: duplicate.position_title, company: companyText(duplicate.company_name) }) : null;
}

/** Inserts text at the cursor, replacing the selection. */
function insertAtCursor(field, text) {
  field.setRangeText(text, field.selectionStart, field.selectionEnd, "end");
  field.dispatchEvent(new Event("input", { bubbles: true }));
}

/** Connects the import field, smart paste, and suggestions to one form. */
export function bindJobImport(form) {
  const section = form.querySelector("#job-import-technologies");
  const list = form.querySelector(".job-import-technology-list");
  const overview = form.elements.company_overview;
  let suggestions = [];

  const renderSuggestions = (items) => {
    const chosen = new Set([...list.querySelectorAll("input:checked")].map((input) => input.value));
    suggestions = items;
    section.hidden = items.length === 0;
    list.innerHTML = items.map((item, index) => `<label class="job-import-technology">
      <input type="checkbox" value="${escapeAttr(item.name)}" data-index="${index}" ${item.recommended || chosen.has(item.name) ? "checked" : ""} />
      <span>${escapeHtml(item.name)}</span>
    </label>`).join("");
  };

  const suggestFromOverview = async () => {
    try {
      const preview = await api.previewJobImport({ description: overview.value });
      renderSuggestions(preview.technologies);
    } catch {
      // Suggestions are optional; pasting must keep working when they fail.
    }
  };

  overview.addEventListener("paste", (event) => {
    const html = event.clipboardData?.getData("text/html");
    if (html) {
      event.preventDefault();
      insertAtCursor(overview, htmlToMarkdown(html));
    }
    setTimeout(suggestFromOverview, 0);
  });

  bindProcessEntry(form, renderSuggestions);

  return {
    /** Returns the selected technology ids, adding new starter entries to the catalog. */
    async technologyIds() {
      const selected = [...list.querySelectorAll("input:checked")].map((input) => suggestions[Number(input.dataset.index)]);
      return selected.length ? Promise.all(selected.map(resolveTechnology)) : undefined;
    },
  };
}

/** Replaces the entry step with the full form. */
function revealForm(form, entry, focusTarget) {
  entry.hidden = true;
  form.hidden = false;
  form.scrollTop = 0;
  focusTarget.focus();
}

/** Connects the entry step's two options to the form. */
function bindProcessEntry(form, renderSuggestions) {
  const entry = form.parentElement.querySelector("#process-entry");
  if (!entry) return;
  const input = entry.querySelector("#job-import-url");
  const button = entry.querySelector("#job-import-submit");
  const entryStatus = entry.querySelector("#job-import-entry-status");
  const formStatus = form.querySelector("#job-import-status");
  let latest = 0;

  const run = async () => {
    const url = input.value.trim();
    if (!url) return;
    const request = ++latest;
    button.disabled = true;
    showStatus(entryStatus, "info", [t("jobImport.reading")]);
    try {
      const preview = await api.previewJobImport({ url });
      if (request !== latest) return;
      applyDraft(form, preview.draft);
      renderSuggestions(preview.technologies);
      const lines = [t("jobImport.filled"), ...preview.draft.warnings.map((warning) => serverMessage(warning, t("server.importWarning"))), duplicateLine(preview.duplicate)];
      showStatus(formStatus, preview.duplicate ? "warning" : "success", lines.filter(Boolean));
      revealForm(form, entry, form.elements.position_title);
    } catch (error) {
      if (request === latest) showImportFailure(form, entry, { entryStatus, formStatus }, url, error);
    } finally {
      if (request === latest) button.disabled = false;
    }
  };

  button.addEventListener("click", run);
  input.addEventListener("keydown", (event) => {
    if (event.key !== "Enter") return;
    event.preventDefault();
    run();
  });
  entry.querySelector("#process-entry-manual").addEventListener("click", () => {
    latest += 1;
    revealForm(form, entry, form.elements.position_title);
  });
}

/** Opens the form for paste when a site cannot be read; otherwise keeps the link step open to retry. */
function showImportFailure(form, entry, { entryStatus, formStatus }, url, error) {
  const lines = [error.message, duplicateLine(error.duplicate)];
  if (!PASTE_CODES.has(error.code)) {
    showStatus(entryStatus, "warning", lines.filter(Boolean));
    return;
  }
  fillEmpty(form.elements.job_post_url, url);
  showStatus(formStatus, "warning", [error.message, t("jobImport.pasteInstead"), lines[1]].filter(Boolean));
  revealForm(form, entry, form.elements.company_overview);
}

/** Finds or creates the catalog entry behind a suggestion. */
async function resolveTechnology(item) {
  if (item.id) return item.id;
  try {
    const created = await api.createTechnology({ name: item.name, aliases: item.aliases || [] });
    return created.id;
  } catch (error) {
    const catalog = await api.getTechnologies();
    const existing = catalog.find((technology) => technology.name.toLowerCase() === item.name.toLowerCase());
    if (existing) return existing.id;
    throw error;
  }
}
