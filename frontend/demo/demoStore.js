import { buildHistoricalDemoJobs, initialJobs } from "./data/jobs.js";
import { cacheDemoLogos, DEMO_LOGOS_STORAGE_KEY, getDemoLogoUrl } from "./logoStore.js";
import { saveFile, readFile, deleteFile, replaceFiles } from "./fileStore.js";
import { formatLocalDate, getDemoMeetingDayOffset, setDemoMeetingDate } from "./data/meetingDates.js";
import { migrateLegacyDemoJob } from "../public/js/utils/demoMigration.js";
import { initialTechnologies } from "./data/technologies.js";
import { applyInactiveWaitingPolicy, touchDemoJob } from "./inactiveWaiting.js";
import { getLanguage, t } from "../public/js/i18n.js";
import { textFor } from "./data/text.js";
import {
  canonicalDemoJobPayload, canonicalDemoPeriodName, canonicalDemoQuestionPayload, canonicalDemoStagePayload,
  localizeDemoJob, localizeDemoPeriodName, localizeDemoQuestion, localizeDemoStage,
} from "./data/localize.js";

const STORAGE_KEY = "war-room-demo-data-v13";
const TECHNOLOGY_STORAGE_KEY = "war-room-demo-technologies-v1";
const SCHEDULE_DATE_KEY = "war-room-demo-schedule-date";
const DEMO_SEED_VERSION_KEY = "war-room-demo-seed-version";
const DEMO_SEED_VERSION = "8";
const NEW_DEMO_SEED_IDS = new Set(["demo-10", "demo-11", "demo-12"]);
const NEW_DEMO_TECHNOLOGY_IDS = new Set(["tech-aws", "tech-terraform"]);
const CV_VERSIONS_KEY = "war-room-demo-cv-versions-v1";
const NEXT_CV_VERSION_KEY = "war-room-demo-next-cv-version-v1";
const SEARCH_PERIODS_KEY = "war-room-demo-search-periods-v1";
const SEARCH_PERIOD_SEED_KEY = "war-room-demo-search-period-seed-v1";
const SEARCH_PERIOD_SEED_VERSION = "3";
const clone = (value) => JSON.parse(JSON.stringify(value));
const DEMO_HISTORY_ACTIONS = new Map([
  ["company_name", "changed company"], ["position_title", "changed role"], ["keyword_note", "changed role highlights"],
  ["description", "changed job description"], ["company_overview", "changed company and role overview"],
]);

/** Saves an empty company name as the "Unknown" placeholder, as the API does. */
function withDemoCompanyName(fields) {
  if (Object.hasOwn(fields, "company_name")) fields.company_name = String(fields.company_name ?? "").trim() || "Unknown";
  return fields;
}

function recordDemoHistory(job, action, timestamp = Math.floor(Date.now() / 1000)) {
  job.history ||= [];
  job.history.push({ occurred_at: timestamp, action });
}

function readDemoCvVersions() {
  try {
    const value = JSON.parse(window.localStorage.getItem(CV_VERSIONS_KEY) || "[]");
    return Array.isArray(value) ? value : [];
  } catch {
    return [];
  }
}

function saveDemoCvVersions(versions) {
  window.localStorage.setItem(CV_VERSIONS_KEY, JSON.stringify(versions));
}

function getDemoNextCvVersion(versions = readDemoCvVersions()) {
  const saved = Number(window.localStorage.getItem(NEXT_CV_VERSION_KEY));
  const minimum = Math.max(1, ...versions.map((version) => (Number(version.version_number) || 0) + 1));
  return Number.isInteger(saved) && saved >= minimum ? saved : minimum;
}

function saveDemoNextCvVersion(nextVersion) {
  window.localStorage.setItem(NEXT_CV_VERSION_KEY, String(nextVersion));
}

function refreshDemoMeetingDates(records, today) {
  if (!Array.isArray(records)) return;
  const sampleJobs = records.filter((job) => /^demo-\d+$/.test(job.id));
  const stages = sampleJobs.flatMap((job) => job.stages || []);
  const todayStage = sampleJobs.find((job) => job.id === "demo-1")?.stages?.find((stage) => stage.id === "demo-1-stage-2")
    || stages.find((stage) => stage.status === "current")
    || stages.find((stage) => stage.meeting_date)
    || stages[0];

  sampleJobs.forEach((job) => {
    (job.stages || []).forEach((stage) => {
      if (stage === todayStage) {
        setDemoMeetingDate(stage, today, 0);
        return;
      }
      if (!stage.meeting_date) return;
      const direction = job.status !== "ongoing" || stage.status === "completed" ? "past" : "future";
      setDemoMeetingDate(stage, today, getDemoMeetingDayOffset(stage.id, new Date(`${today}T12:00:00`), direction));
    });
  });
}

function loadJobs() {
  try {
    const stored = window.localStorage.getItem(STORAGE_KEY);
    const records = stored ? JSON.parse(stored) : clone(initialJobs);
    const today = formatLocalDate(new Date());
    const seedChanged = window.localStorage.getItem(DEMO_SEED_VERSION_KEY) !== DEMO_SEED_VERSION;
    const seededJobs = new Map(initialJobs.map((job) => [job.id, job]));
    if (seedChanged) {
      migrateLegacyDemoJob(records, seededJobs.get("demo-3"));
      const savedIDs = new Set(records.map((job) => job.id));
      initialJobs.filter((job) => NEW_DEMO_SEED_IDS.has(job.id) && !savedIDs.has(job.id)).forEach((job) => records.push(clone(job)));
      migrateDemoTimestamps(records, seededJobs);
      migrateDemoTechnologyCatalog();
    }
    records.forEach((job) => { if (!Array.isArray(job.technologies)) job.technologies = clone(seededJobs.get(job.id)?.technologies || []); });
    const inactivityChanged = applyInactiveWaitingPolicy(records);
    if (seedChanged || window.localStorage.getItem(SCHEDULE_DATE_KEY) !== today) {
      refreshDemoMeetingDates(records, today);
      window.localStorage.setItem(STORAGE_KEY, JSON.stringify(records));
      window.localStorage.setItem(SCHEDULE_DATE_KEY, today);
      window.localStorage.setItem(DEMO_SEED_VERSION_KEY, DEMO_SEED_VERSION);
    } else if (inactivityChanged) {
      window.localStorage.setItem(STORAGE_KEY, JSON.stringify(records));
    }
    return records;
  } catch {
    return clone(initialJobs);
  }
}

function migrateDemoTechnologyCatalog() {
  try {
    const stored = window.localStorage.getItem(TECHNOLOGY_STORAGE_KEY);
    if (stored === null) return;
    const catalog = JSON.parse(stored);
    if (!Array.isArray(catalog)) return;
    const savedIDs = new Set(catalog.map((item) => item?.id));
    initialTechnologies.filter((item) => NEW_DEMO_TECHNOLOGY_IDS.has(item.id) && !savedIDs.has(item.id))
      .forEach((item) => catalog.push(clone(item)));
    window.localStorage.setItem(TECHNOLOGY_STORAGE_KEY, JSON.stringify(catalog));
  } catch { /* The existing demo catalog remains usable if this migration cannot be saved. */ }
}

function migrateDemoTimestamps(records, seededJobs) {
  const now = Math.floor(Date.now() / 1000);
  records.forEach((job) => {
    const seed = seededJobs.get(job.id);
    for (const field of ["created_at", "updated_at", "status_changed_at", "last_activity_at"]) {
      if (Number(job[field]) > 1e12) job[field] = Math.floor(Number(job[field]) / 1000);
    }
    if (!Number(job.last_activity_at)) {
      job.last_activity_at = seed?.automatic_rejection_reason
        ? seed.last_activity_at
        : now;
    }
    if (seed?.automatic_rejection_reason) job.automatic_rejection_reason = seed.automatic_rejection_reason;
    if (!Number(job.status_changed_at)) {
      job.status_changed_at = seed?.status_changed_at || Number(job.created_at) || now;
    }
    if (job.status === "waiting" && !job.is_referral && !job.application_sent_date && seed?.application_sent_date) {
      job.application_sent_date = seed.application_sent_date;
    }
  });
}

let jobs = loadJobs();
const inactivityTimer = setInterval(() => {
  if (applyInactiveWaitingPolicy(jobs)) save();
}, 24 * 60 * 60 * 1000);
inactivityTimer?.unref?.();
let searchPeriods = loadSearchPeriods();
seedDemoSearchPeriods();
refreshDemoSearchPeriodDates();

function loadSearchPeriods() {
  try {
    const value = JSON.parse(window.localStorage.getItem(SEARCH_PERIODS_KEY) || "[]");
    return Array.isArray(value) ? value : [];
  } catch { return []; }
}

function saveSearchPeriods() {
  try { window.localStorage.setItem(SEARCH_PERIODS_KEY, JSON.stringify(searchPeriods)); } catch { /* Keep periods available for this tab. */ }
}

function seedDemoSearchPeriods() {
  try {
    if (window.localStorage.getItem(SEARCH_PERIOD_SEED_KEY) === SEARCH_PERIOD_SEED_VERSION) return;
    const savedDemoSeed = new Set(jobs.map((job) => job.id));
    if (!initialJobs.every((job) => savedDemoSeed.has(job.id))) {
      window.localStorage.setItem(SEARCH_PERIOD_SEED_KEY, SEARCH_PERIOD_SEED_VERSION);
      return;
    }
    if (searchPeriods.length === 0) {
      const createdAt = Math.floor(Date.now() / 1000);
      const currentPeriod = {
        id: "demo-period-current",
        name: "Current",
        start_date: getCurrentSearchPeriodStart(),
        end_date: "",
        created_at: createdAt,
        updated_at: createdAt,
      };
      const previousPeriod = {
        id: "demo-period-june-2021",
        name: "June 2021",
        start_date: "2021-06-01",
        end_date: "2021-06-30",
        created_at: createdAt,
        updated_at: createdAt,
      };
      searchPeriods = [currentPeriod, previousPeriod];
    }
    const currentPeriod = searchPeriods.find((period) => period.id === "demo-period-current");
    const previousPeriod = searchPeriods.find((period) => period.id === "demo-period-june-2021");
    if (currentPeriod && previousPeriod) {
      const currentMonthStart = new Date();
      currentMonthStart.setDate(1);
      Object.assign(currentPeriod, { name: "Current", start_date: formatLocalDate(currentMonthStart), end_date: "" });
      Object.assign(previousPeriod, { name: "June 2021", start_date: "2021-06-01", end_date: "2021-06-30" });
      jobs = jobs.filter((job) => !job.id.startsWith("demo-period-june-2021-"));
      jobs.push(...createHistoricalDemoJobs(previousPeriod));
      jobs.forEach((job) => {
        job.search_period_id = job.id.startsWith("demo-period-june-2021-") ? previousPeriod.id : currentPeriod.id;
      });
      saveSearchPeriods();
      save();
    }
    window.localStorage.setItem(SEARCH_PERIOD_SEED_KEY, SEARCH_PERIOD_SEED_VERSION);
  } catch { /* The demo remains usable if browser storage is unavailable. */ }
}

function getCurrentSearchPeriodStart(today = new Date()) {
  const previousMonth = new Date(today.getFullYear(), today.getMonth() - 1, 1);
  const lastDayOfPreviousMonth = new Date(previousMonth.getFullYear(), previousMonth.getMonth() + 1, 0).getDate();
  previousMonth.setDate(Math.min(today.getDate(), lastDayOfPreviousMonth));
  return formatLocalDate(previousMonth);
}

function refreshDemoSearchPeriodDates() {
  let jobsChanged = false;
  const currentPeriod = searchPeriods.find((period) => period.id === "demo-period-current");
  if (currentPeriod) {
    currentPeriod.start_date = getCurrentSearchPeriodStart();
    currentPeriod.end_date = "";
  }
  const periodsByID = new Map(searchPeriods.map((period) => [period.id, period]));
  jobs.forEach((job) => {
    const period = periodsByID.get(job.search_period_id);
    if (!period) return;
    if (job.application_sent_date) {
      const validCVDate = clampDateToSearchPeriod(job.application_sent_date, period);
      if (validCVDate !== job.application_sent_date) {
        job.application_sent_date = validCVDate;
        jobsChanged = true;
      }
    }
    const changedDate = Number(job.status_changed_at) > 0
      ? formatLocalDate(new Date(Number(job.status_changed_at) * 1000))
      : period.start_date;
    const validStatusDate = clampDateToSearchPeriod(changedDate, period);
    if (validStatusDate !== changedDate) {
      job.status_changed_at = Math.floor(new Date(`${validStatusDate}T12:00:00`).getTime() / 1000);
      jobsChanged = true;
    }
  });
  saveSearchPeriods();
  if (jobsChanged) save();
}

function clampDateToSearchPeriod(date, period) {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(date) || date < period.start_date) return period.start_date;
  if (period.end_date && date > period.end_date) return period.end_date;
  return date;
}

function createDemoID() {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

function assignUnassignedJobsToPeriod(period) {
  const start = new Date(`${period.start_date}T00:00:00`);
  const end = period.end_date ? new Date(`${period.end_date}T23:59:59.999`) : null;
  jobs.forEach((job) => {
    if (job.search_period_id || !job.created_at) return;
    const created = new Date(job.created_at * 1000);
    if (created >= start && (!end || created <= end)) job.search_period_id = period.id;
  });
  save();
}

function validateSearchPeriod(payload, excludeID = "") {
  const name = String(payload.name || "").trim();
  const startDate = String(payload.start_date || "");
  const endDate = String(payload.end_date || "");
  const datePattern = /^\d{4}-\d{2}-\d{2}$/;
  const isCalendarDate = (value) => datePattern.test(value)
    && Number.isFinite(Date.parse(`${value}T00:00:00Z`))
    && new Date(`${value}T00:00:00Z`).toISOString().slice(0, 10) === value;
  if (!name || name.length > 100 || !isCalendarDate(startDate) || (endDate && !isCalendarDate(endDate))
    || (endDate && startDate > endDate)) throw new Error(t("errors.searchPeriodInvalid"));
  if (searchPeriods.some((period) => period.id !== excludeID
    && period.start_date <= (endDate || "9999-12-31")
    && (period.end_date || "9999-12-31") >= startDate)) {
    throw new Error(t("errors.searchPeriodOverlap"));
  }
  return { name, start_date: startDate, end_date: endDate };
}

function loadTechnologies() {
  try {
    const stored = window.localStorage.getItem(TECHNOLOGY_STORAGE_KEY);
    if (stored !== null) {
      const parsed = JSON.parse(stored);
      return Array.isArray(parsed) ? parsed : clone(initialTechnologies);
    }
  } catch { /* Use the built-in demo catalog when browser storage is unavailable. */ }
  const seeded = clone(initialTechnologies);
  try { window.localStorage.setItem(TECHNOLOGY_STORAGE_KEY, JSON.stringify(seeded)); } catch { /* The catalog remains available for this tab. */ }
  return seeded;
}

let technologies = loadTechnologies();

function saveTechnologies() {
  try { window.localStorage.setItem(TECHNOLOGY_STORAGE_KEY, JSON.stringify(technologies)); } catch { /* The demo remains usable in this tab. */ }
}

function save() {
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(jobs));
  } catch {
    // The demo stays usable for the current tab when storage is unavailable.
  }
}

function createHistoricalDemoJobs(period) {
  const firstOrderIndex = Math.max(-1, ...jobs.filter((job) => !job.id.startsWith("demo-period-june-2021-"))
    .map((job) => Number(job.order_index) || 0)) + 1;
  return buildHistoricalDemoJobs({ periodId: period.id, firstOrderIndex });
}

function getJob(id) {
  const job = jobs.find((item) => item.id === String(id));
  if (!job) throw new Error(t("errors.processNotFound"));
  return job;
}

function getStage(id) {
  for (const job of jobs) {
    const stage = job.stages.find((item) => item.id === String(id));
    if (stage) return { job, stage };
  }
  throw new Error(t("errors.stageNotFound"));
}

function refreshDerived(job) {
  job.total_stages_count = job.stages.length;
  const index = job.stages.findIndex((stage) => stage.status === "current");
  job.current_stage_index = index >= 0 ? index + 1 : undefined;
  job.current_stage_title = index >= 0 ? job.stages[index].custom_title || job.stages[index].stage_type : undefined;
  job.current_stage_title_is_type = index >= 0 && !job.stages[index].custom_title;
}

function technologyKey(value) { return String(value || "").trim().toLowerCase().replace(/\s+/g, " "); }

function validateDemoTechnology(name, aliases, excludeID = "") {
  const cleanName = String(name || "").trim();
  const cleanAliases = [...new Set((aliases || []).map((alias) => String(alias || "").trim()).filter(Boolean))];
  if (!cleanName || cleanName.length > 80 || cleanAliases.length > 20 || cleanAliases.some((alias) => alias.length > 80)) throw new Error(t("errors.technologyInvalid"));
  const terms = [cleanName, ...cleanAliases].map(technologyKey);
  if (new Set(terms).size !== terms.length) throw new Error(t("errors.technologyTermsDuplicate"));
  for (const item of technologies) {
    if (item.id === excludeID) continue;
    const existing = [item.name, ...(item.aliases || [])].map(technologyKey);
    if (terms.some((term) => existing.includes(term))) throw new Error(t("errors.technologyExists"));
  }
  return { name: cleanName, aliases: cleanAliases };
}

function jobWithCurrentTechnologies(job) {
  return {
    ...job,
    technologies: (job.technologies || []).map((item) => {
      const technology = technologies.find((entry) => entry.id === item.id) || item;
      return { id: technology.id, name: technology.name, aliases: technology.aliases || [] };
    }),
  };
}

/** Finds catalog technologies named in demo posting text. */
function suggestDemoTechnologies(text) {
  const haystack = ` ${String(text).toLowerCase()} `;
  return listDemoTechnologies()
    .filter((item) => haystack.includes(` ${item.name.toLowerCase()} `) || haystack.includes(` ${item.name.toLowerCase()},`))
    .map((item) => ({ id: item.id, name: item.name, aliases: item.aliases || [], recommended: true }));
}

/** Imitates link import without a backend, using a synthetic posting. */
function previewDemoImport({ url, description }) {
  if (!url) return { technologies: suggestDemoTechnologies(description) };
  if (/linkedin\.com/i.test(url)) throw Object.assign(new Error(t("errors.importNotAllowed")), { code: "paste_required" });
  const sample = textFor(getLanguage()).importDraft;
  const overview = sample.overview;
  return {
    draft: {
      position_title: sample.title, company_name: sample.company, company_overview: overview,
      company_domain: "", job_post_url: url, application_url: "", keyword_note: sample.keywords,
      work_arrangement: "remote", employment_type: "permanent", salary_min: 70000, salary_max: 90000,
      salary_currency: "EUR", source: "demo", warnings: [],
    },
    technologies: suggestDemoTechnologies(overview),
  };
}

function listDemoTechnologies() {
  const shown = jobs.map(presentJob);
  return clone(technologies.map((item) => ({ ...item, jobs: shown.filter((job) => (job.technologies || []).some((entry) => entry.id === item.id)).map((job) => ({ id: job.id, company_name: job.company_name, position_title: job.position_title })) })));
}

/** Returns a copy of a saved job, with current catalog names and sample text in the interface language. */
function presentJob(job) {
  return localizeDemoJob(clone(jobWithCurrentTechnologies(job)), getLanguage());
}

function presentStage(stage) {
  return localizeDemoStage(clone(stage), getLanguage());
}

function presentPeriod(period) {
  return { ...clone(period), name: localizeDemoPeriodName(period, getLanguage()) };
}

function makeStage(jobId, payload, orderIndex) {
  const id = `demo-stage-${createDemoID()}`;
  return {
    id, job_id: jobId, order_index: orderIndex, stage_type: payload.stage_type || "HR",
    custom_title: payload.custom_title || null, description: payload.description || "",
    status: payload.status || "pending", scheduled_at: null, meeting_date: null, meeting_time: null,
    meeting_url: null, meeting_type: "video", notes: "", recruiter_name: null,
    recruiter_type: "none", recruiter_agency: null, recruiter_contact: null,
    interviewers: [], questions: [],
  };
}

function makeJob(payload) {
  const now = Date.now();
  const nowSeconds = Math.floor(now / 1000);
  const id = `demo-job-${createDemoID()}`;
  const localToday = new Date();
  const today = [localToday.getFullYear(), String(localToday.getMonth() + 1).padStart(2, "0"), String(localToday.getDate()).padStart(2, "0")].join("-");
  const currentPeriod = searchPeriods.find((period) => period.start_date <= today && (!period.end_date || period.end_date >= today));
  const suppliedPeriod = String(payload.search_period_id || "");
  const requestedPeriod = suppliedPeriod || currentPeriod?.id || "";
  if (requestedPeriod && !searchPeriods.some((period) => period.id === requestedPeriod)) throw new Error(t("errors.searchPeriodNotFound"));
  const job = {
    id, company_name: payload.company_name || "Unknown", position_title: payload.position_title,
    status: "waiting", salary_type: payload.salary_type || "unknown", salary_min: payload.salary_min ?? null,
    salary_max: payload.salary_max ?? null, salary_currency: payload.salary_currency || "EUR",
    recruiter_type: payload.recruiter_type || "none", recruiter_name: payload.recruiter_name || null,
    recruiter_agency: payload.recruiter_agency || null, recruiter_contact: null,
    job_post_url: payload.job_post_url || null, application_url: payload.application_url || null, avatar_seed: payload.company_name || "Unknown",
    keyword_note: payload.keyword_note || "", description: payload.description || "",
    company_overview: payload.company_overview || "", company_domain: payload.company_domain || null,
    interview_notes: payload.interview_notes || "", reasons_to_change: payload.reasons_to_change || "",
    experience_notes: payload.experience_notes || "", expected_salary: payload.expected_salary || "",
    work_arrangement: payload.work_arrangement || "unknown", employment_type: payload.employment_type || "unknown",
    search_period_id: requestedPeriod || null,
    application_sent_date: payload.is_referral ? null : today,
    is_referral: Boolean(payload.is_referral), order_index: jobs.length, created_at: nowSeconds, status_changed_at: nowSeconds, updated_at: nowSeconds, last_activity_at: nowSeconds,
    history: [{ occurred_at: nowSeconds, action: "created" }],
    interviewers: [], stages: [], attachments: [],
    technologies: [],
  };
  if (payload.create_default_stages) {
    ["HR", "Technical", "Cultural", "Offer & Decision"].forEach((type, index) => job.stages.push(makeStage(id, { stage_type: type }, index)));
  }
  refreshDerived(job);
  return job;
}

function meetsSearch(job, term) {
  const value = term.trim().toLowerCase();
  return !value || [job.company_name, job.position_title, job.keyword_note, job.description].some((field) => String(field || "").toLowerCase().includes(value));
}

function toMeeting(job, stage) {
  return {
    stage_id: stage.id, job_id: job.id, order_index: stage.order_index, stage_type: stage.stage_type,
    custom_title: stage.custom_title, stage_description: stage.description, stage_status: stage.status,
    meeting_date: stage.meeting_date, meeting_time: stage.meeting_time, meeting_url: stage.meeting_url,
    meeting_type: stage.meeting_type, stage_notes: stage.notes, stage_interviewers: stage.interviewers,
    company_name: job.company_name,
    position_title: job.position_title, job_status: job.status, recruiter_name: stage.recruiter_name || job.recruiter_name,
    recruiter_contact: stage.recruiter_contact || job.recruiter_contact, recruiter_agency: stage.recruiter_agency || job.recruiter_agency,
    job_post_url: job.job_post_url, avatar_seed: job.avatar_seed, company_domain: job.company_domain,
  };
}

export const demoApi = {
  getCompanyLogoUrl(companyName, _companyDomain, jobId) { return getDemoLogoUrl(companyName, jobId); },
  async exportBackup() { throw new Error(t("errors.demoBackupUnavailable")); },
  async importBackup() { throw new Error(t("errors.demoBackupUnavailable")); },
  async getJobs(status = "all", search = "") {
    return jobs.filter((job) => status === "all" || job.status === status).map(presentJob).filter((job) => meetsSearch(job, search)).sort((a, b) => a.order_index - b.order_index);
  },
  async getSearchPeriods() {
    return searchPeriods.map((period) => ({ ...presentPeriod(period), job_count: jobs.filter((job) => job.search_period_id === period.id).length }))
      .sort((left, right) => right.start_date.localeCompare(left.start_date));
  },
  async createSearchPeriod(payload) {
    const valid = validateSearchPeriod(payload);
    const now = Math.floor(Date.now() / 1000);
    const period = { id: `demo-period-${createDemoID()}`, ...valid, created_at: now, updated_at: now };
    searchPeriods.push(period);
    assignUnassignedJobsToPeriod(period);
    saveSearchPeriods();
    return clone(period);
  },
  async updateSearchPeriod(id, payload) {
    const period = searchPeriods.find((item) => item.id === String(id));
    if (!period) throw new Error(t("errors.searchPeriodNotFound"));
    const name = canonicalDemoPeriodName(period.id, String(payload.name || "").trim(), getLanguage());
    Object.assign(period, validateSearchPeriod({ ...payload, name }, period.id), { updated_at: Math.floor(Date.now() / 1000) });
    assignUnassignedJobsToPeriod(period);
    saveSearchPeriods(); return presentPeriod(period);
  },
  async deleteSearchPeriod(id) {
    const period = searchPeriods.find((item) => item.id === String(id));
    if (!period) throw new Error(t("errors.searchPeriodNotFound"));
    jobs.forEach((job) => { if (job.search_period_id === period.id) job.search_period_id = null; });
    searchPeriods = searchPeriods.filter((item) => item.id !== period.id);
    saveSearchPeriods(); save(); return { success: true };
  },
  async getJobCounts() {
    return { all: jobs.length, waiting: jobs.filter((job) => job.status === "waiting").length, ongoing: jobs.filter((job) => job.status === "ongoing").length, accepted: jobs.filter((job) => job.status === "accepted").length, rejected: jobs.filter((job) => job.status === "rejected").length };
  },
  async getJob(id) {
    const job = presentJob(getJob(id));
    for (const attachment of job.attachments || []) {
      const blob = await readFile(attachment.id);
      if (blob) attachment.download_url = URL.createObjectURL(blob);
    }
    return job;
  },
  async getCvVersions() { return clone(readDemoCvVersions()); },
  async uploadCvVersion(file) {
    if (!(file instanceof Blob) || !file.size) throw new Error(t("errors.cvFileEmpty"));
    const versions = readDemoCvVersions();
    const digest = await crypto.subtle.digest("SHA-256", await file.arrayBuffer());
    const sha256 = [...new Uint8Array(digest)].map((byte) => byte.toString(16).padStart(2, "0")).join("");
    const existing = versions.find((version) => version.sha256 === sha256);
    const nextVersion = getDemoNextCvVersion(versions);
    const record = { id: createDemoID(), version_number: nextVersion, original_name: file.name || "CV.pdf", file_size: file.size, mime_type: file.type || "application/pdf", sha256, uploaded_at: Math.floor(Date.now() / 1000), stored_file_id: existing?.stored_file_id || existing?.id || "" };
    if (!record.stored_file_id) {
      record.stored_file_id = record.id;
      await saveFile(record.stored_file_id, file);
    }
    versions.unshift(record);
    saveDemoCvVersions(versions);
    saveDemoNextCvVersion(nextVersion + 1);
    return clone(record);
  },
  async downloadCvVersion(id) {
    const version = readDemoCvVersions().find((item) => item.id === String(id));
    if (!version) throw new Error(t("errors.cvNotFound"));
    const blob = await readFile(version.stored_file_id || version.id);
    if (!blob) throw new Error(t("errors.cvNotFound"));
    return blob;
  },
  async deleteCvVersion(id) {
    if (jobs.some((job) => job.selected_cv_version?.id === String(id))) throw new Error(t("errors.cvAssigned"));
    const versions = readDemoCvVersions();
    if (!versions.some((version) => version.id === String(id))) throw new Error(t("errors.cvNotFound"));
    const remaining = versions.filter((version) => version.id !== String(id));
    const storageId = versions.find((version) => version.id === String(id))?.stored_file_id || String(id);
    if (!remaining.some((version) => (version.stored_file_id || version.id) === storageId)) await deleteFile(storageId);
    saveDemoCvVersions(remaining);
    return { success: true };
  },
  async setJobCvVersion(jobId, versionId) {
    const job = getJob(jobId);
    const version = versionId === null ? null : readDemoCvVersions().find((item) => item.id === String(versionId));
    if (versionId !== null && !version) throw new Error(t("errors.cvNotFound"));
    job.selected_cv_version = version ? clone(version) : null;
    save();
    return { success: true, selected_cv_version: clone(job.selected_cv_version) };
  },
  async getTechnologies() { return listDemoTechnologies(); },
  async createTechnology(payload) {
    const normalized = validateDemoTechnology(payload.name, payload.aliases || []);
    const item = { id: `tech-${createDemoID()}`, ...normalized };
    technologies.push(item); saveTechnologies(); return clone(item);
  },
  async updateTechnology(id, payload) {
    const item = technologies.find((technology) => technology.id === String(id));
    if (!item) throw new Error(t("errors.technologyNotFound"));
    Object.assign(item, validateDemoTechnology(payload.name, payload.aliases || [], item.id));
    jobs.forEach((job) => (job.technologies || []).forEach((entry) => { if (entry.id === item.id) entry.name = item.name; }));
    saveTechnologies(); save(); return clone(item);
  },
  async removeTechnologyAssignments(id) {
    const item = technologies.find((technology) => technology.id === String(id));
    if (!item) throw new Error(t("errors.technologyNotFound"));
    let removed = 0;
    jobs.forEach((job) => { const before = (job.technologies || []).length; job.technologies = (job.technologies || []).filter((entry) => entry.id !== item.id); removed += before - job.technologies.length; });
    save(); return { removed };
  },
  async deleteTechnology(id) {
    const item = technologies.find((technology) => technology.id === String(id));
    if (!item) throw new Error(t("errors.technologyNotFound"));
    if (jobs.some((job) => (job.technologies || []).some((entry) => entry.id === item.id))) throw new Error(t("errors.technologyAssigned"));
    technologies = technologies.filter((technology) => technology.id !== item.id); saveTechnologies(); return { success: true };
  },
  async previewJobImport(payload) { return previewDemoImport(payload); },
  async createJob(payload) { const job = makeJob(payload); touchDemoJob(job); jobs.push(job); save(); return clone(job); },
  async updateJob(id, payload) {
    const job = getJob(id);
    const statusChanged = Object.hasOwn(payload, "status") && payload.status !== job.status;
    const nowSeconds = Math.floor(Date.now() / 1000);
    const fields = withDemoCompanyName(canonicalDemoJobPayload(job.id, payload, getLanguage()));
    if (Object.hasOwn(fields, "search_period_id")) {
      const periodID = String(fields.search_period_id || "");
      if (periodID && !searchPeriods.some((period) => period.id === periodID)) throw new Error(t("errors.searchPeriodNotFound"));
      job.search_period_id = periodID || null;
      delete fields.search_period_id;
    }
    if (Object.hasOwn(fields, "technology_ids")) {
      const ids = fields.technology_ids;
      if (!Array.isArray(ids) || new Set(ids).size !== ids.length || ids.some((technologyID) => !technologies.some((item) => item.id === technologyID))) throw new Error(t("errors.technologyNotInCatalog"));
      job.technologies = ids.map((technologyID) => { const item = technologies.find((technology) => technology.id === technologyID); return { id: item.id, name: item.name }; });
      recordDemoHistory(job, "changed tech stack", nowSeconds);
      delete fields.technology_ids;
    }
    DEMO_HISTORY_ACTIONS.forEach((action, field) => {
      if (Object.hasOwn(fields, field) && fields[field] !== job[field]) recordDemoHistory(job, action, nowSeconds);
    });
    if (statusChanged) recordDemoHistory(job, `moved to ${payload.status}`, nowSeconds);
    Object.assign(job, fields, { updated_at: nowSeconds });
    touchDemoJob(job, nowSeconds * 1000);
    if (statusChanged) job.status_changed_at = nowSeconds;
    refreshDerived(job); save(); return presentJob(job);
  },
  async deleteJob(id) {
    const job = getJob(id);
    for (const attachment of job.attachments || []) await deleteFile(attachment.id);
    jobs = jobs.filter((item) => item.id !== String(id)); save(); return { success: true };
  },
  async reorderJobs(ids) { const order = new Map(ids.map((id, index) => [String(id), index])); jobs.forEach((job) => { if (order.has(job.id) && job.order_index !== order.get(job.id)) { job.order_index = order.get(job.id); touchDemoJob(job); } }); jobs.sort((a, b) => a.order_index - b.order_index); save(); return { success: true }; },
  async createStage(payload) { const job = getJob(payload.job_id); const firstStage = job.stages.length === 0; const stage = makeStage(job.id, payload, job.stages.length); job.stages.push(stage); touchDemoJob(job); if (firstStage) { job.status = "ongoing"; job.status_changed_at = Math.floor(Date.now() / 1000); recordDemoHistory(job, "moved to ongoing", job.status_changed_at); } refreshDerived(job); save(); return clone(stage); },
  async updateStage(id, payload) { const { job, stage } = getStage(id); Object.assign(stage, canonicalDemoStagePayload(stage.id, payload, getLanguage())); touchDemoJob(job); refreshDerived(job); save(); return presentStage(stage); },
  async setCurrentStage(id) { const { job, stage } = getStage(id); job.stages.forEach((item) => { item.status = item.id === stage.id ? "current" : item.status === "current" ? "completed" : item.status; }); touchDemoJob(job); refreshDerived(job); save(); return presentStage(stage); },
  async getMeetings(date = "") { return jobs.map(presentJob).flatMap((job) => job.stages.filter((stage) => stage.meeting_date && (!date || stage.meeting_date === date)).map((stage) => toMeeting(job, stage))); },
  async scheduleMeeting(id, payload) { const { job, stage } = getStage(id); Object.assign(stage, canonicalDemoStagePayload(stage.id, payload, getLanguage())); touchDemoJob(job); save(); return presentStage(stage); },
  async deleteStage(id) { const { job, stage } = getStage(id); job.stages = job.stages.filter((item) => item.id !== stage.id); job.attachments.forEach((item) => { if (item.stage_id === stage.id) item.stage_id = null; }); job.stages.forEach((item, index) => { item.order_index = index; }); touchDemoJob(job); refreshDerived(job); save(); return { success: true }; },
  async reorderStages(jobId, ids) { const job = getJob(jobId); const byId = new Map(job.stages.map((stage) => [stage.id, stage])); job.stages = ids.map((id) => byId.get(String(id))).filter(Boolean); job.stages.forEach((stage, index) => { stage.order_index = index; }); touchDemoJob(job); refreshDerived(job); save(); return { success: true }; },
  async createQuestion(payload) { const { job, stage } = getStage(payload.stage_id); const question = { id: `demo-question-${createDemoID()}`, stage_id: stage.id, order_index: stage.questions.length, question: payload.question, answer_notes: payload.answer_notes || "", is_asked: false, created_at: Date.now() }; stage.questions.push(question); touchDemoJob(job); save(); return clone(question); },
  async reorderQuestions(stageId, ids) { const { job, stage } = getStage(stageId); const byId = new Map(stage.questions.map((question) => [question.id, question])); stage.questions = ids.map((id) => byId.get(String(id))).filter(Boolean); stage.questions.forEach((question, index) => { question.order_index = index; }); touchDemoJob(job); save(); return { success: true }; },
  async updateQuestion(id, payload) { for (const job of jobs) for (const stage of job.stages) { const question = stage.questions.find((item) => item.id === String(id)); if (question) { Object.assign(question, canonicalDemoQuestionPayload(question.id, payload, getLanguage())); touchDemoJob(job); save(); return localizeDemoQuestion(clone(question), getLanguage()); } } throw new Error(t("errors.questionNotFound")); },
  async deleteQuestion(id) { for (const job of jobs) for (const stage of job.stages) { const question = stage.questions.find((item) => item.id === String(id)); if (question) { stage.questions = stage.questions.filter((item) => item.id !== question.id); touchDemoJob(job); save(); return { success: true }; } } throw new Error(t("errors.questionNotFound")); },
  async uploadAttachment(formData) {
    const job = getJob(formData.get("job_id"));
    const file = formData.get("file");
    const stageId = formData.get("stage_id") || null;
    if (!(file instanceof Blob) || file.size > 50 * 1024 * 1024 || !file.size) throw new Error(t("errors.fileTooLarge"));
    if (stageId && !job.stages.some((stage) => stage.id === stageId)) throw new Error(t("errors.stageNotFound"));
    const attachment = { id: createDemoID(), job_id: job.id, stage_id: stageId, original_name: file.name || "file", stored_filename: "", file_size: file.size, mime_type: file.type || "application/octet-stream", created_at: Date.now() };
    await saveFile(attachment.id, file);
    job.attachments.push(attachment);
    touchDemoJob(job);
    save();
    return clone(attachment);
  },
  async deleteAttachment(id) {
    for (const job of jobs) {
      if (!job.attachments.some((item) => item.id === String(id))) continue;
      await deleteFile(String(id));
      job.attachments = job.attachments.filter((item) => item.id !== String(id));
      touchDemoJob(job);
      save();
      return { success: true };
    }
    throw new Error(t("errors.attachmentNotFound"));
  },
};

export async function resetDemoData() {
  await replaceFiles(new Map());
  jobs = clone(initialJobs);
  window.localStorage.removeItem(CV_VERSIONS_KEY);
  window.localStorage.removeItem(NEXT_CV_VERSION_KEY);
  window.localStorage.removeItem(DEMO_LOGOS_STORAGE_KEY);
  cacheDemoLogos({});
  save();
}
