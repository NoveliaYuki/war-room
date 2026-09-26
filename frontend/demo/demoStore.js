import { initialJobs } from "./data/jobs.js";

const STORAGE_KEY = "war-room-demo-data-v13";
const clone = (value) => JSON.parse(JSON.stringify(value));

function loadJobs() {
  try {
    const stored = window.localStorage.getItem(STORAGE_KEY);
    return stored ? JSON.parse(stored) : clone(initialJobs);
  } catch {
    return clone(initialJobs);
  }
}

let jobs = loadJobs();

function save() {
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(jobs));
  } catch {
    // The demo stays usable for the current tab when storage is unavailable.
  }
}

function getJob(id) {
  const job = jobs.find((item) => item.id === String(id));
  if (!job) throw new Error("Selection process not found");
  return job;
}

function getStage(id) {
  for (const job of jobs) {
    const stage = job.stages.find((item) => item.id === String(id));
    if (stage) return { job, stage };
  }
  throw new Error("Interview stage not found");
}

function refreshDerived(job) {
  job.total_stages_count = job.stages.length;
  const index = job.stages.findIndex((stage) => stage.status === "current");
  job.current_stage_index = index >= 0 ? index + 1 : undefined;
  job.current_stage_title = index >= 0 ? job.stages[index].custom_title || job.stages[index].stage_type : undefined;
}

function makeStage(jobId, payload, orderIndex) {
  const id = `demo-stage-${crypto.randomUUID()}`;
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
  const id = `demo-job-${crypto.randomUUID()}`;
  const job = {
    id, company_name: payload.company_name || "Unknown", position_title: payload.position_title,
    status: "ongoing", salary_type: payload.salary_type || "unknown", salary_min: payload.salary_min ?? null,
    salary_max: payload.salary_max ?? null, salary_currency: payload.salary_currency || "EUR",
    recruiter_type: payload.recruiter_type || "none", recruiter_name: payload.recruiter_name || null,
    recruiter_agency: payload.recruiter_agency || null, recruiter_contact: null,
    job_post_url: payload.job_post_url || null, avatar_seed: payload.company_name || "Unknown",
    keyword_note: payload.keyword_note || "", description: payload.description || "",
    company_overview: payload.company_overview || "", company_domain: payload.company_domain || null,
    interview_notes: payload.interview_notes || "", reasons_to_change: payload.reasons_to_change || "",
    experience_notes: payload.experience_notes || "", expected_salary: payload.expected_salary || "",
    work_arrangement: payload.work_arrangement || "unknown", employment_type: payload.employment_type || "unknown",
    is_referral: Boolean(payload.is_referral), order_index: jobs.length, created_at: now, updated_at: now,
    interviewers: [], stages: [], attachments: [],
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
  async getJobs(status = "all", search = "") {
    return clone(jobs.filter((job) => (status === "all" || job.status === status) && meetsSearch(job, search)).sort((a, b) => a.order_index - b.order_index));
  },
  async getJobCounts() {
    return { all: jobs.length, ongoing: jobs.filter((job) => job.status === "ongoing").length, accepted: jobs.filter((job) => job.status === "accepted").length, rejected: jobs.filter((job) => job.status === "rejected").length };
  },
  async getJob(id) { return clone(getJob(id)); },
  async createJob(payload) { const job = makeJob(payload); jobs.push(job); save(); return clone(job); },
  async updateJob(id, payload) { const job = getJob(id); Object.assign(job, payload, { updated_at: Date.now() }); refreshDerived(job); save(); return clone(job); },
  async deleteJob(id) { jobs = jobs.filter((job) => job.id !== String(id)); save(); return { success: true }; },
  async reorderJobs(ids) { const order = new Map(ids.map((id, index) => [String(id), index])); jobs.forEach((job) => { if (order.has(job.id)) job.order_index = order.get(job.id); }); jobs.sort((a, b) => a.order_index - b.order_index); save(); return { success: true }; },
  async createStage(payload) { const job = getJob(payload.job_id); const stage = makeStage(job.id, payload, job.stages.length); job.stages.push(stage); refreshDerived(job); save(); return clone(stage); },
  async updateStage(id, payload) { const { job, stage } = getStage(id); Object.assign(stage, payload); refreshDerived(job); save(); return clone(stage); },
  async setCurrentStage(id) { const { job, stage } = getStage(id); job.stages.forEach((item) => { item.status = item.id === stage.id ? "current" : item.status === "current" ? "completed" : item.status; }); refreshDerived(job); save(); return clone(stage); },
  async getMeetings(date = "") { return jobs.flatMap((job) => job.stages.filter((stage) => stage.meeting_date && (!date || stage.meeting_date === date)).map((stage) => toMeeting(job, stage))); },
  async scheduleMeeting(id, payload) { const { stage } = getStage(id); Object.assign(stage, payload); save(); return clone(stage); },
  async deleteStage(id) { const { job, stage } = getStage(id); job.stages = job.stages.filter((item) => item.id !== stage.id); job.stages.forEach((item, index) => { item.order_index = index; }); refreshDerived(job); save(); return { success: true }; },
  async reorderStages(jobId, ids) { const job = getJob(jobId); const byId = new Map(job.stages.map((stage) => [stage.id, stage])); job.stages = ids.map((id) => byId.get(String(id))).filter(Boolean); job.stages.forEach((stage, index) => { stage.order_index = index; }); refreshDerived(job); save(); return { success: true }; },
  async createQuestion(payload) { const { stage } = getStage(payload.stage_id); const question = { id: `demo-question-${crypto.randomUUID()}`, stage_id: stage.id, order_index: stage.questions.length, question: payload.question, answer_notes: payload.answer_notes || "", is_asked: false, created_at: Date.now() }; stage.questions.push(question); save(); return clone(question); },
  async reorderQuestions(stageId, ids) { const { stage } = getStage(stageId); const byId = new Map(stage.questions.map((question) => [question.id, question])); stage.questions = ids.map((id) => byId.get(String(id))).filter(Boolean); stage.questions.forEach((question, index) => { question.order_index = index; }); save(); return { success: true }; },
  async updateQuestion(id, payload) { for (const job of jobs) for (const stage of job.stages) { const question = stage.questions.find((item) => item.id === String(id)); if (question) { Object.assign(question, payload); save(); return clone(question); } } throw new Error("Interview question not found"); },
  async deleteQuestion(id) { for (const job of jobs) for (const stage of job.stages) { const question = stage.questions.find((item) => item.id === String(id)); if (question) { stage.questions = stage.questions.filter((item) => item.id !== question.id); save(); return { success: true }; } } throw new Error("Interview question not found"); },
  async uploadAttachment() { throw new Error("File attachments are disabled in the browser demo."); },
  async deleteAttachment() { return { success: true }; },
};

export function resetDemoData() { jobs = clone(initialJobs); save(); }
