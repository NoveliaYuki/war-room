/**
 * Shows the saved sample records in the interface language.
 * Sample text is saved in English; a field is translated only while it still holds that seeded text,
 * so anything the visitor typed stays exactly as written.
 */
import { buildDemoJobs, buildHistoricalDemoJobs } from "./jobs.js";
import { textFor } from "./text.js";

const JOB_FIELDS = ["position_title", "keyword_note", "description", "company_overview", "interview_notes", "reasons_to_change", "experience_notes", "expected_salary", "recruiter_agency", "current_stage_title"];
const STAGE_FIELDS = ["custom_title", "description", "notes", "recruiter_agency"];
const QUESTION_FIELDS = ["question", "answer_notes"];
const INTERVIEWER_FIELDS = ["role", "notes"];
const PERIOD_NAMES = { "demo-period-current": "current", "demo-period-june-2021": "june2021" };

const seedCache = new Map();

const interviewerKey = (stageId, index) => `${stageId}#interviewer-${index}`;

/** Collects every seeded text field in one language, keyed by record ID and field name. */
function seedText(language) {
  if (seedCache.has(language)) return seedCache.get(language);
  const values = new Map();
  const remember = (key, record, fields) => fields.forEach((field) => {
    if (typeof record[field] === "string") values.set(`${key}|${field}`, record[field]);
  });
  for (const job of [...buildDemoJobs(language), ...buildHistoricalDemoJobs({ language })]) {
    remember(job.id, job, JOB_FIELDS);
    for (const stage of job.stages) {
      remember(stage.id, stage, STAGE_FIELDS);
      stage.interviewers.forEach((person, index) => remember(interviewerKey(stage.id, index), person, INTERVIEWER_FIELDS));
      stage.questions.forEach((question) => remember(question.id, question, QUESTION_FIELDS));
    }
  }
  seedCache.set(language, values);
  return values;
}

/** Replaces each field still holding its `from` seed text with the `to` seed text. */
function swapFields(record, key, fields, from, to) {
  if (!record) return record;
  for (const field of fields) {
    const id = `${key}|${field}`;
    if (Object.hasOwn(record, field) && to.has(id) && record[field] === from.get(id)) record[field] = to.get(id);
  }
  return record;
}

function swapStage(stage, from, to) {
  swapFields(stage, stage.id, STAGE_FIELDS, from, to);
  if (Array.isArray(stage.interviewers)) {
    stage.interviewers = stage.interviewers.map((person, index) => swapFields({ ...person }, interviewerKey(stage.id, index), INTERVIEWER_FIELDS, from, to));
  }
  if (Array.isArray(stage.questions)) stage.questions.forEach((question) => swapFields(question, question.id, QUESTION_FIELDS, from, to));
  return stage;
}

/** Returns the seed maps for showing English records in `language`, or null for English. */
function toLanguage(language) {
  return language && language !== "en" ? [seedText("en"), seedText(language)] : null;
}

/** Returns the seed maps for saving `language` text as English, or null for English. */
function toEnglish(language) {
  return language && language !== "en" ? [seedText(language), seedText("en")] : null;
}

/**
 * Translates a copy of a saved sample job, with its stages, interviewers, and questions.
 * @param {Record<string, any>} job - A copy the caller owns; it is changed in place.
 * @param {string} language - Interface language code.
 * @returns {Record<string, any>} The same job.
 */
export function localizeDemoJob(job, language) {
  const maps = toLanguage(language);
  if (!maps) return job;
  swapFields(job, job.id, JOB_FIELDS, ...maps);
  (job.stages || []).forEach((stage) => swapStage(stage, ...maps));
  return job;
}

/** Translates a copy of a saved sample stage, changing it in place. */
export function localizeDemoStage(stage, language) {
  const maps = toLanguage(language);
  return maps ? swapStage(stage, ...maps) : stage;
}

/** Translates a copy of a saved sample question, changing it in place. */
export function localizeDemoQuestion(question, language) {
  const maps = toLanguage(language);
  return maps ? swapFields(question, question.id, QUESTION_FIELDS, ...maps) : question;
}

/** Returns a job update whose untouched sample text is stored in English again. */
export function canonicalDemoJobPayload(id, payload, language) {
  const maps = toEnglish(language);
  return maps ? swapFields({ ...payload }, id, JOB_FIELDS, ...maps) : payload;
}

/** Returns a stage update whose untouched sample text is stored in English again. */
export function canonicalDemoStagePayload(id, payload, language) {
  const maps = toEnglish(language);
  if (!maps) return payload;
  const fields = swapStage({ ...payload, id }, ...maps);
  delete fields.id;
  return fields;
}

/** Returns a question update whose untouched sample text is stored in English again. */
export function canonicalDemoQuestionPayload(id, payload, language) {
  const maps = toEnglish(language);
  return maps ? swapFields({ ...payload }, id, QUESTION_FIELDS, ...maps) : payload;
}

/** Returns the name to show for a sample search period that still has its seeded name. */
export function localizeDemoPeriodName(period, language) {
  const key = PERIOD_NAMES[period.id];
  return key && period.name === textFor("en").periods[key] ? textFor(language).periods[key] : period.name;
}

/** Returns the name to save for a sample search period, keeping its seeded English name when unchanged. */
export function canonicalDemoPeriodName(id, name, language) {
  const key = PERIOD_NAMES[id];
  return key && name === textFor(language).periods[key] ? textFor("en").periods[key] : name;
}
