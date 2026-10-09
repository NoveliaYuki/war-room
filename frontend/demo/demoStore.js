import { initialJobs } from "./data/jobs.js";
import { cacheDemoLogos, DEMO_LOGOS_STORAGE_KEY, getDemoLogoAsset, getDemoLogoUrl } from "./logoStore.js";
import { saveFile, readFile, deleteFile, replaceFiles } from "./fileStore.js";
import { createZip } from "./zipWriter.js";
import { formatLocalDate, getDemoMeetingDayOffset, setDemoMeetingDate } from "./data/meetingDates.js";
import { migrateLegacyDemoJob } from "../public/js/utils/demoMigration.js";
import { initialTechnologies } from "./data/technologies.js";

const STORAGE_KEY = "war-room-demo-data-v13";
const TECHNOLOGY_STORAGE_KEY = "war-room-demo-technologies-v1";
const SCHEDULE_DATE_KEY = "war-room-demo-schedule-date";
const DEMO_SEED_VERSION_KEY = "war-room-demo-seed-version";
const DEMO_SEED_VERSION = "6";
const NEW_DEMO_SEED_IDS = new Set(["demo-10", "demo-11", "demo-12"]);
const NEW_DEMO_TECHNOLOGY_IDS = new Set(["tech-aws", "tech-terraform"]);
const BACKUP_FORMAT = "war-room-demo-backup";
const BACKUP_VERSION = 1;
const MAX_BACKUP_BYTES = 4 * 1024 * 1024;
const MAX_ARCHIVE_BYTES = 500 * 1024 * 1024;
const MAX_ARCHIVE_EXPANDED_BYTES = 1024 * 1024 * 1024;
const MAX_ARCHIVE_MANIFEST_BYTES = 20 * 1024 * 1024;
const MAX_ARCHIVE_LOGO_BYTES = 1024 * 1024;
const MAX_ARCHIVE_ENTRIES = 10001;
const CV_VERSIONS_KEY = "war-room-demo-cv-versions-v1";
const NEXT_CV_VERSION_KEY = "war-room-demo-next-cv-version-v1";
const SEARCH_PERIODS_KEY = "war-room-demo-search-periods-v1";
const clone = (value) => JSON.parse(JSON.stringify(value));

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
    if (seedChanged || window.localStorage.getItem(SCHEDULE_DATE_KEY) !== today) {
      refreshDemoMeetingDates(records, today);
      window.localStorage.setItem(STORAGE_KEY, JSON.stringify(records));
      window.localStorage.setItem(SCHEDULE_DATE_KEY, today);
      window.localStorage.setItem(DEMO_SEED_VERSION_KEY, DEMO_SEED_VERSION);
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
    for (const field of ["created_at", "updated_at", "status_changed_at"]) {
      if (Number(job[field]) > 1e12) job[field] = Math.floor(Number(job[field]) / 1000);
    }
    if (!Number(job.status_changed_at)) {
      job.status_changed_at = seededJobs.get(job.id)?.status_changed_at || Number(job.created_at) || now;
    }
  });
}

let jobs = loadJobs();
let searchPeriods = loadSearchPeriods();

function loadSearchPeriods() {
  try {
    const value = JSON.parse(window.localStorage.getItem(SEARCH_PERIODS_KEY) || "[]");
    return Array.isArray(value) ? value : [];
  } catch { return []; }
}

function saveSearchPeriods() {
  try { window.localStorage.setItem(SEARCH_PERIODS_KEY, JSON.stringify(searchPeriods)); } catch { /* Keep periods available for this tab. */ }
}

function validateSearchPeriod(payload, excludeID = "") {
  const name = String(payload.name || "").trim();
  const startDate = String(payload.start_date || "");
  const endDate = String(payload.end_date || "");
  const datePattern = /^\d{4}-\d{2}-\d{2}$/;
  const isCalendarDate = (value) => datePattern.test(value)
    && Number.isFinite(Date.parse(`${value}T00:00:00Z`))
    && new Date(`${value}T00:00:00Z`).toISOString().slice(0, 10) === value;
  if (!name || name.length > 100 || !isCalendarDate(startDate) || !isCalendarDate(endDate)
    || startDate > endDate) throw new Error("Enter a name and valid start and end dates.");
  if (searchPeriods.some((period) => period.id !== excludeID && period.start_date <= endDate && period.end_date >= startDate)) {
    throw new Error("Search period dates overlap an existing period.");
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

function validateBackup(value) {
  if (value?.format !== BACKUP_FORMAT || value.version !== BACKUP_VERSION || !Array.isArray(value.jobs)) {
    throw new Error("This is not a supported War Room demo JSON backup.");
  }
  if (value.jobs.length > 10000) throw new Error("The backup contains too many processes.");
  const importedJobs = clone(value.jobs);
  const importedTechnologies = Array.isArray(value.technologies) ? clone(value.technologies) : [];
  const technologyIds = new Set();
  const technologyTerms = new Set();
  for (const item of importedTechnologies) {
    const key = (term) => String(term || "").trim().toLowerCase().replace(/\s+/g, " ");
    if (!item || typeof item.id !== "string" || !item.id.trim() || technologyIds.has(item.id) || typeof item.name !== "string" || !item.name.trim() || !Array.isArray(item.aliases)) {
      throw new Error("The backup contains an invalid technology catalog.");
    }
    technologyIds.add(item.id);
    for (const term of [item.name, ...item.aliases]) {
      const normalized = key(term);
      if (!normalized || technologyTerms.has(normalized)) throw new Error("The backup contains duplicate technology names or aliases.");
      technologyTerms.add(normalized);
    }
  }
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
    job.technologies = Array.isArray(job.technologies) ? job.technologies : [];
    if (job.technologies.some((item) => !item || !technologyIds.has(item.id))) throw new Error("A process refers to a technology missing from the backup catalog.");
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
  const importedCvVersions = Array.isArray(value.cv_versions) ? clone(value.cv_versions) : [];
  if (importedCvVersions.length > 1000) throw new Error("The backup contains too many CV versions.");
  const cvIds = new Set();
  for (const version of importedCvVersions) {
    const checksum = String(version?.sha256 || "").toLowerCase();
    if (!version || typeof version.id !== "string" || !version.id.trim() || cvIds.has(version.id)
      || !Number.isInteger(Number(version.version_number)) || Number(version.version_number) < 1
      || typeof version.original_name !== "string" || !version.original_name
      || version.path !== `cvs/${checksum}`
      || !Number.isInteger(Number(version.file_size)) || Number(version.file_size) < 0
      || !/^[a-f0-9]{64}$/.test(checksum)) {
      throw new Error("The backup contains invalid CV version metadata.");
    }
    version.sha256 = checksum;
    version.stored_file_id = checksum;
    cvIds.add(version.id);
  }
  const minimumNextCVVersion = Math.max(1, ...importedCvVersions.map((version) => Number(version.version_number) + 1));
  const providedNextCVVersion = Number(value.next_cv_version);
  if (value.next_cv_version !== undefined && (!Number.isInteger(providedNextCVVersion) || providedNextCVVersion < minimumNextCVVersion)) {
    throw new Error("The backup CV version sequence is lower than its saved versions.");
  }
  const nextCVVersion = Number.isInteger(providedNextCVVersion) && providedNextCVVersion >= minimumNextCVVersion
    ? providedNextCVVersion : minimumNextCVVersion;
  for (const job of importedJobs) {
    if (job.selected_cv_version && !cvIds.has(String(job.selected_cv_version.id || ""))) {
      throw new Error("A process refers to a CV version missing from the backup.");
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
  return { jobs: importedJobs, technologies: importedTechnologies, logos: importedLogos, cvVersions: importedCvVersions, nextCVVersion };
}

async function readZipEntry(view, bytes, entry, maxOutputBytes) {
  const outputLimit = Math.min(maxOutputBytes, MAX_ARCHIVE_EXPANDED_BYTES - entry.expandedBefore);
  if (entry.uncompressedSize > outputLimit) throw new Error("A file in the ZIP backup exceeds the demo import size limit.");
  const compressed = findZipEntry(view, bytes, entry);
  if (entry.compressionMethod === 0) {
    if (compressed.length !== entry.uncompressedSize || crc32(compressed) !== entry.crc32) {
      throw new Error("A file in the ZIP backup has invalid contents.");
    }
    return compressed;
  }
  if (entry.compressionMethod !== 8 || typeof DecompressionStream === "undefined") {
    throw new Error("This browser cannot read the compression used by the ZIP backup.");
  }
  let stream;
  try {
    stream = new Blob([compressed]).stream().pipeThrough(new DecompressionStream("deflate-raw"));
  } catch {
    throw new Error("This browser cannot read the compression used by the ZIP backup.");
  }
  const output = await readBoundedZipOutput(stream, outputLimit, entry.uncompressedSize);
  if (crc32(output) !== entry.crc32) {
    throw new Error("A file in the ZIP backup has an invalid checksum.");
  }
  return output;
}

async function readBoundedZipOutput(stream, outputLimit, expectedSize) {
  const reader = stream.getReader();
  const chunks = [];
  let total = 0;
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      total += value.length;
      if (total > outputLimit || total > expectedSize) {
        throw new Error("A file in the ZIP backup exceeds its declared size limit.");
      }
      chunks.push(value);
    }
  } catch (error) {
    await reader.cancel().catch(() => {});
    throw error;
  } finally {
    reader.releaseLock();
  }
  if (total !== expectedSize) {
    throw new Error("A file in the ZIP backup has an invalid size.");
  }
  const output = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    output.set(chunk, offset);
    offset += chunk.length;
  }
  return output;
}

function crc32(bytes) {
  let value = 0xffffffff;
  for (const byte of bytes) {
    value ^= byte;
    for (let bit = 0; bit < 8; bit += 1) value = value & 1 ? 0xedb88320 ^ (value >>> 1) : value >>> 1;
  }
  return (value ^ 0xffffffff) >>> 0;
}

function findZipEntry(view, bytes, entry) {
  const start = entry.localHeaderOffset;
  const nameLength = view.getUint16(start + 26, true);
  const extraLength = view.getUint16(start + 28, true);
  const dataStart = start + 30 + nameLength + extraLength;
  return bytes.subarray(dataStart, dataStart + entry.compressedSize);
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
  const diskNumber = view.getUint16(endOffset + 4, true);
  const directoryDisk = view.getUint16(endOffset + 6, true);
  const diskEntries = view.getUint16(endOffset + 8, true);
  const entryCount = view.getUint16(endOffset + 10, true);
  const directorySize = view.getUint32(endOffset + 12, true);
  const directoryOffset = view.getUint32(endOffset + 16, true);
  if (diskNumber !== 0 || directoryDisk !== 0 || diskEntries !== entryCount
    || entryCount > MAX_ARCHIVE_ENTRIES || directoryOffset + directorySize !== endOffset
    || endOffset + 22 + view.getUint16(endOffset + 20, true) !== bytes.length) {
    throw new Error("The ZIP backup has an invalid file directory.");
  }
  const entries = new Map();
  const payloadRanges = [];
  let expandedSize = 0;
  let localHeadersValid = true;
  let offset = directoryOffset;
  for (let index = 0; index < entryCount; index += 1) {
    if (offset + 46 > bytes.length || view.getUint32(offset, true) !== 0x02014b50) {
      throw new Error("The ZIP backup has an invalid file directory.");
    }
    const flags = view.getUint16(offset + 8, true);
    const compressionMethod = view.getUint16(offset + 10, true);
    const crc32 = view.getUint32(offset + 16, true);
    const compressedSize = view.getUint32(offset + 20, true);
    const uncompressedSize = view.getUint32(offset + 24, true);
    const nameLength = view.getUint16(offset + 28, true);
    const extraLength = view.getUint16(offset + 30, true);
    const commentLength = view.getUint16(offset + 32, true);
    const localHeaderOffset = view.getUint32(offset + 42, true);
    const nameStart = offset + 46;
    const nameEnd = nameStart + nameLength;
    if (nameEnd + extraLength + commentLength > endOffset || flags & 0x0001 || flags & 0x0008
      || ![0, 8].includes(compressionMethod)) {
      throw new Error("The ZIP backup contains an unsupported or invalid file entry.");
    }
    const name = new TextDecoder().decode(bytes.subarray(nameStart, nameEnd));
    if (!name || name.startsWith("/") || name.includes("\\") || name.split("/").includes("..")) {
      throw new Error("The ZIP backup contains an unsafe file path.");
    }
    if (entries.has(name)) throw new Error("The ZIP backup contains duplicate file paths.");
    expandedSize += uncompressedSize;
    if (expandedSize > MAX_ARCHIVE_EXPANDED_BYTES) {
      throw new Error("The ZIP backup expands beyond the 1 GiB demo import limit.");
    }
    const entry = { name, flags, compressionMethod, crc32, compressedSize, uncompressedSize, localHeaderOffset, directoryOffset, expandedBefore: expandedSize - uncompressedSize };
    entries.set(name, entry);
    payloadRanges.push(entry);
    if (!Number.isSafeInteger(localHeaderOffset) || localHeaderOffset < 0
      || localHeaderOffset + 30 > directoryOffset
      || view.getUint32(localHeaderOffset, true) !== 0x04034b50) {
      localHeadersValid = false;
    } else {
      const localNameLength = view.getUint16(localHeaderOffset + 26, true);
      const localExtraLength = view.getUint16(localHeaderOffset + 28, true);
      const localNameStart = localHeaderOffset + 30;
      const localDataStart = localNameStart + localNameLength + localExtraLength;
      const localName = new TextDecoder().decode(bytes.subarray(localNameStart, localNameStart + localNameLength));
      if (localName !== name || view.getUint16(localHeaderOffset + 6, true) !== flags
        || view.getUint16(localHeaderOffset + 8, true) !== compressionMethod
        || view.getUint32(localHeaderOffset + 14, true) !== crc32
        || view.getUint32(localHeaderOffset + 18, true) !== compressedSize
        || view.getUint32(localHeaderOffset + 22, true) !== uncompressedSize
        || localDataStart > directoryOffset) {
        localHeadersValid = false;
      }
    }
    offset = nameEnd + extraLength + commentLength;
  }
  if (offset !== directoryOffset + directorySize || !localHeadersValid) {
    throw new Error("The ZIP backup has inconsistent file metadata.");
  }
  payloadRanges.sort((left, right) => left.localHeaderOffset - right.localHeaderOffset);
  for (let index = 0; index < payloadRanges.length; index += 1) {
    const entry = payloadRanges[index];
    const nextStart = payloadRanges[index + 1]?.localHeaderOffset ?? directoryOffset;
    const dataStart = entry.localHeaderOffset + 30 + view.getUint16(entry.localHeaderOffset + 26, true)
      + view.getUint16(entry.localHeaderOffset + 28, true);
    const dataEnd = dataStart + entry.compressedSize;
    if (entry.localHeaderOffset < 0 || dataStart > dataEnd || dataEnd > nextStart) {
      throw new Error("The ZIP backup contains overlapping or invalid file entries.");
    }
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
  if (manifest?.format !== "war-room-backup" || ![1, 2, 3, 4].includes(manifest.version) || !Array.isArray(manifest.jobs)) {
    throw new Error("The ZIP is not a supported War Room backup.");
  }
  if (manifest.version === 1 && Array.isArray(manifest.cv_versions) && manifest.cv_versions.length) {
    throw new Error("Legacy v1 backups cannot contain CV versions.");
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
  const periods = Array.isArray(manifest.search_periods) ? clone(manifest.search_periods) : [];
  periods.sort((left, right) => String(left.start_date).localeCompare(String(right.start_date)));
  const periodIDs = new Set();
  periods.forEach((period, index) => {
    if (!period || typeof period.id !== "string" || !period.id || periodIDs.has(period.id)
      || typeof period.name !== "string" || !period.name.trim()
      || !/^\d{4}-\d{2}-\d{2}$/.test(period.start_date) || !/^\d{4}-\d{2}-\d{2}$/.test(period.end_date)
      || period.start_date > period.end_date || (index > 0 && periods[index - 1].end_date >= period.start_date)) {
      throw new Error("The ZIP backup contains invalid or overlapping search periods.");
    }
    periodIDs.add(period.id);
  });
  if (jobs.some((job) => job.search_period_id && !periodIDs.has(job.search_period_id))) {
    throw new Error("A process refers to a search period missing from the backup.");
  }
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
  const cvVersions = Array.isArray(manifest.cv_versions) ? manifest.cv_versions : [];
  if (cvVersions.length > 1000) throw new Error("The backup contains too many CV versions.");
  for (const version of cvVersions) {
    const checksum = String(version?.sha256 || "").toLowerCase();
    if (!version || typeof version.id !== "string" || !version.id || !/^[a-f0-9]{64}$/.test(checksum)
      || version.path !== `cvs/${checksum}` || !Number.isInteger(Number(version.version_number))
      || Number(version.version_number) < 1 || typeof version.original_name !== "string" || !version.original_name
      || !Number.isInteger(Number(version.file_size)) || Number(version.file_size) < 0) {
      throw new Error("The ZIP backup contains invalid CV version metadata.");
    }
    const entry = entries.get(version.path);
    if (!entry || entry.uncompressedSize > 50 * 1024 * 1024) throw new Error("The ZIP backup is missing a CV version file.");
    const fileBytes = await readZipEntry(view, bytes, entry, 50 * 1024 * 1024);
    if (fileBytes.length !== version.file_size || await sha256(fileBytes) !== checksum) {
      throw new Error("A CV version in the ZIP backup failed validation.");
    }
    version.sha256 = checksum;
    version.stored_file_id = checksum;
    files.set(checksum, new Blob([fileBytes], { type: version.mime_type || "application/pdf" }));
  }
  return { format: BACKUP_FORMAT, version: BACKUP_VERSION, jobs, search_periods: periods, technologies: Array.isArray(manifest.technologies) ? manifest.technologies : [], company_logos: companyLogos, files, cv_versions: cvVersions, next_cv_version: manifest.next_cv_version };
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
  const backupCvVersions = clone(readDemoCvVersions());
  const cvArchivePaths = new Set();
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
  for (const version of backupCvVersions) {
    const blob = await readFile(version.stored_file_id || version.id);
    if (!blob) throw new Error(`CV version ${version.version_number} is missing from browser storage.`);
    const bytes = new Uint8Array(await blob.arrayBuffer());
    const storageId = version.stored_file_id || version.id;
    version.stored_file_id = storageId;
    version.file_size = bytes.length;
    version.sha256 = await sha256(bytes);
    version.path = `cvs/${version.sha256}`;
    if (!cvArchivePaths.has(version.path)) {
      entries.push([version.path, bytes]);
      cvArchivePaths.add(version.path);
    }
  }
  const backupCvById = new Map(backupCvVersions.map((version) => [version.id, version]));
  for (const job of backupJobs) {
    const selected = job.selected_cv_version;
    const version = selected && backupCvById.get(selected.id);
    if (version) {
      job.selected_cv_version = {
        id: version.id, version_number: version.version_number, original_name: version.original_name,
        file_size: version.file_size, mime_type: version.mime_type, sha256: version.sha256, uploaded_at: version.uploaded_at,
      };
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
  const publicCvVersions = backupCvVersions.map(({ stored_file_id, stored_filename, ...version }) => version);
  const manifest = { format: "war-room-backup", version: 4, exported_at: new Date().toISOString(), jobs: backupJobs, search_periods: clone(searchPeriods), technologies: clone(technologies), attachment_sha256: attachmentSHA256, cv_versions: publicCvVersions, next_cv_version: getDemoNextCvVersion(backupCvVersions), logos };
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

function technologyKey(value) { return String(value || "").trim().toLowerCase().replace(/\s+/g, " "); }

function validateDemoTechnology(name, aliases, excludeID = "") {
  const cleanName = String(name || "").trim();
  const cleanAliases = [...new Set((aliases || []).map((alias) => String(alias || "").trim()).filter(Boolean))];
  if (!cleanName || cleanName.length > 80 || cleanAliases.length > 20 || cleanAliases.some((alias) => alias.length > 80)) throw new Error("Enter a technology name up to 80 characters and no more than 20 aliases.");
  const terms = [cleanName, ...cleanAliases].map(technologyKey);
  if (new Set(terms).size !== terms.length) throw new Error("Technology names and aliases must be unique.");
  for (const item of technologies) {
    if (item.id === excludeID) continue;
    const existing = [item.name, ...(item.aliases || [])].map(technologyKey);
    if (terms.some((term) => existing.includes(term))) throw new Error("That technology name or alias already exists in the catalog.");
  }
  return { name: cleanName, aliases: cleanAliases };
}

function jobWithCurrentTechnologies(job) {
  return { ...job, technologies: (job.technologies || []).map((item) => technologies.find((technology) => technology.id === item.id) || item).map((item) => ({ id: item.id, name: item.name })) };
}

function listDemoTechnologies() {
  return clone(technologies.map((item) => ({ ...item, jobs: jobs.filter((job) => (job.technologies || []).some((entry) => entry.id === item.id)).map((job) => ({ id: job.id, company_name: job.company_name, position_title: job.position_title })) })));
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
  const nowSeconds = Math.floor(now / 1000);
  const id = `demo-job-${crypto.randomUUID()}`;
  const localToday = new Date();
  const today = [localToday.getFullYear(), String(localToday.getMonth() + 1).padStart(2, "0"), String(localToday.getDate()).padStart(2, "0")].join("-");
  const currentPeriod = searchPeriods.find((period) => period.start_date <= today && period.end_date >= today);
  const requestedPeriod = Object.hasOwn(payload, "search_period_id") ? String(payload.search_period_id || "") : currentPeriod?.id || "";
  if (requestedPeriod && !searchPeriods.some((period) => period.id === requestedPeriod)) throw new Error("Search period not found");
  const job = {
    id, company_name: payload.company_name || "Unknown", position_title: payload.position_title,
    status: "waiting", salary_type: payload.salary_type || "unknown", salary_min: payload.salary_min ?? null,
    salary_max: payload.salary_max ?? null, salary_currency: payload.salary_currency || "EUR",
    recruiter_type: payload.recruiter_type || "none", recruiter_name: payload.recruiter_name || null,
    recruiter_agency: payload.recruiter_agency || null, recruiter_contact: null,
    job_post_url: payload.job_post_url || null, avatar_seed: payload.company_name || "Unknown",
    keyword_note: payload.keyword_note || "", description: payload.description || "",
    company_overview: payload.company_overview || "", company_domain: payload.company_domain || null,
    interview_notes: payload.interview_notes || "", reasons_to_change: payload.reasons_to_change || "",
    experience_notes: payload.experience_notes || "", expected_salary: payload.expected_salary || "",
    work_arrangement: payload.work_arrangement || "unknown", employment_type: payload.employment_type || "unknown",
    search_period_id: requestedPeriod || null,
    is_referral: Boolean(payload.is_referral), order_index: jobs.length, created_at: nowSeconds, status_changed_at: nowSeconds, updated_at: nowSeconds,
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
  async exportBackup() { return makeZipBackup(); },
  async importBackup(file, allowEmpty = false) {
    const backup = await readApplicationZip(file);
    const { jobs: importedJobs, search_periods: importedPeriods, technologies: importedTechnologies, logos, cvVersions, nextCVVersion } = validateBackup(backup);
    const files = backup.files;
    if (importedJobs.length === 0 && !allowEmpty) {
      const error = new Error("The backup contains no saved demo processes.");
      error.requiresEmptyConfirmation = true;
      throw error;
    }
    const previousJobs = window.localStorage.getItem(STORAGE_KEY);
    const previousTechnologies = window.localStorage.getItem(TECHNOLOGY_STORAGE_KEY);
    const previousLogos = window.localStorage.getItem(DEMO_LOGOS_STORAGE_KEY);
    const previousCvVersions = window.localStorage.getItem(CV_VERSIONS_KEY);
    const previousNextCVVersion = window.localStorage.getItem(NEXT_CV_VERSION_KEY);
    const previousSearchPeriods = window.localStorage.getItem(SEARCH_PERIODS_KEY);
    try {
      window.localStorage.setItem(DEMO_LOGOS_STORAGE_KEY, JSON.stringify(logos));
      window.localStorage.setItem(STORAGE_KEY, JSON.stringify(importedJobs));
      window.localStorage.setItem(SEARCH_PERIODS_KEY, JSON.stringify(importedPeriods));
      window.localStorage.setItem(TECHNOLOGY_STORAGE_KEY, JSON.stringify(importedTechnologies));
      saveDemoCvVersions(cvVersions);
      saveDemoNextCvVersion(nextCVVersion);
      await replaceFiles(files);
    } catch {
      try {
        if (previousJobs === null) window.localStorage.removeItem(STORAGE_KEY);
        else window.localStorage.setItem(STORAGE_KEY, previousJobs);
        if (previousTechnologies === null) window.localStorage.removeItem(TECHNOLOGY_STORAGE_KEY);
        else window.localStorage.setItem(TECHNOLOGY_STORAGE_KEY, previousTechnologies);
        if (previousLogos === null) window.localStorage.removeItem(DEMO_LOGOS_STORAGE_KEY);
        else window.localStorage.setItem(DEMO_LOGOS_STORAGE_KEY, previousLogos);
        if (previousCvVersions === null) window.localStorage.removeItem(CV_VERSIONS_KEY);
        else window.localStorage.setItem(CV_VERSIONS_KEY, previousCvVersions);
        if (previousNextCVVersion === null) window.localStorage.removeItem(NEXT_CV_VERSION_KEY);
        else window.localStorage.setItem(NEXT_CV_VERSION_KEY, previousNextCVVersion);
        if (previousSearchPeriods === null) window.localStorage.removeItem(SEARCH_PERIODS_KEY);
        else window.localStorage.setItem(SEARCH_PERIODS_KEY, previousSearchPeriods);
      } catch {
        // Preserve the import error when browser storage is unavailable.
      }
      throw new Error("The backup could not be saved in this browser's storage.");
    }
    cacheDemoLogos(logos);
    jobs = importedJobs;
    searchPeriods = importedPeriods;
    technologies = importedTechnologies;
    return { success: true };
  },
  async getJobs(status = "all", search = "") {
    return clone(jobs.filter((job) => (status === "all" || job.status === status) && meetsSearch(job, search)).sort((a, b) => a.order_index - b.order_index).map(jobWithCurrentTechnologies));
  },
  async getSearchPeriods() {
    return clone(searchPeriods.map((period) => ({ ...period, job_count: jobs.filter((job) => job.search_period_id === period.id).length }))
      .sort((left, right) => right.start_date.localeCompare(left.start_date)));
  },
  async createSearchPeriod(payload) {
    const valid = validateSearchPeriod(payload);
    const now = Math.floor(Date.now() / 1000);
    const period = { id: `demo-period-${crypto.randomUUID()}`, ...valid, created_at: now, updated_at: now };
    searchPeriods.push(period); saveSearchPeriods(); return clone(period);
  },
  async updateSearchPeriod(id, payload) {
    const period = searchPeriods.find((item) => item.id === String(id));
    if (!period) throw new Error("Search period not found");
    Object.assign(period, validateSearchPeriod(payload, period.id), { updated_at: Math.floor(Date.now() / 1000) });
    saveSearchPeriods(); return clone(period);
  },
  async deleteSearchPeriod(id) {
    const period = searchPeriods.find((item) => item.id === String(id));
    if (!period) throw new Error("Search period not found");
    jobs.forEach((job) => { if (job.search_period_id === period.id) job.search_period_id = null; });
    searchPeriods = searchPeriods.filter((item) => item.id !== period.id);
    saveSearchPeriods(); save(); return { success: true };
  },
  async getJobCounts() {
    return { all: jobs.length, waiting: jobs.filter((job) => job.status === "waiting").length, ongoing: jobs.filter((job) => job.status === "ongoing").length, accepted: jobs.filter((job) => job.status === "accepted").length, rejected: jobs.filter((job) => job.status === "rejected").length };
  },
  async getJob(id) {
    const job = clone(jobWithCurrentTechnologies(getJob(id)));
    for (const attachment of job.attachments || []) {
      const blob = await readFile(attachment.id);
      if (blob) attachment.download_url = URL.createObjectURL(blob);
    }
    return job;
  },
  async getCvVersions() { return clone(readDemoCvVersions()); },
  async uploadCvVersion(file) {
    if (!(file instanceof Blob) || !file.size) throw new Error("Choose a non-empty CV file.");
    const versions = readDemoCvVersions();
    const digest = await crypto.subtle.digest("SHA-256", await file.arrayBuffer());
    const sha256 = [...new Uint8Array(digest)].map((byte) => byte.toString(16).padStart(2, "0")).join("");
    const existing = versions.find((version) => version.sha256 === sha256);
    const nextVersion = getDemoNextCvVersion(versions);
    const record = { id: crypto.randomUUID(), version_number: nextVersion, original_name: file.name || "CV.pdf", file_size: file.size, mime_type: file.type || "application/pdf", sha256, uploaded_at: Math.floor(Date.now() / 1000), stored_file_id: existing?.stored_file_id || existing?.id || "" };
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
    if (!version) throw new Error("CV version not found");
    const blob = await readFile(version.stored_file_id || version.id);
    if (!blob) throw new Error("CV version not found");
    return blob;
  },
  async deleteCvVersion(id) {
    if (jobs.some((job) => job.selected_cv_version?.id === String(id))) throw new Error("This CV version is assigned to a job and cannot be deleted.");
    const versions = readDemoCvVersions();
    if (!versions.some((version) => version.id === String(id))) throw new Error("CV version not found");
    const remaining = versions.filter((version) => version.id !== String(id));
    const storageId = versions.find((version) => version.id === String(id))?.stored_file_id || String(id);
    if (!remaining.some((version) => (version.stored_file_id || version.id) === storageId)) await deleteFile(storageId);
    saveDemoCvVersions(remaining);
    return { success: true };
  },
  async setJobCvVersion(jobId, versionId) {
    const job = getJob(jobId);
    const version = versionId === null ? null : readDemoCvVersions().find((item) => item.id === String(versionId));
    if (versionId !== null && !version) throw new Error("CV version not found");
    job.selected_cv_version = version ? clone(version) : null;
    save();
    return { success: true, selected_cv_version: clone(job.selected_cv_version) };
  },
  async getTechnologies() { return listDemoTechnologies(); },
  async createTechnology(payload) {
    const normalized = validateDemoTechnology(payload.name, payload.aliases || []);
    const item = { id: `tech-${crypto.randomUUID()}`, ...normalized };
    technologies.push(item); saveTechnologies(); return clone(item);
  },
  async updateTechnology(id, payload) {
    const item = technologies.find((technology) => technology.id === String(id));
    if (!item) throw new Error("Technology not found");
    Object.assign(item, validateDemoTechnology(payload.name, payload.aliases || [], item.id));
    jobs.forEach((job) => (job.technologies || []).forEach((entry) => { if (entry.id === item.id) entry.name = item.name; }));
    saveTechnologies(); save(); return clone(item);
  },
  async removeTechnologyAssignments(id) {
    const item = technologies.find((technology) => technology.id === String(id));
    if (!item) throw new Error("Technology not found");
    let removed = 0;
    jobs.forEach((job) => { const before = (job.technologies || []).length; job.technologies = (job.technologies || []).filter((entry) => entry.id !== item.id); removed += before - job.technologies.length; });
    save(); return { removed };
  },
  async deleteTechnology(id) {
    const item = technologies.find((technology) => technology.id === String(id));
    if (!item) throw new Error("Technology not found");
    if (jobs.some((job) => (job.technologies || []).some((entry) => entry.id === item.id))) throw new Error("Technology is assigned to one or more processes; remove its assignments first.");
    technologies = technologies.filter((technology) => technology.id !== item.id); saveTechnologies(); return { success: true };
  },
  async createJob(payload) { const job = makeJob(payload); jobs.push(job); save(); return clone(job); },
  async updateJob(id, payload) {
    const job = getJob(id);
    const statusChanged = Object.hasOwn(payload, "status") && payload.status !== job.status;
    const nowSeconds = Math.floor(Date.now() / 1000);
    const fields = { ...payload };
    if (Object.hasOwn(fields, "search_period_id")) {
      const periodID = String(fields.search_period_id || "");
      if (periodID && !searchPeriods.some((period) => period.id === periodID)) throw new Error("Search period not found");
      job.search_period_id = periodID || null;
      delete fields.search_period_id;
    }
    if (Object.hasOwn(fields, "technology_ids")) {
      const ids = fields.technology_ids;
      if (!Array.isArray(ids) || new Set(ids).size !== ids.length || ids.some((technologyID) => !technologies.some((item) => item.id === technologyID))) throw new Error("Choose technologies from the catalog.");
      job.technologies = ids.map((technologyID) => { const item = technologies.find((technology) => technology.id === technologyID); return { id: item.id, name: item.name }; });
      delete fields.technology_ids;
    }
    Object.assign(job, fields, { updated_at: nowSeconds });
    if (statusChanged) job.status_changed_at = nowSeconds;
    refreshDerived(job); save(); return clone(jobWithCurrentTechnologies(job));
  },
  async deleteJob(id) {
    const job = getJob(id);
    for (const attachment of job.attachments || []) await deleteFile(attachment.id);
    jobs = jobs.filter((item) => item.id !== String(id)); save(); return { success: true };
  },
  async reorderJobs(ids) { const order = new Map(ids.map((id, index) => [String(id), index])); jobs.forEach((job) => { if (order.has(job.id)) job.order_index = order.get(job.id); }); jobs.sort((a, b) => a.order_index - b.order_index); save(); return { success: true }; },
  async createStage(payload) { const job = getJob(payload.job_id); const firstStage = job.stages.length === 0; const stage = makeStage(job.id, payload, job.stages.length); job.stages.push(stage); if (firstStage) { job.status = "ongoing"; job.status_changed_at = Math.floor(Date.now() / 1000); } refreshDerived(job); save(); return clone(stage); },
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
  window.localStorage.removeItem(CV_VERSIONS_KEY);
  window.localStorage.removeItem(NEXT_CV_VERSION_KEY);
  window.localStorage.removeItem(DEMO_LOGOS_STORAGE_KEY);
  cacheDemoLogos({});
  save();
}
