/**
 * @fileoverview The Data & Backups export section: choose a spreadsheet or an encrypted backup, then download it.
 */

import { t } from "../i18n.js";
import { icon } from "../icons.js";
import { escapeHtml } from "../utils/sanitize.js";

const BUTTON_KEYS = { spreadsheet: "data.spreadsheetButton", backup: "data.exportButton" };

function formatOption(value, title, body, { checked = false, disabled = false } = {}) {
  return `<label class="data-format-option">
            <input type="radio" name="data-export-format" value="${value}" ${checked ? "checked" : ""} ${disabled ? "disabled" : ""} />
            <span class="data-format-text"><strong>${escapeHtml(title)}</strong><span>${escapeHtml(body)}</span></span>
          </label>`;
}

/**
 * Renders the export section with the spreadsheet format selected.
 *
 * @param {{demo?: boolean}} [options] - The browser demo cannot create encrypted backups, so that format is disabled.
 * @returns {string} The section HTML.
 */
export function renderDataExport({ demo = false } = {}) {
  return `
      <section class="data-transfer-section" id="data-export-section">
        <h3>${escapeHtml(t("data.exportTitle"))}</h3>
        <fieldset class="data-format-options">
          <legend>${escapeHtml(t("data.formatLegend"))}</legend>
          ${formatOption("spreadsheet", t("data.formatSpreadsheet"), t("data.formatSpreadsheetBody"), { checked: true })}
          ${formatOption("backup", t("data.formatBackup"), t(demo ? "data.exportDemo" : "data.formatBackupBody"), { disabled: demo })}
        </fieldset>
        <button class="btn-primary" id="btn-export-data" type="button">${icon("download", 15)} <span id="btn-export-data-label">${escapeHtml(t(BUTTON_KEYS.spreadsheet))}</span></button>
      </section>`;
}

/**
 * Wires the export section so the button names and downloads the selected format.
 *
 * @param {ParentNode} root - Element that contains the rendered section.
 * @param {{spreadsheet: (event: Event) => unknown, backup: (event: Event) => unknown}} downloads - Download handlers by
 *   format; each receives the button's click event.
 */
export function bindDataExport(root, downloads) {
  const section = root.querySelector("#data-export-section");
  const label = section.querySelector("#btn-export-data-label");
  const selected = () => section.querySelector("input[name=data-export-format]:checked")?.value || "spreadsheet";
  section.addEventListener("change", () => { label.textContent = t(BUTTON_KEYS[selected()]); });
  section.querySelector("#btn-export-data").addEventListener("click", (event) => downloads[selected()](event));
}
