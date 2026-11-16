/**
 * @fileoverview Builds the board export: an Excel workbook with one sheet of processes, under
 * numbered group headers, and one sheet of interview questions. Headers and stored values
 * (status, work arrangement, employment type, default stage titles) use the selected language;
 * text the user entered is exported as written.
 */

import { t } from "../i18n.js";
import { arrangementLabel, companyText, employmentTypeLabel, stageName, stageTitleLabel, stageTypeLabel, statusLabel } from "./labels.js";
import { markdownToText } from "./markdownText.js";
import { buildWorkbook, columnName, dateSerial, sheetName } from "./xlsx.js";

/** MIME type of an .xlsx workbook. */
export const XLSX_MIME = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet";

/** Returns a local calendar date as YYYY-MM-DD. */
function isoDate(date) {
  const pad = (value) => String(value).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

/** Converts Unix seconds to the serial number of its local calendar date, or null when unset. */
function timestampSerial(seconds) {
  const value = Number(seconds);
  if (!seconds || !Number.isFinite(value)) return null;
  const date = new Date(value * 1000);
  return dateSerial(date.getFullYear(), date.getMonth() + 1, date.getDate());
}

/** Converts a stored YYYY-MM-DD date to a serial number; any other value is kept as text. */
function calendarSerial(value) {
  const parts = String(value ?? "").match(/^(\d{4})-(\d{2})-(\d{2})$/);
  return parts ? dateSerial(Number(parts[1]), Number(parts[2]), Number(parts[3])) : value;
}

function yesNo(value) {
  return t(value ? "workbook.yes" : "workbook.no");
}

/** Returns the translated name of a job's current stage. */
function currentStage(job) {
  if (!job.current_stage_title) return "";
  return job.current_stage_title_is_type ? stageTypeLabel(job.current_stage_title) : stageTitleLabel(job.current_stage_title);
}

const column = (key, kind, width, value) => ({ key, kind, width, value });

// Each group spans its columns in order; together they cover columns A to Z.
const BOARD_GROUPS = [
  ["workbook.group.job", [
    column("workbook.company", "text", 24, (job) => companyText(job.company_name)),
    column("workbook.role", "text", 28, (job) => job.position_title),
    column("workbook.description", "wrap", 70, (job) => markdownToText(job.company_overview)),
    column("workbook.employmentType", "text", 18, (job) => employmentTypeLabel(job.employment_type)),
    column("workbook.workArrangement", "text", 16, (job) => arrangementLabel(job.work_arrangement)),
    column("workbook.salaryMin", "number", 16, (job) => job.salary_min),
    column("workbook.salaryMax", "number", 16, (job) => job.salary_max),
    column("workbook.currency", "text", 10, (job) => job.salary_currency),
    column("workbook.expectedSalary", "wrap", 22, (job) => job.expected_salary),
    column("workbook.technologies", "wrap", 28, (job) => (job.technologies || []).map((technology) => technology.name).join("; ")),
    column("workbook.jobPostLink", "text", 40, (job) => job.job_post_url),
  ]],
  ["workbook.group.process", [
    column("workbook.status", "text", 12, (job) => statusLabel(job.status)),
    column("workbook.currentStage", "text", 24, currentStage),
    column("workbook.searchPeriod", "text", 20, (job, periods) => periods.get(job.search_period_id)),
    column("workbook.createdDate", "date", 12, (job) => timestampSerial(job.created_at)),
    column("workbook.statusChangedDate", "date", 14, (job) => timestampSerial(job.status_changed_at)),
    column("workbook.updatedDate", "date", 14, (job) => timestampSerial(job.updated_at)),
  ]],
  ["workbook.group.application", [
    column("workbook.cvVersion", "number", 10, (job) => job.selected_cv_version?.version_number),
    column("workbook.cvFile", "text", 24, (job) => job.selected_cv_version?.original_name),
    column("workbook.cvSentDate", "date", 14, (job) => calendarSerial(job.application_sent_date)),
    column("workbook.applicationLink", "text", 40, (job) => job.application_url),
    column("workbook.referral", "text", 12, (job) => yesNo(job.is_referral)),
  ]],
  ["workbook.group.recruiter", [
    column("workbook.recruiterName", "text", 22, (job) => job.recruiter_name),
    column("workbook.recruiterAgency", "text", 22, (job) => job.recruiter_agency),
    column("workbook.recruiterContact", "wrap", 28, (job) => job.recruiter_contact),
    column("workbook.recruiterContactDate", "date", 14, (job) => calendarSerial(job.recruiter_first_contact_date)),
  ]],
];
const BOARD_COLUMNS = BOARD_GROUPS.flatMap(([, columns]) => columns);

const QUESTION_COLUMNS = [
  column("workbook.company", "text", 24, ({ job }) => companyText(job.company_name)),
  column("workbook.role", "text", 28, ({ job }) => job.position_title),
  column("workbook.stage", "text", 24, ({ stage }) => stageName(stage)),
  column("workbook.question", "wrap", 50, ({ question }) => question.question),
  column("workbook.answerNotes", "wrap", 60, ({ question }) => question.answer_notes),
  column("workbook.asked", "text", 12, ({ question }) => yesNo(question.is_asked)),
  // Identifiers let an edited sheet be matched back to its questions; they stay hidden.
  { ...column("workbook.processId", "text", 38, ({ job }) => job.id), hidden: true },
  { ...column("workbook.stageId", "text", 38, ({ stage }) => stage.id), hidden: true },
  { ...column("workbook.questionId", "text", 38, ({ question }) => question.id), hidden: true },
];

const headerCell = (value) => ({ kind: "header", value });

/** Returns the numbered group header row and the merged range of each group. */
function groupHeaders() {
  const cells = [];
  const merges = [];
  BOARD_GROUPS.forEach(([key, columns], index) => {
    const start = cells.length;
    cells.push({ kind: "group", value: `${index + 1} ${t(key)}` }, ...columns.slice(1).map(() => ({ kind: "group", value: "" })));
    merges.push(`${columnName(start)}1:${columnName(cells.length - 1)}1`);
  });
  return { cells, merges };
}

function columnHeaders() {
  return BOARD_GROUPS.flatMap(([, columns], group) => columns.map((item, index) => headerCell(`${group + 1}.${index + 1} ${t(item.key)}`)));
}

/**
 * Lays out the board sheet: numbered, merged group headers, numbered column headers, and one row per job.
 *
 * @param {object[]} jobs - Job processes, in the order to export.
 * @param {object[]} [searchPeriods=[]] - Search periods, used to name each job's period.
 * @returns {import("./xlsx.js").WorksheetSpec} The sheet.
 */
export function boardSheet(jobs, searchPeriods = []) {
  const periods = new Map(searchPeriods.map((period) => [period.id, period.name]));
  const groups = groupHeaders();
  const rows = jobs.map((job) => BOARD_COLUMNS.map(({ kind, value }) => ({ kind, value: value(job, periods) })));
  return {
    name: sheetName(t("workbook.sheetBoard")),
    columns: BOARD_COLUMNS,
    rows: [groups.cells, columnHeaders(), ...rows],
    merges: groups.merges,
    frozenRows: 2,
    filterRow: 2,
  };
}

function byOrder(left, right) {
  return (left.order_index ?? 0) - (right.order_index ?? 0);
}

/** Lists every question as its job, stage, and question, in job, stage, then question order. */
function questionEntries(jobs, stagesByJob) {
  return jobs.flatMap((job) => [...(stagesByJob.get(job.id) || [])].sort(byOrder)
    .flatMap((stage) => [...(stage.questions || [])].sort(byOrder).map((question) => ({ job, stage, question }))));
}

/**
 * Lays out the interview questions sheet: one row per question, with hidden job, stage, and question IDs.
 *
 * @param {object[]} jobs - Job processes, in the order to export.
 * @param {Map<string, object[]>} stagesByJob - Each job's stages, with their questions, by job ID.
 * @returns {import("./xlsx.js").WorksheetSpec} The sheet.
 */
export function questionsSheet(jobs, stagesByJob) {
  const rows = questionEntries(jobs, stagesByJob).map((entry) => QUESTION_COLUMNS.map(({ kind, value }) => ({ kind, value: value(entry) })));
  return {
    name: sheetName(t("workbook.sheetQuestions")),
    columns: QUESTION_COLUMNS,
    rows: [QUESTION_COLUMNS.map((item) => headerCell(t(item.key))), ...rows],
    frozenRows: 1,
    filterRow: 1,
  };
}

/** Revokes attachment links created in this browser, which the browser demo adds and the export never uses. */
function releaseAttachmentUrls(job) {
  for (const attachment of job.attachments || []) {
    if (String(attachment.download_url || "").startsWith("blob:")) URL.revokeObjectURL(attachment.download_url);
  }
}

/**
 * Loads each job's stages and questions, a few jobs at a time.
 *
 * @param {object[]} jobs - Jobs to load.
 * @param {(id: string) => Promise<object>} loadJob - Returns a job with its stages.
 * @param {number} [concurrency=4] - Most requests in flight at once.
 * @returns {Promise<Map<string, object[]>>} Each job's stages by job ID.
 */
export async function loadJobStages(jobs, loadJob, concurrency = 4) {
  const stages = new Map();
  let next = 0;
  const worker = async () => {
    while (next < jobs.length) {
      const job = jobs[next];
      next += 1;
      const loaded = await loadJob(job.id);
      releaseAttachmentUrls(loaded);
      stages.set(job.id, loaded.stages || []);
    }
  };
  await Promise.all(Array.from({ length: Math.min(concurrency, jobs.length) }, worker));
  return stages;
}

/**
 * Builds the board workbook: a board sheet and an interview questions sheet.
 *
 * @param {object[]} jobs - Job processes, in the order to export.
 * @param {object[]} searchPeriods - Search periods, used to name each job's period.
 * @param {Map<string, object[]>} stagesByJob - Each job's stages, with their questions, by job ID.
 * @param {Date} [date=new Date()] - Export time.
 * @returns {Uint8Array} The .xlsx file.
 */
export function buildBoardWorkbook(jobs, searchPeriods, stagesByJob, date = new Date()) {
  return buildWorkbook([boardSheet(jobs, searchPeriods), questionsSheet(jobs, stagesByJob)], date);
}

/**
 * Names the export after the local date, e.g. war-room-board-2026-11-13.xlsx.
 *
 * @param {Date} [date=new Date()] - Export date.
 * @returns {string} The download filename.
 */
export function boardWorkbookFilename(date = new Date()) {
  return `war-room-board-${isoDate(date)}.xlsx`;
}
