/**
 * @fileoverview Builds a spreadsheet-ready CSV file of the board's job processes.
 * Headers and stored values (status, work arrangement, employment type, stage type)
 * use the selected language; text the user entered is exported as written.
 */

import { t } from "../i18n.js";
import { arrangementLabel, companyText, employmentTypeLabel, stageTitleLabel, stageTypeLabel, statusLabel } from "./labels.js";

// Spreadsheets run a cell that starts with one of these as a formula; imported postings are untrusted.
const FORMULA_PREFIXES = new Set(["=", "+", "-", "@", "\t", "\r"]);
const NEEDS_QUOTES = /[",\r\n]/;

/** Returns a local calendar date as YYYY-MM-DD. */
function isoDate(date) {
  const pad = (value) => String(value).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

/** Converts Unix seconds to a local YYYY-MM-DD date, or "" when unset. */
function timestampDate(seconds) {
  const value = Number(seconds);
  return seconds && Number.isFinite(value) ? isoDate(new Date(value * 1000)) : "";
}

/** Returns the translated name of a job's current stage. */
function currentStage(job) {
  if (!job.current_stage_title) return "";
  return job.current_stage_title_is_type ? stageTypeLabel(job.current_stage_title) : stageTitleLabel(job.current_stage_title);
}

const COLUMNS = [
  ["csv.company", (job) => companyText(job.company_name)],
  ["csv.role", (job) => job.position_title],
  ["csv.status", (job) => statusLabel(job.status)],
  ["csv.currentStage", currentStage],
  ["csv.workArrangement", (job) => arrangementLabel(job.work_arrangement)],
  ["csv.employmentType", (job) => employmentTypeLabel(job.employment_type)],
  ["csv.salaryMin", (job) => job.salary_min],
  ["csv.salaryMax", (job) => job.salary_max],
  ["csv.currency", (job) => job.salary_currency],
  ["csv.referral", (job) => t(job.is_referral ? "csv.yes" : "csv.no")],
  ["csv.recruiter", (job) => job.recruiter_name],
  ["csv.recruiterAgency", (job) => job.recruiter_agency],
  ["csv.recruiterContact", (job) => job.recruiter_contact],
  ["csv.technologies", (job) => (job.technologies || []).map((technology) => technology.name).join("; ")],
  ["csv.cvVersion", (job) => job.selected_cv_version?.version_number],
  ["csv.cvFile", (job) => job.selected_cv_version?.original_name],
  ["csv.cvSentDate", (job) => job.application_sent_date],
  ["csv.recruiterContactDate", (job) => job.recruiter_first_contact_date],
  ["csv.searchPeriod", (job, periods) => periods.get(job.search_period_id)],
  ["csv.jobPostLink", (job) => job.job_post_url],
  ["csv.applicationLink", (job) => job.application_url],
  ["csv.createdDate", (job) => timestampDate(job.created_at)],
  ["csv.statusChangedDate", (job) => timestampDate(job.status_changed_at)],
  ["csv.updatedDate", (job) => timestampDate(job.updated_at)],
];

/**
 * Escapes one CSV cell: neutralizes spreadsheet formulas, then applies RFC 4180 quoting.
 *
 * @param {unknown} value - Cell value; null and undefined become empty cells.
 * @returns {string} The encoded cell.
 */
export function csvCell(value) {
  let text = value == null ? "" : String(value);
  if (FORMULA_PREFIXES.has(text.charAt(0))) text = `'${text}`;
  return NEEDS_QUOTES.test(text) ? `"${text.replaceAll('"', '""')}"` : text;
}

/**
 * Builds the board CSV: UTF-8 byte order mark, a translated header row, and one CRLF-terminated row per job.
 *
 * @param {object[]} jobs - Job processes as returned by the jobs list.
 * @param {object[]} [searchPeriods=[]] - Search periods, used to name each job's period.
 * @returns {string} The CSV document.
 */
export function buildBoardCsv(jobs, searchPeriods = []) {
  const periods = new Map(searchPeriods.map((period) => [period.id, period.name]));
  const rows = [COLUMNS.map(([header]) => t(header))];
  for (const job of jobs) rows.push(COLUMNS.map(([, value]) => value(job, periods)));
  return `﻿${rows.map((row) => row.map(csvCell).join(",")).join("\r\n")}\r\n`;
}

/**
 * Names the export after the local date, e.g. war-room-board-2026-11-13.csv.
 *
 * @param {Date} [date=new Date()] - Export date.
 * @returns {string} The download filename.
 */
export function boardCsvFilename(date = new Date()) {
  return `war-room-board-${isoDate(date)}.csv`;
}
