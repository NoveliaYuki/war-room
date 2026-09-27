import { initialJobs } from "./data/jobs.js";
import { cacheDemoLogos, DEMO_LOGOS_STORAGE_KEY, getDemoLogoAsset, getDemoLogoUrl } from "./logoStore.js";
import { saveFile, readFile, deleteFile, replaceFiles } from "./fileStore.js";
import { createZip } from "./zipWriter.js";

const STORAGE_KEY = "war-room-demo-data-v13";
const BACKUP_FORMAT = "war-room-demo-backup";
const BACKUP_VERSION = 1;
const MAX_BACKUP_BYTES = 4 * 1024 * 1024;
const MAX_ARCHIVE_BYTES = 500 * 1024 * 1024;
const MAX_ARCHIVE_MANIFEST_BYTES = 20 * 1024 * 1024;
const MAX_ARCHIVE_LOGO_BYTES = 1024 * 1024;
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

function validateBackup(value) {
  if (value?.format !== BACKUP_FORMAT || value.version !== BACKUP_VERSION || !Array.isArray(value.jobs)) {
    throw new Error("This is not a supported War Room demo JSON backup.");
  }
  if (value.jobs.length > 10000) throw new Error("The backup contains too many processes.");
  const importedJobs = clone(value.jobs);
  const ids = new Set();
  const addId = (record) => {
    if (!record || typeof record !== "object" || typeof record.id !== "string" || !record.id.trim() || ids.has(record.id)) {
      throw new Error("The backup contains a missing or duplicate record ID.");
    }
    ids.add(record.id);
  };

  for (const job of importedJobs) {
    if (!job || typeof job !== "object" || typeof job.company_name !== "string" || typeof job.position_title !== "string" || !Array.isArray(job.stages)) {
      throw new Error("The backup contains an invalid process record.");
    }
    addId(job);
    job.attachments = Array.isArray(job.attachments) ? job.attachments : [];
    job.interviewers = Array.isArray(job.interviewers) ? job.interviewers : [];
    for (const stage of job.stages) {
      if (!stage || typeof stage !== "object" || !Array.isArray(stage.questions)) {
        throw new Error("The backup contains an invalid interview stage.");
      }
      addId(stage);
      stage.interviewers = Array.isArray(stage.interviewers) ? stage.interviewers : [];
      for (const question of stage.questions) addId(question);
    }
    const stageIds = new Set(job.stages.map((stage) => stage.id));
    for (const attachment of job.attachments) {
      addId(attachment);
      if (attachment.job_id !== job.id || (attachment.stage_id && !stageIds.has(attachment.stage_id)) || typeof attachment.original_name !== "string") {
        throw new Error("The backup contains invalid uploaded file metadata.");
      }
    }
  }
  const validJobIds = new Set(importedJobs.map((job) => job.id));
  const importedLogos = value.company_logos ?? {};
  if (!importedLogos || typeof importedLogos !== "object" || Array.isArray(importedLogos)) {
    throw new Error("The backup contains invalid company icons.");
  }
  let logoBytes = 0;
  for (const [jobId, dataUrl] of Object.entries(importedLogos)) {
    if (!validJobIds.has(jobId) || typeof dataUrl !== "string" || !/^data:image\/(?:png|jpeg|webp|svg\+xml);base64,[A-Za-z0-9+/]+={0,2}$/.test(dataUrl)) {
      throw new Error("The backup contains an invalid company icon.");
    }
    logoBytes += dataUrl.length;
    if (logoBytes > MAX_BACKUP_BYTES) throw new Error("Company icons exceed the demo backup size limit.");
  }
  return { jobs: importedJobs, logos: importedLogos };
}

function findZipEntry(view, bytes, entry) {
  const start = entry.localHeaderOffset;
  if (start + 30 > bytes.length || view.getUint32(start, true) !== 0x04034b50) {
    throw new Error("The ZIP backup contains an invalid file entry.");
  }
  const nameLength = view.getUint16(start + 26, true);
  const extraLength = view.getUint16(start + 28, true);
  const dataStart = start + 30 + nameLength + extraLength;
  const dataEnd = dataStart + entry.compressedSize;
  if (dataEnd > bytes.length) throw new Error("The ZIP backup contains a truncated file.");
  return bytes.subarray(dataStart, dataEnd);
}

async function readZipEntry(view, bytes, entry, maxOutputBytes) {
  if (entry.uncompressedSize > maxOutputBytes) throw new Error("A file in the ZIP backup exceeds the demo import size limit.");
  const compressed = findZipEntry(view, bytes, entry);
  if (entry.compressionMethod === 0) return compressed;
  if (entry.compressionMethod !== 8 || typeof DecompressionStream === "undefined") {
    throw new Error("This browser cannot read the compression used by the ZIP backup.");
  }
  let stream;
  try {
    stream = new Blob([compressed]).stream().pipeThrough(new DecompressionStream("deflate-raw"));
  } catch {
    throw new Error("This browser cannot read the compression used by the ZIP backup.");
  }
  const output = new Uint8Array(await new Response(stream).arrayBuffer());
  if (output.length !== entry.uncompressedSize || output.length > maxOutputBytes) {
    throw new Error("A file in the ZIP backup has an invalid size.");
  }
  return output;
}

function parseZipDirectory(bytes) {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  const minimum = Math.max(0, bytes.length - 65557);
  let endOffset = -1;
  for (let offset = bytes.length - 22; offset >= minimum; offset -= 1) {
    if (view.getUint32(offset, true) === 0x06054b50) {
      endOffset = offset;
      break;
    }
  }
  if (endOffset < 0) throw new Error("The selected file is not a valid War Room ZIP backup.");
  const entryCount = view.getUint16(endOffset + 10, true);
  const directorySize = view.getUint32(endOffset + 12, true);
  const directoryOffset = view.getUint32(endOffset + 16, true);
  if (entryCount > 10001 || directoryOffset + directorySize > endOffset) {
    throw new Error("The ZIP backup has an invalid file directory.");
  }
  const entries = new Map();
  let offset = directoryOffset;
  for (let index = 0; index < entryCount; index += 1) {
    if (offset + 46 > bytes.length || view.getUint32(offset, true) !== 0x02014b50) {
      throw new Error("The ZIP backup has an invalid file directory.");
    }
    const flags = view.getUint16(offset + 8, true);
    const compressionMethod = view.getUint16(offset + 10, true);
    const compressedSize = view.getUint32(offset + 20, true);
    const uncompressedSize = view.getUint32(offset + 24, true);
    const nameLength = view.getUint16(offset + 28, true);
    const extraLength = view.getUint16(offset + 30, true);
    const commentLength = view.getUint16(offset + 32, true);
    const localHeaderOffset = view.getUint32(offset + 42, true);
    const nameStart = offset + 46;
    const nameEnd = nameStart + nameLength;
    if (nameEnd + extraLength + commentLength > bytes.length || flags & 1) {
      throw new Error("The ZIP backup contains an unsupported or invalid file entry.");
    }
    const name = new TextDecoder().decode(bytes.subarray(nameStart, nameEnd));
    if (name.startsWith("/") || name.split("/").includes("..")) {
      throw new Error("The ZIP backup contains an unsafe file path.");
    }
    if (entries.has(name)) throw new Error("The ZIP backup contains duplicate file paths.");
    entries.set(name, { compressionMethod, compressedSize, uncompressedSize, localHeaderOffset });
    offset = nameEnd + extraLength + commentLength;
  }
  return { view, entries };
}

function logoDomain(job) {
  const explicit = String(job.company_domain || "").trim().replace(/^https?:\/\//i, "").split(/[/?#]/)[0].toLowerCase();
  if (explicit) return explicit.replace(/\.$/, "");
  let company = String(job.company_name || "").toLowerCase().trim();
  for (const suffix of [" technologies", " technology", " inc", " corp", " ltd", " llc", " s.l.", " sl", " gmbh", " sa"]) {
    company = company.replace(new RegExp(`${suffix.replace(/[.*+?^${}()|[\\]\\]/g, "\\$&")}$`), "");
  }
  company = company.replaceAll(" ", "");
  return company.length > 2 ? `${company}.com` : "";
}

async function readApplicationZip(file) {
  if (!file || file.size > MAX_ARCHIVE_BYTES) throw new Error("Choose a War Room ZIP backup no larger than 500 MiB.");
  const bytes = new Uint8Array(await file.arrayBuffer());
  const { view, entries } = parseZipDirectory(bytes);
  const manifestEntry = entries.get("manifest.json");
  if (!manifestEntry) throw new Error("The ZIP does not contain a War Room backup manifest.");
  const manifestBytes = await readZipEntry(view, bytes, manifestEntry, MAX_ARCHIVE_MANIFEST_BYTES);
  let manifest;
  try {
    manifest = JSON.parse(new TextDecoder().decode(manifestBytes));
  } catch {
    throw new Error("The ZIP backup manifest is not valid JSON.");
  }
  if (manifest?.format !== "war-room-backup" || manifest.version !== 1 || !Array.isArray(manifest.jobs)) {
    throw new Error("The ZIP is not a supported War Room backup.");
  }
  const jobs = manifest.jobs.map((job) => {
    if (!job || typeof job !== "object" || Array.isArray(job)) return job;
    return {
      ...job,
      interviewers: Array.isArray(job.interviewers) ? job.interviewers : [],
      stages: Array.isArray(job.stages) ? job.stages.map((stage) => {
        if (!stage || typeof stage !== "object" || Array.isArray(stage)) return stage;
        return {
          ...stage,
          interviewers: Array.isArray(stage.interviewers) ? stage.interviewers : [],
          questions: Array.isArray(stage.questions) ? stage.questions : [],
        };
      }) : [],
      attachments: Array.isArray(job.attachments) ? job.attachments : [],
    };
  });
  const logosByDomain = new Map();
  for (const logo of manifest.logos || []) {
    if (typeof logo?.domain !== "string" || typeof logo.path !== "string") throw new Error("The ZIP backup contains invalid company icon metadata.");
    const entry = entries.get(logo.path);
    if (!entry || entry.uncompressedSize > MAX_ARCHIVE_LOGO_BYTES) throw new Error("The ZIP backup is missing a company icon.");
    const logoBytes = await readZipEntry(view, bytes, entry, MAX_ARCHIVE_LOGO_BYTES);
    const mime = String(logo.mime_type || "");
    if (!["image/png", "image/jpeg", "image/webp", "image/svg+xml"].includes(mime) || logoBytes.length !== logo.size || await sha256(logoBytes) !== String(logo.sha256).toLowerCase()) {
      throw new Error("A company icon in the ZIP backup failed validation.");
    }
    let binary = "";
    for (let offset = 0; offset < logoBytes.length; offset += 0x8000) {
      binary += String.fromCharCode(...logoBytes.subarray(offset, offset + 0x8000));
    }
    logosByDomain.set(logo.domain.toLowerCase(), `data:${mime};base64,${btoa(binary)}`);
  }
  const companyLogos = {};
  for (const job of jobs) {
    const logo = logosByDomain.get(logoDomain(job));
    if (logo) companyLogos[job.id] = logo;
  }
  const files = new Map();
  for (const job of jobs) {
    for (const attachment of job.attachments) {
      const entry = entries.get(attachment.stored_filename);
      if (!entry || entry.uncompressedSize > 50 * 1024 * 1024) throw new Error("The ZIP backup is missing an uploaded file.");
      const fileBytes = await readZipEntry(view, bytes, entry, 50 * 1024 * 1024);
      const hash = await sha256(fileBytes);
      if (hash !== manifest.attachment_sha256?.[attachment.id]?.toLowerCase() || fileBytes.length !== attachment.file_size) {
        throw new Error("An uploaded file in the ZIP backup failed validation.");
      }
      files.set(attachment.id, new Blob([fileBytes], { type: attachment.mime_type || "application/octet-stream" }));
    }
  }
  return { format: BACKUP_FORMAT, version: BACKUP_VERSION, jobs, company_logos: companyLogos, files };
}

async function sha256(bytes) {
  const digest = new Uint8Array(await crypto.subtle.digest("SHA-256", bytes));
  return Array.from(digest, (byte) => byte.toString(16).padStart(2, "0")).join("");
}

function dataUrlBytes(dataUrl) {
  const comma = dataUrl.indexOf(",");
  const binary = atob(dataUrl.slice(comma + 1));
  return Uint8Array.from(binary, (character) => character.charCodeAt(0));
}

function logoExtension(mime) {
  return { "image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp", "image/svg+xml": ".svg" }[mime];
}

async function makeZipBackup() {
  const backupJobs = clone(jobs);
  const entries = [];
  const attachmentSHA256 = {};
  for (const job of backupJobs) {
    for (const attachment of job.attachments || []) {
      const blob = await readFile(attachment.id);
      if (!blob) throw new Error(`The uploaded file ${attachment.original_name} is missing from browser storage.`);
      const bytes = new Uint8Array(await blob.arrayBuffer());
      const name = String(attachment.original_name).replaceAll("\\", "/").split("/").pop() || "file";
      const path = `attachments/${attachment.id}/${name}`;
      attachment.stored_filename = path;
      attachment.file_size = bytes.length;
      attachmentSHA256[attachment.id] = await sha256(bytes);
      entries.push([path, bytes]);
    }
  }
  const logos = [];
  const seenDomains = new Set();
  const demoLogos = await collectDemoLogos();
  for (const job of backupJobs) {
    const domain = logoDomain(job);
    if (!domain || seenDomains.has(domain)) continue;
    const logo = demoLogos[job.id];
    if (!logo) continue;
    const mime = logo.slice(5, logo.indexOf(";"));
    const extension = logoExtension(mime);
    if (!extension) continue;
    const bytes = dataUrlBytes(logo);
    const path = `logos/${domain}${extension}`;
    logos.push({ domain, path, mime_type: mime, size: bytes.length, sha256: await sha256(bytes) });
    entries.push([path, bytes]);
    seenDomains.add(domain);
  }
  const manifest = { format: "war-room-backup", version: 1, exported_at: new Date().toISOString(), jobs: backupJobs, attachment_sha256: attachmentSHA256, logos };
  entries.push(["manifest.json", new TextEncoder().encode(JSON.stringify(manifest))]);
  return createZip(entries);
}

async function readDemoLogoData(job) {
  const logoAsset = getDemoLogoAsset(job.company_name);
  if (!logoAsset) return "";
  const response = await fetch(`/demo/assets/logos/${logoAsset}`);
  if (!response.ok) throw new Error(`Could not include the company icon for ${job.company_name} in the backup.`);
  const blob = await response.blob();
  if (!blob.type.startsWith("image/") || blob.size > 1024 * 1024) {
    throw new Error(`The company icon for ${job.company_name} is not a supported image.`);
  }
  const bytes = new Uint8Array(await blob.arrayBuffer());
  let binary = "";
  for (let offset = 0; offset < bytes.length; offset += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + 0x8000));
  }
  return `data:${blob.type};base64,${btoa(binary)}`;
}

async function collectDemoLogos() {
  let storedLogos = {};
  try {
    storedLogos = JSON.parse(window.localStorage.getItem(DEMO_LOGOS_STORAGE_KEY) || "{}");
  } catch {
    storedLogos = {};
  }
  const logos = {};
  for (const job of jobs) {
    const stored = storedLogos[job.id];
    if (typeof stored === "string" && stored.startsWith("data:image/")) {
      logos[job.id] = stored;
      continue;
    }
    const logo = await readDemoLogoData(job);
    if (logo) logos[job.id] = logo;
  }
  return logos;
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
  getCompanyLogoUrl(companyName, _companyDomain, jobId) { return getDemoLogoUrl(companyName, jobId); },
  async exportBackup() { return makeZipBackup(); },
  async importBackup(file, allowEmpty = false) {
    const backup = await readApplicationZip(file);
    const { jobs: importedJobs, logos } = validateBackup(backup);
    const files = backup.files;
    if (importedJobs.length === 0 && !allowEmpty) {
      const error = new Error("The backup contains no saved demo processes.");
      error.requiresEmptyConfirmation = true;
      throw error;
    }
    const previousJobs = window.localStorage.getItem(STORAGE_KEY);
    const previousLogos = window.localStorage.getItem(DEMO_LOGOS_STORAGE_KEY);
    try {
      window.localStorage.setItem(DEMO_LOGOS_STORAGE_KEY, JSON.stringify(logos));
      window.localStorage.setItem(STORAGE_KEY, JSON.stringify(importedJobs));
      await replaceFiles(files);
    } catch {
      try {
        if (previousJobs === null) window.localStorage.removeItem(STORAGE_KEY);
        else window.localStorage.setItem(STORAGE_KEY, previousJobs);
        if (previousLogos === null) window.localStorage.removeItem(DEMO_LOGOS_STORAGE_KEY);
        else window.localStorage.setItem(DEMO_LOGOS_STORAGE_KEY, previousLogos);
      } catch {
        // Preserve the import error when browser storage is unavailable.
      }
      throw new Error("The backup could not be saved in this browser's storage.");
    }
    cacheDemoLogos(logos);
    jobs = importedJobs;
    return { success: true };
  },
  async getJobs(status = "all", search = "") {
    return clone(jobs.filter((job) => (status === "all" || job.status === status) && meetsSearch(job, search)).sort((a, b) => a.order_index - b.order_index));
  },
  async getJobCounts() {
    return { all: jobs.length, ongoing: jobs.filter((job) => job.status === "ongoing").length, accepted: jobs.filter((job) => job.status === "accepted").length, rejected: jobs.filter((job) => job.status === "rejected").length };
  },
  async getJob(id) {
    const job = clone(getJob(id));
    for (const attachment of job.attachments || []) {
      const blob = await readFile(attachment.id);
      if (blob) attachment.download_url = URL.createObjectURL(blob);
    }
    return job;
  },
  async createJob(payload) { const job = makeJob(payload); jobs.push(job); save(); return clone(job); },
  async updateJob(id, payload) { const job = getJob(id); Object.assign(job, payload, { updated_at: Date.now() }); refreshDerived(job); save(); return clone(job); },
  async deleteJob(id) {
    const job = getJob(id);
    for (const attachment of job.attachments || []) await deleteFile(attachment.id);
    jobs = jobs.filter((item) => item.id !== String(id)); save(); return { success: true };
  },
  async reorderJobs(ids) { const order = new Map(ids.map((id, index) => [String(id), index])); jobs.forEach((job) => { if (order.has(job.id)) job.order_index = order.get(job.id); }); jobs.sort((a, b) => a.order_index - b.order_index); save(); return { success: true }; },
  async createStage(payload) { const job = getJob(payload.job_id); const stage = makeStage(job.id, payload, job.stages.length); job.stages.push(stage); refreshDerived(job); save(); return clone(stage); },
  async updateStage(id, payload) { const { job, stage } = getStage(id); Object.assign(stage, payload); refreshDerived(job); save(); return clone(stage); },
  async setCurrentStage(id) { const { job, stage } = getStage(id); job.stages.forEach((item) => { item.status = item.id === stage.id ? "current" : item.status === "current" ? "completed" : item.status; }); refreshDerived(job); save(); return clone(stage); },
  async getMeetings(date = "") { return jobs.flatMap((job) => job.stages.filter((stage) => stage.meeting_date && (!date || stage.meeting_date === date)).map((stage) => toMeeting(job, stage))); },
  async scheduleMeeting(id, payload) { const { stage } = getStage(id); Object.assign(stage, payload); save(); return clone(stage); },
  async deleteStage(id) { const { job, stage } = getStage(id); job.stages = job.stages.filter((item) => item.id !== stage.id); job.attachments.forEach((item) => { if (item.stage_id === stage.id) item.stage_id = null; }); job.stages.forEach((item, index) => { item.order_index = index; }); refreshDerived(job); save(); return { success: true }; },
  async reorderStages(jobId, ids) { const job = getJob(jobId); const byId = new Map(job.stages.map((stage) => [stage.id, stage])); job.stages = ids.map((id) => byId.get(String(id))).filter(Boolean); job.stages.forEach((stage, index) => { stage.order_index = index; }); refreshDerived(job); save(); return { success: true }; },
  async createQuestion(payload) { const { stage } = getStage(payload.stage_id); const question = { id: `demo-question-${crypto.randomUUID()}`, stage_id: stage.id, order_index: stage.questions.length, question: payload.question, answer_notes: payload.answer_notes || "", is_asked: false, created_at: Date.now() }; stage.questions.push(question); save(); return clone(question); },
  async reorderQuestions(stageId, ids) { const { stage } = getStage(stageId); const byId = new Map(stage.questions.map((question) => [question.id, question])); stage.questions = ids.map((id) => byId.get(String(id))).filter(Boolean); stage.questions.forEach((question, index) => { question.order_index = index; }); save(); return { success: true }; },
  async updateQuestion(id, payload) { for (const job of jobs) for (const stage of job.stages) { const question = stage.questions.find((item) => item.id === String(id)); if (question) { Object.assign(question, payload); save(); return clone(question); } } throw new Error("Interview question not found"); },
  async deleteQuestion(id) { for (const job of jobs) for (const stage of job.stages) { const question = stage.questions.find((item) => item.id === String(id)); if (question) { stage.questions = stage.questions.filter((item) => item.id !== question.id); save(); return { success: true }; } } throw new Error("Interview question not found"); },
  async uploadAttachment(formData) {
    const job = getJob(formData.get("job_id"));
    const file = formData.get("file");
    const stageId = formData.get("stage_id") || null;
    if (!(file instanceof Blob) || file.size > 50 * 1024 * 1024 || !file.size) throw new Error("Choose a file smaller than 50 MiB.");
    if (stageId && !job.stages.some((stage) => stage.id === stageId)) throw new Error("Interview stage not found");
    const attachment = { id: crypto.randomUUID(), job_id: job.id, stage_id: stageId, original_name: file.name || "file", stored_filename: "", file_size: file.size, mime_type: file.type || "application/octet-stream", created_at: Date.now() };
    await saveFile(attachment.id, file);
    job.attachments.push(attachment);
    save();
    return clone(attachment);
  },
  async deleteAttachment(id) {
    for (const job of jobs) {
      if (!job.attachments.some((item) => item.id === String(id))) continue;
      await deleteFile(String(id));
      job.attachments = job.attachments.filter((item) => item.id !== String(id));
      save();
      return { success: true };
    }
    throw new Error("Attachment not found");
  },
};

export async function resetDemoData() {
  await replaceFiles(new Map());
  jobs = clone(initialJobs);
  window.localStorage.removeItem(DEMO_LOGOS_STORAGE_KEY);
  cacheDemoLogos({});
  save();
}
