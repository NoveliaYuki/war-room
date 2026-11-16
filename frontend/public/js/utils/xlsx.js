/**
 * @fileoverview Writes a minimal Excel workbook (Office Open XML SpreadsheetML).
 * Text is written as inline strings, so a value that looks like a formula is never
 * evaluated; numbers and dates are real numeric cells.
 */

import { createZip } from "./zip.js";

/** Excel's limit on the number of characters in one cell. */
export const MAX_CELL_LENGTH = 32767;
const MAX_SHEET_NAME_LENGTH = 31;
const MAIN_NS = "http://schemas.openxmlformats.org/spreadsheetml/2006/main";
const REL_NS = "http://schemas.openxmlformats.org/officeDocument/2006/relationships";
const PACKAGE_REL_NS = "http://schemas.openxmlformats.org/package/2006/relationships";
const XML_HEADER = '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>\n';
// Characters XML 1.0 forbids, including lone UTF-16 surrogates.
const INVALID_XML = /[^\t\n\r -퟿-�\u{10000}-\u{10FFFF}]/gu;
const XML_ESCAPES = { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&apos;" };
// Style index for each cell kind; matches the cellXfs order in styles.xml.
const STYLES = { text: 1, wrap: 2, number: 3, date: 4, header: 5, group: 6 };
const NUMERIC_KINDS = new Set(["number", "date"]);
const EXCEL_EPOCH = Date.UTC(1899, 11, 30);
const DAY_MS = 86400000;

/**
 * Removes characters XML 1.0 cannot carry and normalizes line breaks to "\n".
 *
 * @param {unknown} value - Text to clean; null and undefined become "".
 * @returns {string} The cleaned text.
 */
export function cleanText(value) {
  return String(value ?? "").replace(/\r\n?/g, "\n").replace(INVALID_XML, "");
}

/** Cuts text to Excel's cell limit without splitting a surrogate pair. */
function truncate(text) {
  if (text.length <= MAX_CELL_LENGTH) return text;
  const cut = text.slice(0, MAX_CELL_LENGTH);
  return /[\uD800-\uDBFF]$/.test(cut) ? cut.slice(0, -1) : cut;
}

/**
 * Escapes text for an XML element or attribute.
 *
 * @param {string} text - Text that is already valid XML 1.0.
 * @returns {string} The escaped text.
 */
export function escapeXml(text) {
  return text.replace(/[&<>"']/g, (character) => XML_ESCAPES[character]);
}

/**
 * Returns the spreadsheet column letters for a zero-based column index (0 is A, 26 is AA).
 *
 * @param {number} index - Zero-based column index.
 * @returns {string} The column letters.
 */
export function columnName(index) {
  let name = "";
  for (let remaining = index + 1; remaining > 0; remaining = Math.floor((remaining - 1) / 26)) {
    name = String.fromCharCode(65 + ((remaining - 1) % 26)) + name;
  }
  return name;
}

/**
 * Converts a calendar date to an Excel date serial number (days since 1899-12-30).
 *
 * @param {number} year - Full year.
 * @param {number} month - Month, 1 to 12.
 * @param {number} day - Day of the month.
 * @returns {number} The serial number.
 */
export function dateSerial(year, month, day) {
  return (Date.UTC(year, month - 1, day) - EXCEL_EPOCH) / DAY_MS;
}

/**
 * Makes a valid, unique-looking sheet name: at most 31 characters, none of []:*?/\, and no apostrophe at either end.
 *
 * @param {string} name - Preferred name.
 * @returns {string} A name Excel accepts.
 */
export function sheetName(name) {
  const cleaned = cleanText(name).replace(/[[\]:*?/\\\n\t]/g, " ").replace(/^'+|'+$/g, "").trim();
  return Array.from(cleaned).slice(0, MAX_SHEET_NAME_LENGTH).join("").trim() || "Sheet";
}

/** Returns true when a cell has nothing to write. */
function isBlank(value) {
  return value == null || value === "";
}

/** Writes one cell, or "" for an empty cell that needs no style. */
function cellXml(cell, reference) {
  const style = STYLES[cell.kind] ?? 0;
  if (isBlank(cell.value)) return cell.kind === "group" ? `<c r="${reference}" s="${style}"/>` : "";
  if (NUMERIC_KINDS.has(cell.kind) && Number.isFinite(Number(cell.value))) {
    return `<c r="${reference}" s="${style}"><v>${Number(cell.value)}</v></c>`;
  }
  const text = escapeXml(truncate(cleanText(cell.value)));
  return `<c r="${reference}" s="${style}" t="inlineStr"><is><t xml:space="preserve">${text}</t></is></c>`;
}

function rowXml(cells, rowIndex) {
  const row = rowIndex + 1;
  const content = cells.map((cell, column) => (cell ? cellXml(cell, `${columnName(column)}${row}`) : "")).join("");
  return `<row r="${row}">${content}</row>`;
}

function columnsXml(columns) {
  const items = columns.map((column, index) => {
    const hidden = column.hidden ? ' hidden="1"' : "";
    return `<col min="${index + 1}" max="${index + 1}" width="${column.width}" customWidth="1"${hidden}/>`;
  });
  return `<cols>${items.join("")}</cols>`;
}

function sheetViewXml(frozenRows) {
  if (!frozenRows) return '<sheetViews><sheetView workbookViewId="0"/></sheetViews>';
  const topLeft = `A${frozenRows + 1}`;
  return `<sheetViews><sheetView workbookViewId="0"><pane ySplit="${frozenRows}" topLeftCell="${topLeft}" activePane="bottomLeft" state="frozen"/><selection pane="bottomLeft" activeCell="${topLeft}" sqref="${topLeft}"/></sheetView></sheetViews>`;
}

/** Returns the autofilter range, from the filter row to the last row, across every column. */
function filterRange(sheet) {
  if (!sheet.filterRow) return "";
  const lastRow = Math.max(sheet.rows.length, sheet.filterRow);
  return `A${sheet.filterRow}:${columnName(sheet.columns.length - 1)}${lastRow}`;
}

function mergeCellsXml(merges = []) {
  if (!merges.length) return "";
  return `<mergeCells count="${merges.length}">${merges.map((range) => `<mergeCell ref="${range}"/>`).join("")}</mergeCells>`;
}

/**
 * Writes one worksheet part. Element order follows the SpreadsheetML schema, which Excel enforces.
 *
 * @param {WorksheetSpec} sheet - Sheet contents and layout.
 * @returns {string} The worksheet XML.
 */
export function worksheetXml(sheet) {
  const range = filterRange(sheet);
  return `${XML_HEADER}<worksheet xmlns="${MAIN_NS}" xmlns:r="${REL_NS}">`
    + sheetViewXml(sheet.frozenRows)
    + '<sheetFormatPr defaultRowHeight="15"/>'
    + columnsXml(sheet.columns)
    + `<sheetData>${sheet.rows.map(rowXml).join("")}</sheetData>`
    + (range ? `<autoFilter ref="${range}"/>` : "")
    + mergeCellsXml(sheet.merges)
    + "</worksheet>";
}

/** Returns a sheet-qualified absolute range for a defined name, quoting the sheet name. */
function absoluteRange(name, range) {
  const absolute = range.replace(/([A-Z]+)(\d+)/g, "$$$1$$$2");
  return `'${name.replaceAll("'", "''")}'!${absolute}`;
}

function workbookXml(sheets) {
  const entries = sheets.map((sheet, index) => `<sheet name="${escapeXml(sheet.name)}" sheetId="${index + 1}" r:id="rId${index + 1}"/>`);
  const filters = sheets.map((sheet, index) => [sheet, index, filterRange(sheet)]).filter(([, , range]) => range)
    .map(([sheet, index, range]) => `<definedName name="_xlnm._FilterDatabase" localSheetId="${index}" hidden="1">${escapeXml(absoluteRange(sheet.name, range))}</definedName>`);
  const definedNames = filters.length ? `<definedNames>${filters.join("")}</definedNames>` : "";
  return `${XML_HEADER}<workbook xmlns="${MAIN_NS}" xmlns:r="${REL_NS}"><bookViews><workbookView/></bookViews><sheets>${entries.join("")}</sheets>${definedNames}</workbook>`;
}

function workbookRelsXml(sheets) {
  const sheetRels = sheets.map((_, index) => `<Relationship Id="rId${index + 1}" Type="${REL_NS}/worksheet" Target="worksheets/sheet${index + 1}.xml"/>`);
  const styles = `<Relationship Id="rId${sheets.length + 1}" Type="${REL_NS}/styles" Target="styles.xml"/>`;
  return `${XML_HEADER}<Relationships xmlns="${PACKAGE_REL_NS}">${sheetRels.join("")}${styles}</Relationships>`;
}

function contentTypesXml(sheets) {
  const worksheets = sheets.map((_, index) => `<Override PartName="/xl/worksheets/sheet${index + 1}.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`);
  return `${XML_HEADER}<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`
    + '<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>'
    + '<Default Extension="xml" ContentType="application/xml"/>'
    + '<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>'
    + '<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>'
    + `${worksheets.join("")}</Types>`;
}

const ROOT_RELS = `${XML_HEADER}<Relationships xmlns="${PACKAGE_REL_NS}"><Relationship Id="rId1" Type="${REL_NS}/officeDocument" Target="xl/workbook.xml"/></Relationships>`;

// Order of cellXfs entries must match STYLES. fills[0] and fills[1] are the two fills Excel reserves.
const STYLES_XML = `${XML_HEADER}<styleSheet xmlns="${MAIN_NS}">`
  + '<numFmts count="1"><numFmt numFmtId="164" formatCode="yyyy-mm-dd"/></numFmts>'
  + '<fonts count="2"><font><sz val="11"/><name val="Calibri"/><family val="2"/></font><font><b/><sz val="11"/><name val="Calibri"/><family val="2"/></font></fonts>'
  + '<fills count="4"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill>'
  + '<fill><patternFill patternType="solid"><fgColor rgb="FFE7EDF5"/><bgColor indexed="64"/></patternFill></fill>'
  + '<fill><patternFill patternType="solid"><fgColor rgb="FFCBD6E6"/><bgColor indexed="64"/></patternFill></fill></fills>'
  + '<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>'
  + '<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>'
  + '<cellXfs count="7">'
  + '<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>'
  + '<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0" applyAlignment="1"><alignment vertical="top"/></xf>'
  + '<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0" applyAlignment="1"><alignment vertical="top" wrapText="1"/></xf>'
  + '<xf numFmtId="3" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1" applyAlignment="1"><alignment vertical="top"/></xf>'
  + '<xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1" applyAlignment="1"><alignment horizontal="left" vertical="top"/></xf>'
  + '<xf numFmtId="0" fontId="1" fillId="2" borderId="0" xfId="0" applyFont="1" applyFill="1" applyAlignment="1"><alignment vertical="center" wrapText="1"/></xf>'
  + '<xf numFmtId="0" fontId="1" fillId="3" borderId="0" xfId="0" applyFont="1" applyFill="1" applyAlignment="1"><alignment horizontal="center" vertical="center"/></xf>'
  + "</cellXfs>"
  + '<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>'
  + "</styleSheet>";

/**
 * @typedef {object} CellSpec
 * @property {"text"|"wrap"|"number"|"date"|"header"|"group"} kind - How the cell is written and styled; a date's value is a serial number.
 * @property {unknown} value - Cell value; null, undefined, and "" leave the cell empty.
 */

/**
 * @typedef {object} WorksheetSpec
 * @property {string} name - Sheet tab name, already passed through {@link sheetName}.
 * @property {{width: number, hidden?: boolean}[]} columns - Column widths in characters, in order.
 * @property {(CellSpec|null)[][]} rows - Rows from the top; a null cell is left empty.
 * @property {string[]} [merges] - Merged ranges such as "A1:C1".
 * @property {number} [frozenRows] - Number of rows kept visible while scrolling.
 * @property {number} [filterRow] - One-based row that carries the autofilter buttons.
 */

/**
 * Lists the parts of a workbook package in archive order.
 *
 * @param {WorksheetSpec[]} sheets - Worksheets in tab order.
 * @returns {{name: string, data: string}[]} Package part names and XML.
 */
export function workbookParts(sheets) {
  return [
    { name: "[Content_Types].xml", data: contentTypesXml(sheets) },
    { name: "_rels/.rels", data: ROOT_RELS },
    { name: "xl/workbook.xml", data: workbookXml(sheets) },
    { name: "xl/_rels/workbook.xml.rels", data: workbookRelsXml(sheets) },
    { name: "xl/styles.xml", data: STYLES_XML },
    ...sheets.map((sheet, index) => ({ name: `xl/worksheets/sheet${index + 1}.xml`, data: worksheetXml(sheet) })),
  ];
}

/**
 * Builds an .xlsx file.
 *
 * @param {WorksheetSpec[]} sheets - Worksheets in tab order.
 * @param {Date} [modified=new Date()] - Modification time recorded in the archive.
 * @returns {Uint8Array} The workbook bytes.
 */
export function buildWorkbook(sheets, modified = new Date()) {
  return createZip(workbookParts(sheets), modified);
}
