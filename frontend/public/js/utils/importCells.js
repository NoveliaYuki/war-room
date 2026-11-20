/**
 * @fileoverview Reads spreadsheet cells back into stored values for an import, and tells whether a
 * cell still holds what the export wrote. Labels such as statuses and Yes/No are read in every
 * supported language, whichever language the file was exported in.
 */

import { LANGUAGES, t, withLanguage } from "../i18n.js";
import { LABEL_KEYS } from "./labels.js";
import { cellText, dateSerial } from "./xlsx.js";

const ISO_DATE = /^(\d{4})-(\d{2})-(\d{2})(?:T[\d:.]+Z?)?$/;
// Excel's last date, 9999-12-31.
const MAX_DATE_SERIAL = 2958465;

/**
 * Returns a cell's text: numbers and true/false read as usual, empty cells as "".
 *
 * @param {unknown} value - Cell value.
 * @returns {string} The text.
 */
export function textOf(value) {
  return value == null ? "" : String(value);
}

/**
 * Normalizes text for comparison and saving: as a cell stores it, without trailing spaces on its lines, trimmed.
 *
 * @param {unknown} value - Cell value.
 * @returns {string} The normalized text.
 */
export function normalizeText(value) {
  return cellText(textOf(value)).replace(/[ \t]+$/gm, "").trim();
}

/**
 * Returns whether a cell is empty or holds only whitespace.
 *
 * @param {unknown} value - Cell value.
 * @returns {boolean} True when there is nothing to read.
 */
export function isBlank(value) {
  return normalizeText(value) === "";
}

/**
 * Folds a label or name for matching: lower case, with runs of whitespace as one space.
 *
 * @param {unknown} value - Cell value.
 * @returns {string} The folded text.
 */
export function foldText(value) {
  return normalizeText(value).replace(/\s+/g, " ").toLowerCase();
}

/** Turns "70.000", "70,000", "72,5", or "85,000.40" into text Number reads: groups of three after a dot or comma are thousands. */
function decimalText(digits) {
  if (/^\d{1,3}([.,])\d{3}(?:\1\d{3})*$/.test(digits)) return digits.replace(/[.,]/g, "");
  const point = Math.max(digits.lastIndexOf("."), digits.lastIndexOf(","));
  if (point < 0) return digits;
  return `${digits.slice(0, point).replace(/[.,]/g, "")}.${digits.slice(point + 1)}`;
}

/**
 * Reads a number from a numeric cell or from text such as "60000", "60,000", "60.000 €", or "72.5k".
 *
 * @param {unknown} value - Cell value.
 * @returns {number|null} The number, null for an empty cell, or NaN when the cell is not a number.
 */
export function amountOf(value) {
  if (typeof value === "number") return value;
  if (isBlank(value)) return null;
  const match = /^([\d.,]+)(k?)$/.exec(foldText(value).replace(/[\s'€$£]/g, ""));
  if (!match || !/\d/.test(match[1])) return Number.NaN;
  const amount = Number(decimalText(match[1]));
  return match[2] ? amount * 1000 : amount;
}

/** Returns the ISO date of a 1900-system serial number, or null outside Excel's date range. */
function serialDate(serial) {
  const day = Math.floor(serial);
  if (!(day >= 1 && day <= MAX_DATE_SERIAL)) return null;
  return new Date(Date.UTC(1899, 11, 30 + day)).toISOString().slice(0, 10);
}

/**
 * Reads a date from a date cell's serial number or from YYYY-MM-DD text.
 *
 * @param {unknown} value - Cell value.
 * @param {number} [dateOffset=0] - Days to add to a serial number from a 1904-system workbook.
 * @returns {string|null} The YYYY-MM-DD date, "" for an empty cell, or null when the cell is not a date.
 */
export function isoDateOf(value, dateOffset = 0) {
  if (isBlank(value)) return "";
  if (typeof value === "number") return serialDate(value + dateOffset);
  const parts = ISO_DATE.exec(normalizeText(value));
  if (!parts) return null;
  const iso = `${parts[1]}-${parts[2]}-${parts[3]}`;
  return serialDate(dateSerial(Number(parts[1]), Number(parts[2]), Number(parts[3]))) === iso ? iso : null;
}

/** Returns a date cell as comparable text: its ISO date when it holds one, otherwise its text. */
function dateText(value, dateOffset) {
  return isoDateOf(value, dateOffset) ?? normalizeText(value);
}

function sameAmount(left, right) {
  return left === right || (left == null && right == null);
}

const SAME_CELL = {
  number: (cell, exported) => sameAmount(amountOf(cell), amountOf(exported)),
  date: (cell, exported, dateOffset) => dateText(cell, dateOffset) === dateText(exported, 0),
};

/**
 * Returns whether a cell still holds what the export wrote for it.
 *
 * @param {string} kind - The column's cell kind: "text", "wrap", "number", or "date".
 * @param {unknown} cell - Value read from the file.
 * @param {unknown} exported - Value the export writes for the saved record.
 * @param {number} [dateOffset=0] - Days to add to the file's date serial numbers.
 * @returns {boolean} True when the cell is unchanged.
 */
export function sameAsExported(kind, cell, exported, dateOffset = 0) {
  const same = SAME_CELL[kind];
  return same ? same(cell, exported, dateOffset) : normalizeText(cell) === normalizeText(exported);
}

/**
 * @typedef {object} LabelIndex
 * @property {Map<string, string>} status - Folded status labels to stored statuses.
 * @property {Map<string, string>} workArrangement - Folded labels to stored work arrangements.
 * @property {Map<string, string>} employmentType - Folded labels to stored employment types.
 * @property {Map<string, boolean>} yesNo - Folded Yes and No labels to true and false.
 * @property {Set<string>} unknownCompany - Folded names of the undisclosed company placeholder.
 */

function addLanguageLabels(index) {
  for (const [kind, keys] of Object.entries(LABEL_KEYS)) {
    for (const [stored, key] of Object.entries(keys)) index[kind].set(foldText(t(key)), stored).set(foldText(stored), stored);
  }
  // Job details call an accepted process approved.
  index.status.set(foldText(t("detail.statusApproved")), "accepted");
  index.yesNo.set(foldText(t("workbook.yes")), true).set(foldText(t("workbook.no")), false);
  index.unknownCompany.add(foldText(t("company.unknown")));
}

/**
 * Builds lookups from labels in every supported language, and from stored values, back to stored values.
 *
 * @returns {LabelIndex} The lookups.
 */
export function buildLabelIndex() {
  const index = {
    status: new Map(), workArrangement: new Map(), employmentType: new Map(),
    yesNo: new Map([["true", true], ["false", false]]), unknownCompany: new Set(["unknown"]),
  };
  for (const { code } of LANGUAGES) withLanguage(code, () => addLanguageLabels(index));
  return index;
}

const accept = (value) => ({ value });
const reject = (problem, params = {}) => ({ problem, params });

/**
 * Returns a reader for a labeled value, such as a status, in any supported language.
 *
 * @param {"status"|"workArrangement"|"employmentType"} kind - Which labels to read.
 * @param {string} [blankValue] - Value of an empty cell; when omitted, the cell is required.
 * @returns {CellReader} The reader.
 */
export function labelReader(kind, blankValue) {
  return (cell, context) => {
    if (isBlank(cell)) return blankValue === undefined ? reject("requiredValue") : accept(blankValue);
    const value = context.labels[kind].get(foldText(cell));
    return value === undefined ? reject("invalidValue", { value: normalizeText(cell) }) : accept(value);
  };
}

function readCompany(cell, context) {
  const name = normalizeText(cell);
  return accept(!name || context.labels.unknownCompany.has(foldText(name)) ? "Unknown" : name);
}

function readAmount(cell) {
  const amount = amountOf(cell);
  if (amount === null) return accept(null);
  return Number.isFinite(amount) && amount >= 0 ? accept(Math.round(amount)) : reject("invalidNumber", { value: normalizeText(cell) });
}

function readDate(cell, context) {
  const date = isoDateOf(cell, context.dateOffset);
  return date === null ? reject("invalidDate", { value: normalizeText(cell) }) : accept(date);
}

function readYesNo(cell, context) {
  if (typeof cell === "boolean") return accept(cell);
  if (isBlank(cell)) return accept(false);
  if (cell === 1 || cell === 0) return accept(cell === 1);
  const value = context.labels.yesNo.get(foldText(cell));
  return value === undefined ? reject("invalidValue", { value: normalizeText(cell) }) : accept(value);
}

/** Reads a "; "-separated tech stack. Any name missing from the catalog leaves the whole cell unread, so a typo never drops a technology. */
function readTechnologies(cell, context) {
  const names = normalizeText(cell).split(/[;\n]/).map((name) => name.trim()).filter(Boolean);
  const unknown = names.filter((name) => !context.technologies.has(foldText(name)));
  if (unknown.length) return reject("unknownTechnologies", { names: unknown.join(", ") });
  return accept([...new Set(names.map((name) => context.technologies.get(foldText(name)).id))]);
}

function readPeriod(cell, context) {
  if (isBlank(cell)) return accept("");
  const matches = context.periods.filter((period) => foldText(period.name) === foldText(cell));
  return matches.length === 1 ? accept(matches[0].id) : reject("unknownPeriod", { value: normalizeText(cell) });
}

function readCvVersion(cell, context) {
  const number = amountOf(cell);
  if (number === null) return accept(null);
  const version = context.cvVersions.find((item) => item.version_number === number);
  return version ? accept(version.id) : reject("unknownCvVersion", { value: normalizeText(cell) });
}

/**
 * @callback CellReader
 * @param {unknown} cell - Value read from the file.
 * @param {object} context - Labels, catalogs, and the file's date offset.
 * @returns {{value: unknown}|{problem: string, params: Record<string, string>}} The stored value, or why it cannot be read.
 */

/** Readers for each kind of importable cell. */
export const CELL_READERS = Object.freeze({
  text: (cell) => accept(normalizeText(cell)),
  required: (cell) => (isBlank(cell) ? reject("requiredValue") : accept(normalizeText(cell))),
  currency: (cell) => accept(normalizeText(cell).toUpperCase()),
  company: readCompany,
  amount: readAmount,
  date: readDate,
  yesNo: readYesNo,
  technologies: readTechnologies,
  period: readPeriod,
  cvVersion: readCvVersion,
});
