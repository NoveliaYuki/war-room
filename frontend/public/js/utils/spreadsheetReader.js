/**
 * @fileoverview Reads a spreadsheet chosen for import: an .xlsx workbook saved by War Room, Excel,
 * Google Sheets, Numbers, or LibreOffice, or a comma-, semicolon-, or tab-separated text file.
 * Cells keep their stored type (number, text, or true/false); empty cells are null.
 */

import { isZip, openZip, readError } from "./unzip.js";

/** Largest file accepted for import, in bytes. */
export const MAX_SPREADSHEET_BYTES = 20 * 1024 * 1024;
/** Most rows read from one sheet. */
export const MAX_SHEET_ROWS = 10000;
/** Columns past this one are ignored. */
export const MAX_SHEET_COLUMNS = 256;

const OFFICE_DOCUMENT = /\/officeDocument$/;
const SHARED_STRINGS = /\/sharedStrings$/;
// The 1904 date system counts days from 1904-01-01, 1462 days after the 1900 system's epoch.
const DATE_1904_OFFSET = 1462;
const DELIMITERS = [",", ";", "\t"];

/**
 * @typedef {object} SpreadsheetData
 * @property {number} dateOffset - Days to add to a date cell's number to read it in the 1900 date system.
 * @property {{name: string, rows: (string|number|boolean|null)[][]}[]} sheets - Sheets in tab order.
 */

/**
 * Parses an XML part, refusing document type declarations so entities cannot expand.
 *
 * @param {string} text - XML source.
 * @returns {Document} The parsed document.
 */
export function parseXml(text) {
  if (/<!DOCTYPE/i.test(text)) throw readError("unreadable");
  const document = new DOMParser().parseFromString(text, "application/xml");
  if (document.getElementsByTagName("parsererror").length) throw readError("unreadable");
  return document;
}

/** Returns an element's name without a namespace prefix, also from parsers that keep the prefix in localName. */
function baseName(element) {
  return element.localName.replace(/^[^:]*:/, "");
}

/** Lists descendant elements by name, so prefixed, unprefixed, and Strict OOXML parts read alike. */
function elements(node, name) {
  return Array.from(node.getElementsByTagName("*")).filter((element) => baseName(element) === name);
}

/** Joins a cell's or shared string's text runs, leaving out phonetic guides. */
function richText(node) {
  return elements(node, "t").filter((text) => baseName(text.parentNode) !== "rPh").map((text) => text.textContent).join("");
}

/** Resolves a relationship target against the part that refers to it. */
function resolvePart(source, target) {
  const parts = target.startsWith("/") ? [] : source.split("/").slice(0, -1);
  for (const segment of target.split("/")) {
    if (segment === "..") parts.pop();
    else if (segment && segment !== ".") parts.push(segment);
  }
  return parts.join("/");
}

async function relationships(zip, part) {
  const relsPart = part.replace(/([^/]*)$/, "_rels/$1.rels");
  if (!zip.has(relsPart)) return [];
  return elements(parseXml(await zip.text(relsPart)), "Relationship").map((relationship) => ({
    id: relationship.getAttribute("Id"),
    type: relationship.getAttribute("Type") || "",
    target: resolvePart(part, relationship.getAttribute("Target") || ""),
  }));
}

/** Returns a sheet's relationship ID (r:id), whichever prefix its namespace uses. */
function relationshipId(sheet) {
  return Array.from(sheet.attributes).find((attribute) => /^[^:]+:id$/.test(attribute.name))?.value;
}

async function sharedStrings(zip, rels) {
  const part = rels.find((relationship) => SHARED_STRINGS.test(relationship.type))?.target;
  if (!part || !zip.has(part)) return [];
  return elements(parseXml(await zip.text(part)), "si").map(richText);
}

/** Returns the zero-based column of a reference such as "AA12", or fallback when it has none. */
function referenceColumn(reference, fallback) {
  const letters = /^([A-Z]+)\d*$/i.exec(reference || "")?.[1];
  if (!letters) return fallback;
  return [...letters.toUpperCase()].reduce((index, letter) => index * 26 + letter.charCodeAt(0) - 64, 0) - 1;
}

/** Returns the zero-based row of a reference such as "12", or fallback when it has none. */
function referenceRow(reference, fallback) {
  const row = Number.parseInt(reference || "", 10);
  return Number.isInteger(row) && row > 0 ? row - 1 : fallback;
}

const CELL_READERS = {
  s: (raw, strings) => strings[Number(raw)] ?? null,
  b: (raw) => raw === "1" || raw === "true",
  e: () => null,
  n: (raw) => (raw.trim() !== "" && Number.isFinite(Number(raw)) ? Number(raw) : raw),
};

/** Reads a cell's value: a formula's cached result, shared or inline text, a number, or true/false. */
function cellValue(cell, strings) {
  const type = cell.getAttribute("t") || "n";
  if (type === "inlineStr") return richText(cell);
  const value = elements(cell, "v")[0];
  if (!value) return null;
  const read = CELL_READERS[type] || String;
  return read(value.textContent, strings);
}

function rowCells(row, strings) {
  const cells = [];
  let column = -1;
  for (const cell of elements(row, "c")) {
    column = referenceColumn(cell.getAttribute("r"), column + 1);
    const value = cellValue(cell, strings);
    if (column < MAX_SHEET_COLUMNS && value !== null && value !== "") cells[column] = value;
  }
  return Array.from(cells, (value) => value ?? null);
}

function worksheetRows(document, strings) {
  const rows = [];
  let index = -1;
  for (const row of elements(document, "row")) {
    index = referenceRow(row.getAttribute("r"), index + 1);
    const cells = rowCells(row, strings);
    if (!cells.length) continue;
    if (index >= MAX_SHEET_ROWS) throw readError("tooLarge");
    rows[index] = cells;
  }
  return Array.from(rows, (row) => row || []);
}

function uses1904Dates(workbook) {
  const flag = elements(workbook, "workbookPr")[0]?.getAttribute("date1904");
  return flag === "1" || flag === "true";
}

/**
 * Reads every worksheet of an .xlsx workbook.
 *
 * @param {Uint8Array} bytes - Workbook bytes.
 * @returns {Promise<SpreadsheetData>} The sheets in tab order.
 */
export async function readWorkbook(bytes) {
  const zip = openZip(bytes);
  const workbookPart = (await relationships(zip, "")).find((relationship) => OFFICE_DOCUMENT.test(relationship.type))?.target || "xl/workbook.xml";
  const workbook = parseXml(await zip.text(workbookPart));
  const rels = await relationships(zip, workbookPart);
  const strings = await sharedStrings(zip, rels);
  const targets = new Map(rels.map((relationship) => [relationship.id, relationship.target]));
  const sheets = [];
  for (const sheet of elements(workbook, "sheet")) {
    const target = targets.get(relationshipId(sheet));
    if (target && zip.has(target)) sheets.push({ name: sheet.getAttribute("name") || "", rows: worksheetRows(parseXml(await zip.text(target)), strings) });
  }
  return { dateOffset: uses1904Dates(workbook) ? DATE_1904_OFFSET : 0, sheets };
}

/**
 * Picks the separator a delimited text file uses from its first line: comma, semicolon (which Excel
 * writes in Spanish and Catalan), or tab.
 *
 * @param {string} text - File text.
 * @returns {string} The separator.
 */
export function detectDelimiter(text) {
  const header = text.slice(0, 65536).replace(/"(?:[^"]|"")*"/g, "").split(/\r\n|\n|\r/, 1)[0];
  const counts = DELIMITERS.map((delimiter) => header.split(delimiter).length);
  return DELIMITERS[counts.indexOf(Math.max(...counts))];
}

function fieldPattern(delimiter) {
  const separator = delimiter === "\t" ? "\\t" : delimiter;
  return new RegExp(`(?:"((?:[^"]|"")*)"|([^${separator}\\r\\n]*))(${separator}|\\r\\n|\\n|\\r|$)`, "y");
}

/**
 * Splits delimited text into rows of cells. Quoted cells may hold separators, doubled quotes, and line breaks.
 *
 * @param {string} text - File text.
 * @param {string} [delimiter] - Separator; detected from the first line when omitted.
 * @returns {(string|null)[][]} Rows of cells; empty cells are null.
 */
export function parseDelimited(text, delimiter = detectDelimiter(text)) {
  const pattern = fieldPattern(delimiter);
  const rows = [];
  let row = [];
  while (pattern.lastIndex < text.length) {
    const [, quoted, plain, end] = pattern.exec(text);
    const value = quoted === undefined ? plain : quoted.replace(/""/g, '"').replace(/\r\n?/g, "\n");
    row.push(value === "" ? null : value);
    if (end === delimiter) continue;
    rows.push(row);
    row = [];
    if (rows.length > MAX_SHEET_ROWS) throw readError("tooLarge");
  }
  if (row.length) rows.push([...row, null]);
  return rows;
}

/**
 * Decodes a text file: UTF-16 with a byte order mark, UTF-8, or Windows-1252 (older Excel CSV files).
 *
 * @param {Uint8Array} bytes - File contents.
 * @returns {string} The text, without a byte order mark.
 */
export function decodeText(bytes) {
  if (bytes[0] === 0xff && bytes[1] === 0xfe) return new TextDecoder("utf-16le").decode(bytes);
  if (bytes[0] === 0xfe && bytes[1] === 0xff) return new TextDecoder("utf-16be").decode(bytes);
  try {
    return new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  } catch {
    return new TextDecoder("windows-1252").decode(bytes);
  }
}

/** Returns whether bytes look like a binary file, such as an old .xls workbook, rather than text. */
function looksBinary(bytes) {
  const utf16 = (bytes[0] === 0xff && bytes[1] === 0xfe) || (bytes[0] === 0xfe && bytes[1] === 0xff);
  return !utf16 && bytes.subarray(0, 4096).includes(0);
}

/**
 * Reads a chosen spreadsheet file: an .xlsx workbook, or delimited text as one sheet.
 *
 * @param {Blob & {name?: string}} file - The chosen file.
 * @returns {Promise<SpreadsheetData>} The sheets in tab order.
 */
export async function readSpreadsheet(file) {
  if (file.size > MAX_SPREADSHEET_BYTES) throw readError("tooLarge");
  const bytes = new Uint8Array(await file.arrayBuffer());
  if (isZip(bytes)) return readWorkbook(bytes);
  if (looksBinary(bytes)) throw readError("unreadable");
  return { dateOffset: 0, sheets: [{ name: file.name || "", rows: parseDelimited(decodeText(bytes)) }] };
}
