/**
 * @fileoverview Plans and applies a spreadsheet import: an edited board workbook, or a CSV saved from
 * its Board sheet, read back into War Room. Rows are matched to processes and questions by their hidden
 * IDs, and only cells that differ from what the export writes for the saved record are imported, so an
 * unedited file changes nothing in any language. Nothing missing from the file is deleted.
 */

import { LANGUAGES, getLanguage, withLanguage } from "../i18n.js";
import { BOARD_COLUMNS, QUESTION_COLUMNS, boardColumnTitles, loadJobStages, questionColumnTitles } from "./boardWorkbook.js";
import { CELL_READERS, buildLabelIndex, foldText, isBlank, isoDateOf, labelReader, normalizeText, sameAsExported } from "./importCells.js";
import { markdownToText } from "./markdownText.js";
import { readError } from "./unzip.js";

const PROCESS_ID = "workbook.processId";
const STAGE_ID = "workbook.stageId";
const QUESTION_ID = "workbook.questionId";
const LAST_UPDATED = "workbook.updatedDate";
// Matched to find a row's record, never compared or imported.
const ID_COLUMNS = new Set([PROCESS_ID, STAGE_ID, QUESTION_ID]);
// Show which process a question belongs to; a new question's row may hold anything here.
const QUESTION_CONTEXT = new Set(["workbook.company", "workbook.role", "workbook.stage"]);
const HEADER_SCAN_ROWS = 5;

const sameText = (left, right) => normalizeText(left) === normalizeText(right);
const sameIds = (left, right) => [...left].sort().join("\n") === [...right].sort().join("\n");
const field = (name, read, options = {}) => ({ field: name, read, ...options });

// Importable board columns and the job field each sets; any other column is read-only.
const BOARD_FIELDS = {
  "workbook.company": field("company_name", CELL_READERS.company),
  "workbook.role": field("position_title", CELL_READERS.required),
  "workbook.description": field("company_overview", CELL_READERS.text),
  "workbook.employmentType": field("employment_type", labelReader("employmentType", "unknown")),
  "workbook.workArrangement": field("work_arrangement", labelReader("workArrangement", "unknown")),
  "workbook.salaryMin": field("salary_min", CELL_READERS.amount),
  "workbook.salaryMax": field("salary_max", CELL_READERS.amount),
  "workbook.currency": field("salary_currency", CELL_READERS.currency),
  "workbook.expectedSalary": field("expected_salary", CELL_READERS.text),
  "workbook.technologies": field("technology_ids", CELL_READERS.technologies, {
    current: (job) => (job.technologies || []).map((technology) => technology.id), same: sameIds,
  }),
  "workbook.jobPostLink": field("job_post_url", CELL_READERS.text),
  "workbook.status": field("status", labelReader("status")),
  "workbook.searchPeriod": field("search_period_id", CELL_READERS.period),
  "workbook.cvVersion": field("cv_version_id", CELL_READERS.cvVersion, { current: (job) => job.selected_cv_version?.id ?? null }),
  "workbook.cvSentDate": field("application_sent_date", CELL_READERS.date),
  "workbook.applicationLink": field("application_url", CELL_READERS.text),
  "workbook.referral": field("is_referral", CELL_READERS.yesNo),
  "workbook.recruiterName": field("recruiter_name", CELL_READERS.text),
  "workbook.recruiterAgency": field("recruiter_agency", CELL_READERS.text),
  "workbook.recruiterContact": field("recruiter_contact", CELL_READERS.text),
  "workbook.recruiterContactDate": field("recruiter_first_contact_date", CELL_READERS.date),
};
const JOB_FIELDS = new Map(Object.values(BOARD_FIELDS).map((spec) => [spec.field, spec]));

const questionField = (name, read) => field(name, read, { current: (entry) => entry.question?.[name] });
const QUESTION_FIELDS = {
  "workbook.question": questionField("question", CELL_READERS.required),
  "workbook.answerNotes": questionField("answer_notes", CELL_READERS.text),
  "workbook.asked": questionField("is_asked", CELL_READERS.yesNo),
};

const SHEET_KINDS = [
  { kind: "board", columns: BOARD_COLUMNS, titles: boardColumnTitles, identifies: ["workbook.role", PROCESS_ID] },
  { kind: "questions", columns: QUESTION_COLUMNS, titles: questionColumnTitles, identifies: ["workbook.question"] },
];

function sameValue(spec, record, value) {
  const current = spec.current ? spec.current(record) : record[spec.field];
  return (spec.same || sameText)(current ?? "", value ?? "");
}

function addHeaderForm(forms, form, key, language) {
  const entry = forms.get(form) || { key, languages: new Set() };
  entry.languages.add(language);
  forms.set(form, entry);
}

/** Maps every header a sheet kind can have, numbered or not and in every language, to its column key and languages. */
function headerForms({ columns, titles }) {
  const forms = new Map();
  for (const { code } of LANGUAGES) {
    withLanguage(code, titles).forEach((title, position) => {
      const unnumbered = title.replace(/^\d+(?:\.\d+)?\s+/, "");
      for (const form of [title, unnumbered]) addHeaderForm(forms, foldText(form), columns[position].key, code);
    });
  }
  return forms;
}

function matchHeader(row, forms) {
  const columns = new Map();
  const votes = new Map();
  row.forEach((cell, index) => {
    const entry = forms.get(foldText(cell));
    if (!entry || columns.has(entry.key)) return;
    columns.set(entry.key, index);
    entry.languages.forEach((language) => votes.set(language, (votes.get(language) || 0) + 1));
  });
  return { columns, votes };
}

/** Picks the language most headers are written in, preferring the interface language on a tie. */
function headerLanguage(votes) {
  const top = Math.max(...votes.values());
  const leaders = LANGUAGES.map(({ code }) => code).filter((code) => votes.get(code) === top);
  return leaders.includes(getLanguage()) ? getLanguage() : leaders[0];
}

function isKnownSheet(kind, columns) {
  return columns.size >= 3 && kind.identifies.some((key) => columns.has(key));
}

/** Finds the header row that matches a sheet kind best within the first rows, or null. */
function recognizeSheet(sheet, kinds) {
  let best = null;
  sheet.rows.slice(0, HEADER_SCAN_ROWS).forEach((row, headerRow) => {
    for (const kind of kinds) {
      const match = matchHeader(row, kind.forms);
      if (isKnownSheet(kind, match.columns) && match.columns.size > (best?.columns.size ?? 0)) best = { ...match, kind: kind.kind, headerRow };
    }
  });
  return best && { kind: best.kind, headerRow: best.headerRow, columns: best.columns, language: headerLanguage(best.votes), rows: sheet.rows };
}

/**
 * Finds the first board sheet and the first interview questions sheet by their headers, in any
 * supported language, whatever the sheets are named.
 *
 * @param {{name: string, rows: unknown[][]}[]} sheets - Sheets in tab order.
 * @returns {{board?: object, questions?: object}} Each recognized sheet with its header row, columns, and language.
 */
export function recognizeSheets(sheets) {
  const kinds = SHEET_KINDS.map((kind) => ({ ...kind, forms: headerForms(kind) }));
  const found = {};
  for (const sheet of sheets) {
    const match = recognizeSheet(sheet, kinds);
    if (match && !found[match.kind]) found[match.kind] = match;
  }
  return found;
}

/** Lists a sheet's non-empty rows below its header with their one-based row numbers. */
function dataRows(sheet) {
  return sheet.rows.slice(sheet.headerRow + 1)
    .map((cells, index) => ({ cells, number: sheet.headerRow + index + 2 }))
    .filter(({ cells }) => cells.some((cell) => !isBlank(cell)));
}

function cellAt(row, sheet, key) {
  const index = sheet.columns.get(key);
  return index === undefined ? null : row.cells[index] ?? null;
}

function warning(sheet, row, problem, params = {}) {
  return { sheet, row: row.number, problem, params };
}

/** Reads one changed cell into the result: a new value, a problem, or an edit to a read-only column. */
function readCell(result, key, cell, record, spec) {
  const fieldSpec = spec.fields[key];
  if (!fieldSpec) {
    result.readOnly.push(key);
    return;
  }
  const read = fieldSpec.read(cell, spec.context);
  if (read.problem) result.problems.push({ ...read, column: key });
  else if (!sameValue(fieldSpec, record, read.value)) {
    result.changes[fieldSpec.field] = read.value;
    result.columns.push(key);
  }
}

/** Compares a row with a record, reading every cell that unchanged() does not skip. */
function rowChanges(row, spec, record, unchanged) {
  const result = { changes: {}, columns: [], problems: [], readOnly: [] };
  for (const [key, index] of spec.sheet.columns) {
    const cell = row.cells[index] ?? null;
    if (ID_COLUMNS.has(key) || unchanged(key, cell)) continue;
    readCell(result, key, cell, record, spec);
  }
  return result;
}

function addCellWarnings(warnings, sheetKind, row, result, includeReadOnly = true) {
  for (const problem of result.problems) warnings.push({ ...warning(sheetKind, row, problem.problem, problem.params), column: problem.column });
  if (includeReadOnly && result.readOnly.length) warnings.push({ ...warning(sheetKind, row, "readOnly"), columns: result.readOnly });
}

function boardSpec(sheet, context) {
  const columns = new Map(BOARD_COLUMNS.map((column) => [column.key, column]));
  const exported = (key, job) => withLanguage(sheet.language, () => columns.get(key).value(job, context.periodNames));
  const matchesExport = (key, cell, job) => sameAsExported(columns.get(key).kind, cell, exported(key, job), context.dateOffset);
  return { sheet, context, fields: BOARD_FIELDS, exported, matchesExport };
}

/** Returns whether the process changed in War Room after the file's Last updated date. */
function changedSinceExport(row, spec, job) {
  const fileDate = isoDateOf(cellAt(row, spec.sheet, LAST_UPDATED), spec.context.dateOffset);
  const savedDate = isoDateOf(spec.exported(LAST_UPDATED, job));
  return Boolean(fileDate && savedDate && fileDate < savedDate);
}

function nameKey(company, role, context) {
  const folded = foldText(company);
  return `${!folded || context.labels.unknownCompany.has(folded) ? "unknown" : folded}\n${foldText(role)}`;
}

function claim(job, row, seen) {
  if (seen.has(job.id)) return { problem: "duplicateProcess", params: { row: seen.get(job.id) } };
  seen.set(job.id, row.number);
  return { job };
}

/** Matches a row from a sheet without a Process ID column by its company and role. */
function processByName(row, sheet, context, seen) {
  const key = nameKey(cellAt(row, sheet, "workbook.company"), cellAt(row, sheet, "workbook.role"), context);
  const matches = context.jobs.filter((job) => nameKey(job.company_name, job.position_title, context) === key);
  return matches.length === 1 ? claim(matches[0], row, seen) : { problem: "noMatchingProcess" };
}

/** Finds a row's process: { job } to update, { job: null } to add, or { problem } to skip. */
function findProcess(row, sheet, context, seen) {
  if (!sheet.columns.has(PROCESS_ID)) return processByName(row, sheet, context, seen);
  const id = normalizeText(cellAt(row, sheet, PROCESS_ID));
  if (!id) return { job: null };
  const job = context.jobsById.get(id);
  return job ? claim(job, row, seen) : { problem: "unknownProcess" };
}

/** Returns whether an edited Description would replace an overview that has Markdown formatting the cell does not show. */
function dropsOverviewFormatting(job, changes) {
  return changes.company_overview !== undefined && normalizeText(job.company_overview) !== normalizeText(markdownToText(job.company_overview));
}

function planProcessUpdate(plan, row, job, spec) {
  const result = rowChanges(row, spec, job, (key, cell) => spec.matchesExport(key, cell, job));
  const stale = changedSinceExport(row, spec, job);
  addCellWarnings(plan.warnings, "board", row, result, !stale);
  if (!result.columns.length) return;
  if (dropsOverviewFormatting(job, result.changes)) plan.warnings.push({ ...warning("board", row, "overviewFormatting"), column: "workbook.description" });
  if (stale) plan.warnings.push(warning("board", row, "changedSinceExport"));
  plan.processUpdates.push({ row: row.number, job, changes: result.changes, columns: result.columns });
}

function planProcessCreate(plan, row, spec) {
  const result = rowChanges(row, spec, {}, (_key, cell) => isBlank(cell));
  if (!result.changes.position_title) {
    plan.warnings.push(warning("board", row, "roleRequired"));
    return;
  }
  addCellWarnings(plan.warnings, "board", row, result);
  plan.processCreates.push({ row: row.number, values: result.changes, columns: result.columns });
}

function planBoard(sheet, context) {
  const plan = { processUpdates: [], processCreates: [], warnings: [] };
  const spec = boardSpec(sheet, context);
  const seen = new Map();
  for (const row of dataRows(sheet)) {
    const target = findProcess(row, sheet, context, seen);
    if (target.problem) plan.warnings.push(warning("board", row, target.problem, target.params));
    else if (target.job) planProcessUpdate(plan, row, target.job, spec);
    else planProcessCreate(plan, row, spec);
  }
  return plan;
}

function questionIndex(context) {
  const questions = new Map();
  const stages = new Map();
  for (const [jobId, jobStages] of context.stagesByJob) {
    const job = context.jobsById.get(jobId);
    for (const stage of jobStages) {
      stages.set(stage.id, { job, stage });
      for (const question of stage.questions || []) questions.set(question.id, { job, stage, question });
    }
  }
  return { questions, stages };
}

/** Builds the question sheet comparison; context columns also match the company and role the board import sets. */
function questionSpec(sheet, context, plannedJobs) {
  const columns = new Map(QUESTION_COLUMNS.map((column) => [column.key, column]));
  const exported = (key, entry) => withLanguage(sheet.language, () => columns.get(key).value(entry));
  const matchesExport = (key, cell, entry) => [entry, { ...entry, job: plannedJobs.get(entry.job.id) || entry.job }]
    .some((candidate) => sameAsExported(columns.get(key).kind, cell, exported(key, candidate), context.dateOffset));
  return { sheet, context, fields: QUESTION_FIELDS, matchesExport };
}

function planNewQuestion(plan, row, spec, index) {
  const target = index.stages.get(normalizeText(cellAt(row, spec.sheet, STAGE_ID)));
  if (!target) {
    plan.warnings.push(warning("questions", row, "unknownStage"));
    return;
  }
  const result = rowChanges(row, spec, {}, (key, cell) => isBlank(cell) || QUESTION_CONTEXT.has(key));
  if (!result.changes.question) {
    plan.warnings.push(warning("questions", row, "questionRequired"));
    return;
  }
  addCellWarnings(plan.warnings, "questions", row, result);
  plan.questionCreates.push({ row: row.number, target, values: result.changes });
}

/** Finds a question row's saved question, or why the row is skipped. */
function findQuestion(id, row, index, seen) {
  if (seen.has(id)) return { problem: "duplicateQuestion", params: { row: seen.get(id) } };
  seen.set(id, row.number);
  const entry = index.questions.get(id);
  return entry ? { entry } : { problem: "unknownQuestion" };
}

function planQuestionUpdate(plan, row, spec, entry) {
  const result = rowChanges(row, spec, entry, (key, cell) => spec.matchesExport(key, cell, entry));
  addCellWarnings(plan.warnings, "questions", row, result);
  if (result.columns.length) plan.questionUpdates.push({ row: row.number, entry, changes: result.changes, columns: result.columns });
}

function planQuestionRow(plan, row, spec, index, seen) {
  const id = normalizeText(cellAt(row, spec.sheet, QUESTION_ID));
  const found = id ? findQuestion(id, row, index, seen) : null;
  if (!found) planNewQuestion(plan, row, spec, index);
  else if (found.problem) plan.warnings.push(warning("questions", row, found.problem, found.params));
  else planQuestionUpdate(plan, row, spec, found.entry);
}

function planQuestions(sheet, context, processUpdates) {
  const plan = { questionUpdates: [], questionCreates: [], warnings: [] };
  if (!sheet.columns.has(QUESTION_ID) || !sheet.columns.has(STAGE_ID)) {
    plan.warnings.push({ sheet: "questions", row: null, problem: "questionsWithoutIds", params: {} });
    return plan;
  }
  const plannedJobs = new Map(processUpdates.map(({ job, changes }) => [job.id, { ...job, ...changes }]));
  const spec = questionSpec(sheet, context, plannedJobs);
  const index = questionIndex(context);
  const seen = new Map();
  for (const row of dataRows(sheet)) planQuestionRow(plan, row, spec, index, seen);
  return plan;
}

function technologyIndex(technologies) {
  const index = new Map();
  for (const technology of technologies) {
    for (const term of [technology.name, ...(technology.aliases || [])]) index.set(foldText(term), technology);
  }
  return index;
}

/**
 * @typedef {object} ImportSource
 * @property {(status: string, search: string, options: {language: string}) => Promise<object[]>} getJobs - Lists every process.
 * @property {(id: string, options: {language: string}) => Promise<object>} getJob - Returns a process with its stages and questions.
 * @property {(options: {language: string}) => Promise<object[]>} getSearchPeriods - Lists search periods.
 * @property {() => Promise<object[]>} getTechnologies - Lists the technology catalog.
 * @property {() => Promise<object[]|{versions: object[]}>} getCvVersions - Lists CV versions.
 */

/** Loads the saved records, with the browser demo's sample text in the file's language. */
async function loadContext(source, sheets, dateOffset) {
  const language = (sheets.board || sheets.questions).language;
  const [jobs, periods, technologies, cvVersions] = await Promise.all([
    source.getJobs("all", "", { language }), source.getSearchPeriods({ language }), source.getTechnologies(), source.getCvVersions(),
  ]);
  const stagesByJob = sheets.questions ? await loadJobStages(jobs, (id) => source.getJob(id, { language })) : new Map();
  return {
    dateOffset, jobs, periods, stagesByJob,
    jobsById: new Map(jobs.map((job) => [job.id, job])),
    periodNames: new Map(periods.map((period) => [period.id, period.name])),
    technologies: technologyIndex(technologies),
    cvVersions: Array.isArray(cvVersions) ? cvVersions : cvVersions?.versions || [],
    labels: buildLabelIndex(),
  };
}

/**
 * @typedef {object} ImportPlan
 * @property {{row: number, job: object, changes: object, columns: string[]}[]} processUpdates - Saved processes with changed cells.
 * @property {{row: number, values: object, columns: string[]}[]} processCreates - Rows without a Process ID, added as new processes.
 * @property {{row: number, entry: object, changes: object, columns: string[]}[]} questionUpdates - Saved questions with changed cells.
 * @property {{row: number, target: object, values: object}[]} questionCreates - Rows without a Question ID, added to their stage.
 * @property {{sheet: "board"|"questions", row: number|null, problem: string, params: object, column?: string, columns?: string[]}[]} warnings
 *   Rows and cells that are not imported, and why.
 */

/**
 * Compares a spreadsheet with the saved data and lists what an import would change, without saving anything.
 *
 * @param {import("./spreadsheetReader.js").SpreadsheetData} spreadsheet - The file's sheets.
 * @param {ImportSource} source - Reads the saved data.
 * @returns {Promise<ImportPlan>} The changes and warnings.
 */
export async function buildImportPlan(spreadsheet, source) {
  const sheets = recognizeSheets(spreadsheet.sheets);
  if (!sheets.board && !sheets.questions) throw readError("noSheets");
  const context = await loadContext(source, sheets, spreadsheet.dateOffset);
  const board = sheets.board ? planBoard(sheets.board, context) : { processUpdates: [], processCreates: [], warnings: [] };
  const questions = sheets.questions ? planQuestions(sheets.questions, context, board.processUpdates) : { questionUpdates: [], questionCreates: [], warnings: [] };
  return { ...board, ...questions, warnings: [...board.warnings, ...questions.warnings] };
}

/**
 * Counts the records an import plan changes.
 *
 * @param {ImportPlan} plan - The plan.
 * @returns {number} Processes and questions to update or add.
 */
export function planSize(plan) {
  return plan.processUpdates.length + plan.processCreates.length + plan.questionUpdates.length + plan.questionCreates.length;
}

/** Returns the salary fields for a pair of bounds; a salary type is required to clear a bound. */
function salaryFields(min, max) {
  const known = [min != null, max != null].join();
  const type = { "true,true": "limited", "true,false": "no_max", "false,true": "no_min" }[known] || "unknown";
  return { salary_type: type, salary_min: min ?? null, salary_max: max ?? null };
}

/** Builds the update request for changed job fields, completing a salary change with the bound that did not change. */
function jobPayload(changes, record) {
  const { salary_min: min, salary_max: max, cv_version_id: _cvVersion, ...payload } = changes;
  if (min === undefined && max === undefined) return payload;
  return { ...payload, ...salaryFields(min === undefined ? record.salary_min : min, max === undefined ? record.salary_max : max) };
}

async function updateProcess(target, job, changes) {
  const payload = jobPayload(changes, job);
  if (Object.keys(payload).length) await target.updateJob(job.id, payload);
  if (changes.cv_version_id !== undefined) await target.setJobCvVersion(job.id, changes.cv_version_id);
}

/** Adds a process like the New process form, then sets whatever the new process does not already hold. */
async function createProcess(target, values) {
  const created = await target.createJob({ ...jobPayload(values, {}), company_name: values.company_name ?? "", create_default_stages: false });
  const pending = Object.entries(values).filter(([name, value]) => !sameValue(JOB_FIELDS.get(name), created, value));
  await updateProcess(target, created, Object.fromEntries(pending));
}

async function createQuestion(target, stageId, values) {
  const question = await target.createQuestion({ stage_id: stageId, question: values.question, answer_notes: values.answer_notes || "" });
  if (values.is_asked) await target.updateQuestion(question.id, { is_asked: true });
}

/**
 * Saves an import plan one record at a time. A failed record is reported and the rest still run, so
 * importing the same file again completes only what is left.
 *
 * @param {ImportPlan} plan - The plan from {@link buildImportPlan}.
 * @param {object} target - The API: updateJob, createJob, setJobCvVersion, updateQuestion, and createQuestion.
 * @param {(done: number, total: number) => void} [onProgress] - Called after each record.
 * @returns {Promise<{total: number, failures: {sheet: string, row: number, message: string}[]}>} What failed.
 */
export async function applyImportPlan(plan, target, onProgress = () => {}) {
  const steps = [
    ...plan.processUpdates.map((entry) => ({ sheet: "board", row: entry.row, run: () => updateProcess(target, entry.job, entry.changes) })),
    ...plan.processCreates.map((entry) => ({ sheet: "board", row: entry.row, run: () => createProcess(target, entry.values) })),
    ...plan.questionUpdates.map((entry) => ({ sheet: "questions", row: entry.row, run: () => target.updateQuestion(entry.entry.question.id, entry.changes) })),
    ...plan.questionCreates.map((entry) => ({ sheet: "questions", row: entry.row, run: () => createQuestion(target, entry.target.stage.id, entry.values) })),
  ];
  const failures = [];
  for (const [index, step] of steps.entries()) {
    try {
      await step.run();
    } catch (error) {
      failures.push({ sheet: step.sheet, row: step.row, message: error?.message || "" });
    }
    onProgress(index + 1, steps.length);
  }
  return { total: steps.length, failures };
}
