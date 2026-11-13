import { renderDetailLoadingSkeleton } from "./loadingViews.js";
import { captureLoadingView, markLoadingStarted, revealLoadedContent } from "../utils/loadingTransition.js";
import { showToast } from "../utils/toast.js";
/**
 * @fileoverview Card Detail Modal Component.
 * Implements an ergonomic dual-pane split layout:
 * - Left Pane: Job details, recruiter contact, interviewer roster, attachments, edit/delete.
 * - Right Pane: Dedicated Stage selector and Interview Questions to ask (no scrolling needed!).
 */

import { api } from "../api.js";
import { renderCompanyAvatar } from "../avatar.js";
import { closeWithFlip, cancelPendingFlipClose } from "../flip.js";
import { icon } from "../icons.js";
import { activateModal, focusModalContents, restoreModalFocus } from "../modalA11y.js";
import { enableQuestionReordering } from "./questionList.js";
import { makeInlineEditable, parseSalaryInput } from "../inlineEdit.js";
import { escapeHtml, escapeAttr, safeUrl } from "../utils/sanitize.js";
import { renderMarkdown } from "../utils/markdown.js";
import { formatSalary } from "../utils/salary.js";
import { formatCvUploadDate } from "./cvLibrary.js";
import { confirmAction } from "../utils/confirmDialog.js";
import { formatCalendarDate } from "../utils/date.js";
import { renderMeetingLinkContent } from "../utils/meetingLink.js";
import { getLocale, t } from "../i18n.js";
import { arrangementLabel, companyText, editableCompanyName, employmentTypeLabel, historyActionLabel, isUndisclosedCompany, meetingFormatLabel, stageDescriptionText, stageName, stageTypeLabel, statusLabel } from "../utils/labels.js";

const MEETING_LINK_FIELDS = new Map([
  ["video", { labelKey: "schedule.form.videoUrl", hintKey: "schedule.form.videoUrlHint", placeholder: "https://meet.example.com/..." }],
  ["onsite", { labelKey: "schedule.form.mapsUrl", hintKey: "schedule.form.mapsUrlHint", placeholder: "https://maps.app.goo.gl/..." }],
]);
const STAGE_TYPE_CLASSES = new Map([
  ["HR", "HR"],
  ["Technical", "Technical"],
  ["Cultural", "Cultural"],
  ["Offer & Decision", "Offer-Decision"],
]);

const SPLIT_RATIO_STORAGE_KEY = "war-room-detail-split-ratio";
const DEFAULT_SPLIT_RATIO = 47;
const MIN_SPLIT_RATIO = 20;
const MAX_SPLIT_RATIO = 80;
const MIN_LEFT_PANE_PX = 340;
const MIN_RIGHT_PANE_PX = 400;
const SPLIT_GUTTER_PX = 14;
const JOB_STATUS_CYCLE = ["waiting", "ongoing", "rejected", "accepted"];
const OPEN_JOB_STORAGE_KEY = "war-room.open-job";
const splitResizeObservers = new WeakMap();
const stageTabResizeObservers = new WeakMap();
let detailLoadRequestId = 0;

/** Keeps both desktop panes wide enough for their contents. */
function getSplitRatioLimits(layoutWidth) {
  const usableWidth = layoutWidth - SPLIT_GUTTER_PX;
  if (usableWidth < MIN_LEFT_PANE_PX + MIN_RIGHT_PANE_PX) {
    return { minimum: MIN_SPLIT_RATIO, maximum: MAX_SPLIT_RATIO };
  }
  return {
    minimum: Math.max(MIN_SPLIT_RATIO, Math.ceil((MIN_LEFT_PANE_PX / usableWidth) * 100)),
    maximum: Math.min(MAX_SPLIT_RATIO, Math.floor(((usableWidth - MIN_RIGHT_PANE_PX) / usableWidth) * 100)),
  };
}

/** Reads and bounds the saved left-pane percentage. */
function readSplitRatio() {
  try {
    const savedRatio = Number(window.localStorage.getItem(SPLIT_RATIO_STORAGE_KEY));
    return Number.isFinite(savedRatio) && savedRatio >= MIN_SPLIT_RATIO && savedRatio <= MAX_SPLIT_RATIO
      ? savedRatio
      : DEFAULT_SPLIT_RATIO;
  } catch {
    return DEFAULT_SPLIT_RATIO;
  }
}

/** Adds a draggable, keyboard-accessible divider between the detail panes. */
function bindSplitPaneResize(modalEl) {
  splitResizeObservers.get(modalEl)?.disconnect();
  const layout = modalEl.querySelector(".modal-split-layout");
  const handle = modalEl.querySelector(".split-pane-resizer");
  if (!layout || !handle) return;

  let preferredRatio = readSplitRatio();
  let ratio = preferredRatio;
  const applyRatio = (nextRatio, persist = true, limits = getSplitRatioLimits(layout.getBoundingClientRect().width), preservePreference = false) => {
    if (!preservePreference) preferredRatio = Math.round(nextRatio);
    ratio = Math.min(limits.maximum, Math.max(limits.minimum, Math.round(nextRatio)));
    layout.style.setProperty("--split-left-fr", `${ratio}fr`);
    layout.style.setProperty("--split-right-fr", `${100 - ratio}fr`);
    handle.setAttribute("aria-valuenow", String(ratio));
    handle.setAttribute("aria-valuemin", String(limits.minimum));
    handle.setAttribute("aria-valuemax", String(limits.maximum));
    if (persist) {
      try {
        window.localStorage.setItem(SPLIT_RATIO_STORAGE_KEY, String(ratio));
      } catch {
        // Resizing remains available when browser storage is disabled.
      }
    }
  };

  applyRatio(preferredRatio, false, getSplitRatioLimits(layout.getBoundingClientRect().width), true);
  const observer = new ResizeObserver(() => {
    applyRatio(preferredRatio, false, getSplitRatioLimits(layout.getBoundingClientRect().width), true);
  });
  observer.observe(layout);
  splitResizeObservers.set(modalEl, observer);
  handle.addEventListener("keydown", (event) => {
    if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
      event.preventDefault();
      applyRatio(ratio + (event.key === "ArrowRight" ? 2 : -2));
    } else if (event.key === "Home" || event.key === "End") {
      event.preventDefault();
      applyRatio(event.key === "Home" ? MIN_SPLIT_RATIO : MAX_SPLIT_RATIO);
    }
  });

  handle.addEventListener("pointerdown", (event) => {
    if (event.button !== undefined && event.button !== 0) return;
    event.preventDefault();
    handle.classList.add("is-dragging");
    document.body.classList.add("is-resizing-split");

    const bounds = layout.getBoundingClientRect();
    const usableWidth = bounds.width - SPLIT_GUTTER_PX;
    const limits = getSplitRatioLimits(bounds.width);
    let latestClientX = null;
    let animationFrame = null;
    const applyPointerPosition = (clientX) => {
      if (usableWidth > 0) {
        applyRatio(((clientX - bounds.left - SPLIT_GUTTER_PX / 2) / usableWidth) * 100, false, limits);
      }
    };
    const onPointerMove = (moveEvent) => {
      latestClientX = moveEvent.clientX;
      if (animationFrame !== null) return;
      animationFrame = window.requestAnimationFrame(() => {
        animationFrame = null;
        applyPointerPosition(latestClientX);
      });
    };
    const stopDragging = () => {
      if (animationFrame !== null) {
        window.cancelAnimationFrame(animationFrame);
        animationFrame = null;
      }
      if (latestClientX !== null) applyPointerPosition(latestClientX);
      applyRatio(ratio, true, limits);
      handle.classList.remove("is-dragging");
      document.body.classList.remove("is-resizing-split");
      window.removeEventListener("pointermove", onPointerMove);
      window.removeEventListener("pointerup", stopDragging);
      window.removeEventListener("pointercancel", stopDragging);
    };

    window.addEventListener("pointermove", onPointerMove);
    window.addEventListener("pointerup", stopDragging);
    window.addEventListener("pointercancel", stopDragging);
  });
}

/** Maps a stage type to a known presentation class. */
function getStageTypeClass(type) {
  return STAGE_TYPE_CLASSES.get(type) || "HR";
}

/** Returns a stage-level value, falling back to the job-level value on stage one. */
function resolveStageAttribute(stage, key, job, stageIndex, fallback = "") {
  if (stage?.[key] !== undefined && stage[key] !== null) return stage[key];
  if (stageIndex === 0 && job[key]) return job[key];
  return fallback;
}

/** Chooses the interview roster for the active stage. */
function resolveStageInterviewers(stage, job, stageIndex) {
  if (!stage) return [];
  if (Array.isArray(stage?.interviewers) && stage.interviewers.length > 0) return stage.interviewers;
  return stageIndex > 0 && job.interviewers?.length ? job.interviewers : [];
}

/** Creates the normalized values required to render the modal. */
function createModalView(job) {
  const stages = Array.isArray(job.stages)
    ? job.stages.map((stage) => ({
      ...stage,
      questions: Array.isArray(stage.questions) ? stage.questions : [],
      interviewers: Array.isArray(stage.interviewers) ? stage.interviewers : [],
    }))
    : [];
  activeStageIndex = Math.min(activeStageIndex, Math.max(0, stages.length - 1));
  const activeStage = stages[activeStageIndex] || null;
  const attachments = Array.isArray(job.attachments) ? job.attachments : [];
  return {
    job,
    stages,
    activeStage,
    activeStageIndex,
    stageRecruiterType: resolveStageAttribute(activeStage, "recruiter_type", job, activeStageIndex, "none"),
    stageRecruiterName: resolveStageAttribute(activeStage, "recruiter_name", job, activeStageIndex),
    stageRecruiterAgency: resolveStageAttribute(activeStage, "recruiter_agency", job, activeStageIndex),
    stageRecruiterContact: resolveStageAttribute(activeStage, "recruiter_contact", job, activeStageIndex),
    stageInterviewers: resolveStageInterviewers(activeStage, job, activeStageIndex),
    generalAttachments: attachments.filter((attachment) => !attachment.stage_id),
    stageAttachments: activeStage ? attachments.filter((attachment) => attachment.stage_id === activeStage.id) : [],
    avatarHtml: renderCompanyAvatar(job.company_name, job.avatar_seed, 56, job.company_domain, job.id),
  };
}

/** Returns the accessible company label used in the modal header. */
function renderCompanyLabel(companyName) {
  return isUndisclosedCompany(companyName)
    ? `<span class="inline-icon-text">${icon("building", 13)} ${escapeHtml(t("company.unknownUndisclosed"))}</span>`
    : escapeHtml(companyName);
}

/** Returns the raw salary value used by inline editing. */
function getSalaryRawValue(job) {
  if (job.salary_min && job.salary_max) return `${job.salary_min} - ${job.salary_max}`;
  return job.salary_max || job.salary_min || t("salary.undisclosed");
}

/** Returns the next job status in the modal's click cycle. */
function getNextJobStatus(status) {
  const currentIndex = JOB_STATUS_CYCLE.indexOf(status);
  return currentIndex === -1 ? "waiting" : JOB_STATUS_CYCLE[(currentIndex + 1) % JOB_STATUS_CYCLE.length];
}

/** Escapes optional note text or returns its empty-state markup. */
function renderOptionalText(value, emptyMarkup) {
  return value ? escapeHtml(value) : emptyMarkup;
}

/** Renders an optional Markdown field with its empty-state prompt. */
function renderMarkdownField(value, placeholder) {
  return value ? renderMarkdown(value) : `<span class="empty-note">${escapeHtml(placeholder)}</span>`;
}

/** Renders an optional job link only when it uses a safe web URL. */
function renderJobPostLink(value, emptyLabel = t("detail.noLink")) {
  if (safeUrl(value)) return `<a href="${escapeAttr(safeUrl(value))}" target="_blank" rel="noopener noreferrer" class="inline-icon-text detail-external-link" style="word-break: break-all; color: var(--accent-blue);">${escapeHtml(value)} ${icon("arrowUpRight", 11)}</a>`;
  return value ? escapeHtml(value) : `<span style="color: var(--text-muted); font-style: italic; font-size: 12px;">${escapeHtml(emptyLabel)}</span>`;
}

/** Renders application and first recruiter contact dates. */
function renderProcessTimeline(job) {
  return `<div class="detail-section process-timeline-section">
    <div class="section-title"><span>${escapeHtml(t("detail.processDates"))}</span></div>
    <div class="process-timeline-list">
      <label class="process-timeline-item" for="application-sent-date"><span>${escapeHtml(t("detail.applicationSent"))}</span><input id="application-sent-date" type="date" data-process-date="application_sent_date" value="${escapeAttr(job.application_sent_date || "")}" /></label>
      <label class="process-timeline-item" for="recruiter-first-contact-date"><span>${escapeHtml(t("detail.firstRecruiterContact"))}</span><input id="recruiter-first-contact-date" type="date" data-process-date="recruiter_first_contact_date" value="${escapeAttr(job.recruiter_first_contact_date || "")}" /></label>
    </div>
  </div>`;
}

/** Formats recent activity relatively and older activity as a dated timestamp. */
function formatJobHistoryDate(timestamp) {
  const date = new Date(Number(timestamp) * 1000);
  if (!Number.isFinite(date.getTime())) return "";
  const elapsedMinutes = Math.floor((Date.now() - date.getTime()) / 60000);
  if (elapsedMinutes < 1) return t("history.justNow");
  if (elapsedMinutes < 60) return new Intl.RelativeTimeFormat(getLocale(), { numeric: "always" }).format(-elapsedMinutes, "minute");
  const elapsedHours = Math.floor(elapsedMinutes / 60);
  if (elapsedHours < 24) return new Intl.RelativeTimeFormat(getLocale(), { numeric: "always" }).format(-elapsedHours, "hour");
  const parts = new Intl.DateTimeFormat(getLocale(), { weekday: "short", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit", hourCycle: "h23" }).formatToParts(date);
  const values = Object.fromEntries(parts.map(({ type, value }) => [type, value]));
  return `${values.weekday} ${values.day} ${values.month} ${values.hour}:${values.minute}`;
}

/** Renders dated changes and only the saved legacy dates available for a job. */
const STATUSES_WITH_HISTORY = new Set(["waiting", "ongoing", "accepted", "rejected"]);

function renderJobHistory(job) {
  const history = Array.isArray(job.history) ? [...job.history] : [];
  const hasCreatedEvent = history.some((event) => event.action === "created");
  const createdAt = Number(job.created_at);
  if (!hasCreatedEvent && createdAt > 0) history.push({ occurred_at: createdAt, action: "created" });
  const hasStatusEvent = history.some((event) => String(event.action || "").startsWith("moved to "));
  const statusChangedAt = Number(job.status_changed_at);
  if (!hasStatusEvent && statusChangedAt > createdAt && STATUSES_WITH_HISTORY.has(job.status)) {
    history.push({ occurred_at: statusChangedAt, action: `moved to ${job.status}` });
  }
  const entries = history
    .filter((event) => Number(event.occurred_at) > 0 && event.action)
    .sort((a, b) => Number(b.occurred_at) - Number(a.occurred_at));
  if (!entries.length) return "";
  return `<details class="detail-section card-history-section">
    <summary class="card-history-toggle"><span>${escapeHtml(t("history.title"))}</span><span class="card-history-toggle-meta"><span>${entries.length}</span><span class="card-history-chevron">${icon("chevronDown", 12)}</span></span></summary>
    <ol class="card-history-list">${entries.map((event) => {
      const timestamp = Number(event.occurred_at);
      const label = historyActionLabel(event.action);
      return `<li class="card-history-item"><span>${escapeHtml(label)}</span><time datetime="${escapeAttr(new Date(timestamp * 1000).toISOString())}">${escapeHtml(formatJobHistoryDate(timestamp))}</time></li>`;
    }).join("")}</ol>
  </details>`;
}

/** Renders the optional original job description section. */
function renderJobDescription(job) {
  return `<div class="detail-section">
    <div class="section-title">
      <span class="inline-icon-text">${icon("clipboard", 13)} ${escapeHtml(t("detail.originalDescription"))}</span>
      ${job.description ? `<button class="btn-secondary btn-toggle-reachout" style="padding: 2px 8px; font-size: 11px;">${escapeHtml(t("detail.toggle"))}</button>` : ''}
      <button type="button" class="btn-secondary markdown-field-edit" data-markdown-field="description">${escapeHtml(t("common.edit"))}</button>
    </div>
    <div id="reachout-text-container" class="editable-description markdown-preview" data-raw-value="${escapeAttr(job.description || '')}" style="background: var(--bg-surface-elevated); border: 1px solid var(--border-subtle); border-radius: var(--radius-sm); padding: 10px 14px; font-size: 12px; line-height: 1.45; color: var(--text-secondary); max-height: 260px; overflow-y: auto; display: none;">${renderMarkdownField(job.description, t("detail.descriptionEmpty"))}</div>
  </div>`;
}

/** Renders the general attachment chips. */
function renderGeneralAttachments(attachments) {
  if (!attachments.length) return `<div style="font-size: 12px; color: var(--text-muted); font-style: italic;">${escapeHtml(t("detail.noGeneralFiles"))}</div>`;
  return attachments.map((att) => `<div class="attachment-chip">
    <div class="attachment-meta"><span>${icon("fileText", 14)}</span><div>
      <div class="attachment-name" title="${escapeAttr(att.original_name)}">${escapeHtml(att.original_name)}</div>
      <div class="attachment-size">${escapeHtml(formatFileSize(att.file_size))} • ${escapeHtml(t("detail.generalFile"))}</div>
    </div></div>
    <div style="display: flex; gap: 6px;">
      <a href="${escapeAttr(att.download_url || "#")}" class="stage-action-btn" title="${escapeAttr(t("common.download"))}" download="${escapeAttr(att.original_name)}">${icon("download", 12)}</a>
      <button class="stage-action-btn btn-del-attachment" data-id="${escapeAttr(att.id)}" title="${escapeAttr(t("common.remove"))}">${icon("close", 11)}</button>
    </div>
  </div>`).join("");
}

/** Formats an attachment size in kilobytes with the active locale's decimal separator. */
function formatFileSize(bytes) {
  const kilobytes = new Intl.NumberFormat(getLocale(), { minimumFractionDigits: 1, maximumFractionDigits: 1 }).format(bytes / 1024);
  return t("detail.fileSizeKb", { size: kilobytes });
}

/** Renders the selected shared CV version without mixing library management into job details. */
function renderSelectedCvVersion(job) {
  const version = job.selected_cv_version;
  const summary = version
    ? `<span class="job-cv-version">${escapeHtml(t("cv.version", { version: version.version_number }))}${version.original_name ? ` · ${escapeHtml(version.original_name)}` : ""}</span>`
    : `<span class="job-cv-version is-empty">${escapeHtml(t("detail.noCvSelected"))}</span>`;
  return `<div class="job-cv-row" id="job-cv-row"><span class="inline-icon-text">${icon("fileText", 13)} ${escapeHtml(t("detail.cvSent"))}</span>${summary}<button type="button" class="btn-secondary job-cv-change" aria-label="${escapeAttr(t("detail.changeCvVersion"))}">${escapeHtml(t(version ? "detail.change" : "detail.select"))}</button><button type="button" class="btn-secondary job-cv-manage">${escapeHtml(t("detail.manageCvVersions"))}</button></div>`;
}

/** Renders recruiter metadata for the active stage. */
function renderStageRecruiterDetails(view) {
  const { stageRecruiterType, stageRecruiterName, stageRecruiterAgency, stageRecruiterContact } = view;
  const contactUrl = safeUrl(stageRecruiterContact);
  const contactValue = !stageRecruiterContact
    ? `<span style="color: var(--text-muted); font-style: italic;">${escapeHtml(t("detail.noneRecorded"))}</span>`
    : contactUrl
      ? `<a href="${escapeAttr(contactUrl)}" target="_blank" rel="noopener noreferrer" class="inline-icon-text detail-external-link">${escapeHtml(t(stageRecruiterContact.toLowerCase().includes("linkedin") ? "detail.linkedInProfile" : "detail.contactLink"))} ${icon("externalLink", 11)}</a>`
      : escapeHtml(stageRecruiterContact);
  return `<div class="meta-grid">
    <div class="meta-item"><span class="meta-label">${escapeHtml(t("detail.relationship"))}</span>
      <select class="stage-recruiter-source-select" aria-label="${escapeAttr(t("detail.relationship"))}">
        <option value="none" ${stageRecruiterType === "none" ? "selected" : ""}>${escapeHtml(t("detail.relationship.none"))}</option>
        <option value="internal" ${stageRecruiterType === "internal" ? "selected" : ""}>${escapeHtml(t("detail.relationship.internal"))}</option>
        <option value="external" ${stageRecruiterType === "external" ? "selected" : ""}>${escapeHtml(t("detail.relationship.external"))}</option>
      </select>
    </div>
    <div class="meta-item"><span class="meta-label">${escapeHtml(t("detail.personName"))}</span><span class="meta-value editable-stage-recruiter-name" data-raw-value="${escapeAttr(stageRecruiterName)}">${renderOptionalText(stageRecruiterName, `<span style="color: var(--text-muted); font-style: italic;">${escapeHtml(t("detail.noContactName"))}</span>`)}</span></div>
    <div class="meta-item"><span class="meta-label">${escapeHtml(t("detail.agencyDepartment"))}</span><span class="meta-value editable-stage-recruiter-agency" data-raw-value="${escapeAttr(stageRecruiterAgency)}">${renderOptionalText(stageRecruiterAgency, `<span style="color: var(--text-muted); font-style: italic;">${escapeHtml(t("detail.none"))}</span>`)}</span></div>
    <div class="meta-item"><span class="meta-label">${escapeHtml(t("detail.contactUrl"))}</span><span class="meta-value editable-stage-recruiter-contact" data-raw-value="${escapeAttr(stageRecruiterContact)}">${contactValue}</span></div>
  </div>`;
}

/** Renders one interviewer's editable information. */
function renderInterviewerRow(interviewer, index) {
  return `<div class="interviewer-row" data-idx="${index}"><div class="interviewer-info">
    <span class="interviewer-name editable-stage-interviewer-name" data-idx="${index}" data-raw-value="${escapeAttr(interviewer.name)}">${escapeHtml(interviewer.name)}</span>
    <span class="interviewer-role editable-stage-interviewer-role" data-idx="${index}" data-raw-value="${escapeAttr(interviewer.role || "")}">• ${escapeHtml(interviewer.role || t("interviewer.addRole"))}</span>
    <span class="interviewer-note editable-stage-interviewer-note" data-idx="${index}" data-raw-value="${escapeAttr(interviewer.notes || "")}">${interviewer.notes ? `(${escapeHtml(interviewer.notes)})` : `<span style="color: var(--text-muted); font-style: italic;">(${escapeHtml(t("interviewer.addNote"))})</span>`}</span>
  </div><button class="stage-action-btn btn-del-stage-interviewer" data-idx="${index}" style="color: var(--text-muted);" title="${escapeAttr(t("interviewer.remove"))}" aria-label="${escapeAttr(t("interviewer.removeLabel", { name: interviewer.name }))}">${icon("close", 11)}</button></div>`;
}

/** Renders the interviewer roster or its empty state. */
function renderInterviewerRoster(interviewers) {
  if (!interviewers.length) return `<div style="font-size: 12px; color: var(--text-muted); font-style: italic;">${escapeHtml(t("interviewer.none"))}</div>`;
  return interviewers.map(renderInterviewerRow).join("");
}

/** Renders attachment rows for an active stage. */
function renderStageAttachmentRows(view) {
  const { stageAttachments, activeStageIndex } = view;
  if (!stageAttachments.length) {
    return `<div style="font-size: 12px; color: var(--text-muted); font-style: italic;">${escapeHtml(t(activeStageIndex === 0 ? "detail.noScreeningDocuments" : "detail.noRoundFiles"))}</div>`;
  }
  return stageAttachments.map((att) => `<div class="attachment-chip"><div class="attachment-meta"><span>${icon("fileText", 14)}</span><div>
    <div class="attachment-name" title="${escapeAttr(att.original_name)}">${escapeHtml(att.original_name)}</div>
    <div class="attachment-size">${escapeHtml(formatFileSize(att.file_size))} • ${escapeHtml(activeStageIndex === 0 ? t("detail.screeningDocument") : t("stage.step", { number: activeStageIndex + 1 }))}</div>
  </div></div><div style="display: flex; gap: 6px;">
    <a href="${escapeAttr(att.download_url || "#")}" class="stage-action-btn" title="${escapeAttr(t("common.download"))}" download="${escapeAttr(att.original_name)}">${icon("download", 12)}</a>
    <button class="stage-action-btn btn-del-attachment" data-id="${escapeAttr(att.id)}" title="${escapeAttr(t("common.remove"))}">${icon("close", 11)}</button>
  </div></div>`).join("");
}

let activeStageIndex = 0;

/** Resolves the supported modal and job arguments. */
/** Picks the requested stage, falling back to the current one and then the first. */
function findInitialStageIndex(stages = [], stageId) {
  const requestedIdx = stageId ? stages.findIndex((stage) => stage.id === stageId) : -1;
  if (requestedIdx !== -1) return requestedIdx;
  return Math.max(0, stages.findIndex((stage) => stage.status === "current"));
}

function resolveDetailModalTarget(arg1, arg2, arg3, arg4) {
  if (typeof arg1 === "string") {
    return {
      jobId: arg1,
      onGlobalRefresh: arg2,
      modalEl: document.querySelector("#detail-modal"),
      backdropEl: document.querySelector("#modal-backdrop"),
    };
  }
  return {
    modalEl: arg1 || document.querySelector("#detail-modal"),
    backdropEl: arg2 || document.querySelector("#modal-backdrop"),
    jobId: arg3,
    onGlobalRefresh: arg4,
  };
}

/**
 * Renders the modal content and mounts event listeners.
 * Accepts either (arg1: jobId, arg2: onGlobalRefresh)
 * or (arg1: modalEl, arg2: backdropEl, arg3: jobId, arg4: onGlobalRefresh)
 */
export async function openDetailModal(arg1, arg2, arg3, arg4, { stageId } = {}) {
  const requestId = ++detailLoadRequestId;
  cancelPendingFlipClose();
  const { modalEl, backdropEl, jobId, onGlobalRefresh } = resolveDetailModalTarget(arg1, arg2, arg3, arg4);
  try { window.localStorage.setItem(OPEN_JOB_STORAGE_KEY, String(jobId)); } catch { /* Details remain available when browser storage is blocked. */ }

  const loadingTimer = prepareDetailLoading(modalEl, backdropEl, requestId);

  try {
    const job = await api.getJob(jobId);
    if (!isPendingDetailLoad(requestId, modalEl)) return;
    if (!job) {
      console.error("Job process not found:", jobId);
      clearFailedDetailLoad(modalEl, backdropEl);
      return;
    }
    activeStageIndex = findInitialStageIndex(job.stages, stageId);
    // Phone layouts show one pane at a time, so a requested stage needs its pane open.
    if (stageId) modalEl.dataset.mobileDetailPane = "stages";
    const loadingView = captureLoadingView(modalEl);
    renderModalContent(modalEl, backdropEl, job, onGlobalRefresh);
    revealLoadedContent(modalEl, loadingView);
    modalEl.removeAttribute("aria-busy");
    showDetailModal(modalEl, backdropEl);
    focusModalContents(modalEl, ".modal-close-btn");
  } catch (err) {
    if (!isPendingDetailLoad(requestId, modalEl)) return;
    console.error("Failed to load job details:", err);
    clearFailedDetailLoad(modalEl, backdropEl);
  } finally {
    window.clearTimeout(loadingTimer);
  }
}

/** Opens a ready dialog, or its loading shell after a noticeable wait. */
function showDetailModal(modalEl, backdropEl) {
  backdropEl?.classList.add("active");
  activateModal(modalEl);
}

function isPendingDetailLoad(requestId, modalEl) {
  return requestId === detailLoadRequestId && Boolean(modalEl.querySelector(".detail-loading-skeleton"));
}

/** Keeps fast job requests from flashing an empty loading dialog. */
function prepareDetailLoading(modalEl, backdropEl, requestId) {
  modalEl.setAttribute("aria-busy", "true");
  markLoadingStarted(modalEl);
  modalEl.innerHTML = renderDetailLoadingSkeleton();
  modalEl.querySelector(".modal-close-btn")?.addEventListener("click", () => {
    detailLoadRequestId += 1;
    clearFailedDetailLoad(modalEl, backdropEl);
  });
  if (backdropEl?.classList.contains("active")) {
    activateModal(modalEl);
    return null;
  }
  return window.setTimeout(() => {
    if (isPendingDetailLoad(requestId, modalEl)) showDetailModal(modalEl, backdropEl);
  }, 250);
}

/** Removes the empty modal shell after the requested job cannot be loaded. */
function clearFailedDetailLoad(modalEl, backdropEl) {
  try { window.localStorage.removeItem(OPEN_JOB_STORAGE_KEY); } catch { /* Keep the dialog usable when browser storage is blocked. */ }
  modalEl.innerHTML = "";
  modalEl.removeAttribute("aria-busy");
  backdropEl?.classList.remove("active");
  restoreModalFocus();
}

/** Renders one cohesive section of the job detail modal. */
function renderModalHeader(view) {
  const { job, avatarHtml } = view;
  return `

    <div class="modal-header">
      <div class="modal-header-main">
        <div class="modal-avatar">${avatarHtml}</div>
        <div class="modal-title-area">
          <div class="modal-company ${isUndisclosedCompany(job.company_name) ? 'is-unknown' : ''}">
            <span class="editable-company-name" data-raw-value="${escapeAttr(editableCompanyName(job.company_name))}">${renderCompanyLabel(job.company_name)}</span>
            <button type="button" class="editable-job-status" aria-label="${escapeAttr(t("detail.statusLabel", { status: detailStatusLabel(job.status) }))}" title="${escapeAttr(t("detail.statusTitle"))}"><span class="status-pill ${escapeAttr(job.status)}">${escapeHtml(detailStatusLabel(job.status))}</span></button>
          </div>
          <h2 class="modal-title editable-position-title" data-raw-value="${escapeAttr(job.position_title)}">${escapeHtml(job.position_title)}</h2>
        </div>
        <div class="modal-meta-pills">
            <button type="button" class="salary-tag editable-salary ${job.salary_type === 'unknown' ? 'undisclosed' : ''} inline-icon-text" data-raw-value="${escapeAttr(getSalaryRawValue(job))}" aria-label="${escapeAttr(t("detail.salaryLabel", { salary: formatSalary(job.salary_type, job.salary_min, job.salary_max, job.salary_currency) }))}" title="${escapeAttr(t("detail.salaryTitle"))}" data-edit-titled>
              ${escapeHtml(formatSalary(job.salary_type, job.salary_min, job.salary_max, job.salary_currency))}
            </button>
            <button type="button" class="referral-tag ${job.is_referral ? 'is-referral' : 'not-referral'} editable-modal-referral inline-icon-text" data-id="${escapeAttr(job.id)}" title="${escapeAttr(t("detail.referralTitle"))}" aria-label="${escapeAttr(t("detail.referralLabel", { label: t(job.is_referral ? "referral.yes" : "referral.none") }))}">
              ${icon("userCheck", 12)} ${escapeHtml(t(job.is_referral ? "referral.yes" : "referral.none"))}
            </button>
            ${renderJobMetadataTag("employment-type-tag", "employment_type", employmentTypeLabel(job.employment_type), {
              title: t("detail.employmentTypeTitle"), field: t("detail.employmentType"),
            })}
            ${renderJobMetadataTag("work-arrangement-tag", "work_arrangement", arrangementLabel(job.work_arrangement), {
              title: t("detail.workArrangementTitle"), field: t("detail.workArrangement"),
            })}
        </div>
        <div class="modal-header-actions">
          <button class="modal-close-btn" type="button" aria-label="${escapeAttr(t("common.closeDialog"))}" title="${escapeAttr(t("detail.closeTitle"))}">${icon("close", 14)}</button>
        </div>
      </div>
    </div>


  `;
}

/** Returns the detail header's status wording, where accepted reads as approved. */
function detailStatusLabel(status) {
  return status === "accepted" ? t("detail.statusApproved") : statusLabel(status);
}

/** Renders employment and work arrangement badges in the job detail header. */
function renderJobMetadataTag(className, field, valueLabel, labels) {
  const label = valueLabel || t("detail.notSpecified");
  return `<button type="button" class="${className}" data-cycle-field="${field}" title="${escapeAttr(labels.title)}" aria-label="${escapeAttr(t("detail.cycleLabel", { field: labels.field, value: label }))}">${escapeHtml(label)}</button>`;
}

/** Renders employer and role information inside the general process section. */
function renderJobDetailsSection(view) {
  const { job, generalAttachments } = view;
  return `
        <section class="process-subsection job-details-group">
          <div class="process-subsection-header"><span class="group-title inline-icon-text">${icon("building", 13)} ${escapeHtml(t("detail.jobDetails"))}</span></div>

          ${renderProcessTimeline(job)}
          ${renderJobHistory(job)}

          <div class="detail-section">
            <div class="section-title"><span>${escapeHtml(t("detail.roleHighlights"))}</span></div>
            <div class="keyword-note-box editable-keywords" data-raw-value="${escapeAttr(job.keyword_note || '')}">
              ${renderOptionalText(job.keyword_note, `<span style="color: var(--text-muted); font-style: italic;">${escapeHtml(t("detail.noKeywords"))}</span>`)}
            </div>
          </div>

          <div class="detail-section technology-detail-section">
            <div class="section-title"><span class="inline-icon-text">${icon("fileText", 13)} ${escapeHtml(t("detail.techStack"))}</span><span class="technology-count">${(job.technologies || []).length}</span></div>
            <div class="technology-assigned-list">${renderTechnologyChips(job.technologies || [])}</div>
            <label class="technology-search-label" for="technology-assignment-search">${escapeHtml(t("detail.addTechnologies"))}</label>
            <input id="technology-assignment-search" class="technology-assignment-search" type="search" autocomplete="off" placeholder="${escapeAttr(t("detail.searchTechnologies"))}" aria-controls="technology-assignment-options" />
            <div id="technology-assignment-options" class="technology-assignment-options" role="listbox" aria-label="${escapeAttr(t("detail.availableTechnologies"))}" hidden></div>
            <button type="button" class="btn-secondary technology-catalog-manage">${escapeHtml(t("technologies.manageCatalog"))}</button>
          </div>

          <div class="detail-section">
            <div class="section-title"><span class="inline-icon-text">${icon("externalLink", 12)} ${escapeHtml(t("detail.jobLinks"))}</span></div>
            <div class="job-post-link-fields">
              <div class="job-post-link-field">
                <div class="meta-label">${escapeHtml(t("detail.linkedInPost"))}</div>
                <div class="meta-item" style="background: var(--bg-surface-elevated); padding: 9px 12px; border: 1px solid var(--border-subtle); border-radius: var(--radius-sm);">
                  <span class="meta-value editable-job-post-url" data-raw-value="${escapeAttr(job.job_post_url || '')}">
                    ${renderJobPostLink(job.job_post_url, t("detail.noLinkedInPost"))}
                  </span>
                </div>
              </div>
              <div class="job-post-link-field">
                <div class="meta-label">${escapeHtml(t("detail.applicationPage"))}</div>
                <div class="meta-item" style="background: var(--bg-surface-elevated); padding: 9px 12px; border: 1px solid var(--border-subtle); border-radius: var(--radius-sm);">
                  <span class="meta-value editable-application-url" data-raw-value="${escapeAttr(job.application_url || '')}">
                    ${renderJobPostLink(job.application_url, t("detail.noApplicationLink"))}
                  </span>
                </div>
              </div>
            </div>
          </div>

          <div class="detail-section">
            <div class="section-title">
              <span class="inline-icon-text">${icon("building", 13)} ${escapeHtml(t("form.companyOverview"))}</span>
              <button type="button" class="btn-secondary markdown-field-edit" data-markdown-field="company_overview" aria-label="${escapeAttr(t("detail.editOverview"))}">${escapeHtml(t("common.edit"))}</button>
            </div>
            <div id="company-overview-display" class="editable-overview markdown-preview" data-raw-value="${escapeAttr(job.company_overview || '')}">${renderMarkdownField(job.company_overview, t("detail.overviewEmpty"))}</div>
          </div>

          ${renderJobDescription(job)}

          <div class="detail-section">
            <div class="section-title">
              <span class="inline-icon-text">${icon("fileText", 13)} ${escapeHtml(t("detail.files", { count: generalAttachments.length }))}</span>
              <label class="btn-secondary inline-icon-text" style="padding: 2px 8px; font-size: 11px; cursor: pointer;">
                ${icon("plus", 11)} ${escapeHtml(t("detail.attach"))}
                <input type="file" id="file-upload-general-input" style="display: none;" />
              </label>
            </div>
            <div class="attachments-grid">
              ${renderGeneralAttachments(generalAttachments)}
            </div>
            ${renderSelectedCvVersion(job)}
          </div>
        </section>
  `;
}

function renderTechnologyChips(technologies) {
  if (!technologies.length) return `<span class="technology-empty">${escapeHtml(t("detail.noTechnologies"))}</span>`;
  return technologies.map((technology) => `<button type="button" class="technology-assigned-chip" data-remove-technology="${escapeAttr(technology.id)}" aria-label="${escapeAttr(t("detail.removeTechnology", { name: technology.name }))}">${escapeHtml(technology.name)} <span aria-hidden="true">×</span></button>`).join("");
}

/** Renders personal preparation notes within the general process section. */
function renderMyNotesSection(job) {
  return `
        <section class="process-subsection my-notes-group">
          <div class="process-subsection-header"><span class="group-title inline-icon-text">${icon("fileText", 13)} ${escapeHtml(t("detail.myNotes"))}</span></div>

          <div class="detail-section">
            <div class="section-title"><span>${icon("compass", 13)} ${escapeHtml(t("detail.reasonsToChange"))}</span></div>
            <div id="reasons-to-change-display" class="note-value editable-reasons-to-change" data-raw-value="${escapeAttr(job.reasons_to_change || '')}">${renderOptionalText(job.reasons_to_change, `<span class="empty-note">${escapeHtml(t("detail.reasonsEmpty"))}</span>`)}</div>
          </div>

          <div class="detail-section">
            <div class="section-title"><span>${icon("user", 13)} ${escapeHtml(t("form.relevantExperience"))}</span><button type="button" class="btn-secondary markdown-field-edit" data-markdown-field="experience_notes">${escapeHtml(t("common.edit"))}</button></div>
            <div id="experience-notes-display" class="note-value editable-experience-notes markdown-preview" data-raw-value="${escapeAttr(job.experience_notes || '')}">${renderMarkdownField(job.experience_notes, t("detail.experienceEmpty"))}</div>
          </div>

          <div class="detail-section">
            <div class="section-title"><span>${icon("euro", 13)} ${escapeHtml(t("form.expectedSalary"))}</span></div>
            <div id="expected-salary-display" class="note-value note-value-compact editable-expected-salary" data-raw-value="${escapeAttr(job.expected_salary || '')}">${renderOptionalText(job.expected_salary, `<span class="empty-note">${escapeHtml(t("detail.expectedSalaryEmpty"))}</span>`)}</div>
          </div>

          <div class="detail-section">
            <div class="section-title"><span>${icon("fileText", 13)} ${escapeHtml(t("detail.otherNotes"))}</span><button type="button" class="btn-secondary markdown-field-edit" data-markdown-field="interview_notes">${escapeHtml(t("common.edit"))}</button></div>
            <div id="interview-notes-display" class="note-value editable-interview-notes markdown-preview" data-raw-value="${escapeAttr(job.interview_notes || '')}">${renderMarkdownField(job.interview_notes, t("detail.otherNotesEmpty"))}</div>
          </div>
        </section>
  `;
}

/** Groups job facts and personal notes for the selection process. */
function renderGeneralProcessSection(view) {
  return `
    <div class="left-pane-section-group general-process-group">
      <div class="group-header">
        <span class="group-title inline-icon-text">${icon("building", 13)} ${escapeHtml(t("detail.generalProcess"))}</span>
        <button class="btn-secondary btn-edit-details inline-icon-text" style="padding: 2px 8px; font-size: 11px;">${icon("edit", 11)} ${escapeHtml(t("detail.editDetails"))}</button>
      </div>
      ${renderJobDetailsSection(view)}
      ${renderMyNotesSection(view.job)}
    </div>
  `;
}
/** Renders one cohesive section of the job detail modal. */
function renderActiveStageFocus(view) {
  const { activeStage, activeStageIndex, stageInterviewers, stageAttachments } = view;
  return `
        ${
          activeStage
            ? `
        <div class="left-pane-section-group stage-focus-group">
          <div class="group-header">
            <div class="inline-icon-text" style="min-width: 0; flex: 1; align-items: center; flex-wrap: wrap; gap: 6px;">
              <span class="group-title inline-icon-text">${icon("user", 13)} ${escapeHtml(t("stage.roundDetails"))}</span>
              <span style="color: var(--text-muted); font-size: 11px;">•</span>
              <span class="stage-round-subtitle" title="${escapeAttr(t("stage.stepTitle", { number: activeStageIndex + 1, title: stageName(activeStage) }))}">${escapeHtml(t("stage.stepTitle", { number: activeStageIndex + 1, title: stageName(activeStage) }))}</span>
            </div>
          </div>

          <div class="detail-section">
            <div class="section-title"><span class="inline-icon-text">${icon("fileText", 13)} ${escapeHtml(t("stage.conversationNotes"))}</span></div>
            <div class="note-value editable-stage-notes" data-raw-value="${escapeAttr(activeStage.notes || "")}">${renderOptionalText(activeStage.notes, `<span class="empty-note">${escapeHtml(t("stage.notesEmpty"))}</span>`)}</div>
          </div>

          <div class="detail-section">
            <div class="section-title"><span class="inline-icon-text">${icon("user", 13)} ${escapeHtml(t("stage.roundContact"))}</span></div>
            ${renderStageRecruiterDetails(view)}
          </div>

          <div class="detail-section">
            <div class="section-title">
              <span class="inline-icon-text">${icon("users", 13)} ${escapeHtml(t("interviewer.heading", { count: stageInterviewers.length }))}</span>
              <button type="button" class="btn-secondary btn-add-stage-interviewer inline-icon-text">${icon("plus", 11)} ${escapeHtml(t("interviewer.add"))}</button>
            </div>
            <div class="interviewers-list">${renderInterviewerRoster(stageInterviewers)}</div>
            <form class="stage-interviewer-form" hidden>
              <label>${escapeHtml(t("interviewer.name"))}<input name="name" type="text" maxlength="120" autocomplete="name" required /></label>
              <label>${escapeHtml(t("interviewer.role"))}<input name="role" type="text" maxlength="120" placeholder="${escapeAttr(t("interviewer.rolePlaceholder"))}" /></label>
              <label class="stage-interviewer-notes">${escapeHtml(t("interviewer.notes"))}<textarea name="notes" rows="2" maxlength="1000" placeholder="${escapeAttr(t("interviewer.notesPlaceholder"))}"></textarea></label>
              <div class="stage-interviewer-form-actions">
                <button class="btn-secondary" type="button" data-cancel-interviewer>${escapeHtml(t("common.cancel"))}</button>
                <button class="btn-primary" type="submit">${escapeHtml(t("interviewer.save"))}</button>
              </div>
            </form>
          </div>

          <div class="detail-section">
            <div class="section-title">
              <span class="inline-icon-text">${icon("fileText", 13)} ${escapeHtml(activeStageIndex === 0 ? t("stage.screeningDocuments", { count: stageAttachments.length }) : t("stage.stepDocuments", { number: activeStageIndex + 1, count: stageAttachments.length }))}</span>
              ${activeStageIndex > 0 ? `<label class="btn-secondary inline-icon-text" style="padding: 2px 8px; font-size: 11px; cursor: pointer;">
                ${icon("plus", 11)} ${escapeHtml(t("detail.attach"))}
                <input type="file" id="file-upload-stage-input" style="display: none;" />
              </label>` : ''}
            </div>
            <div class="attachments-grid">${renderStageAttachmentRows(view)}</div>
          </div>
        </div>
        `
            : ''
        }


  `;
}
/** Renders one cohesive section of the job detail modal. */
function renderStageTabs(view) {
  const { stages, activeStageIndex } = view;
  return `
        <div class="stage-navigation">
          <div class="stage-tabs-scroll">
            <div class="stages-tab-bar" role="tablist" aria-label="${escapeAttr(t("stage.tabsLabel"))}">
              ${stages
                .map(
                  (s, idx) => `
            <button type="button" id="stage-tab-${idx}" class="stage-tab-item ${idx === activeStageIndex ? 'active' : ''}" role="tab" aria-selected="${idx === activeStageIndex}" aria-controls="stage-panel" tabindex="${idx === activeStageIndex ? '0' : '-1'}" data-idx="${idx}">
              <span class="stage-tab-index">${idx + 1}</span>
              <span>${escapeHtml(stageName(s))}</span>
              ${s.status === 'current' ? `<span class="stage-current-chip">${escapeHtml(t("stage.current"))}</span>` : ''}
              ${s.status === 'completed' ? '<span class="stage-tab-status-icon">✓</span>' : ''}
              <span class="stage-tab-qcount">${escapeHtml(t("stage.questionCount", { count: (s.questions || []).length }))}</span>
            </button>
          `
                )
                .join('')}
            </div>
          </div>
          ${stages.length ? `<button class="btn-tab-add-stage inline-icon-text" id="btn-add-stage-tab" type="button" aria-label="${escapeAttr(t("stage.addLabel"))}" title="${escapeAttr(t("stage.addLabel"))}">${icon("plus", 11)} ${escapeHtml(t("stage.add"))}</button>` : ""}
        </div>


  `;
}

/** Keeps the active interview stage visible when the tab bar changes size. */
function bindStageTabResizeObserver(modalEl) {
  stageTabResizeObservers.get(modalEl)?.disconnect();
  const tabs = modalEl.querySelector(".stages-tab-bar");
  if (!tabs) return;
  if (typeof ResizeObserver !== "undefined") {
    const observer = new ResizeObserver(() => {
      positionActiveStageTab(modalEl);
    });
    observer.observe(tabs);
    stageTabResizeObservers.set(modalEl, observer);
  }
}

/** Switches the stacked detail panes on smaller screens. */
function bindMobileDetailPaneSwitch(modalEl) {
  const switcher = modalEl.querySelector(".mobile-detail-view-switch");
  const layout = modalEl.querySelector(".modal-split-layout");
  if (!switcher || !layout) return;

  const scrollPositions = modalEl.mobileDetailScrollPositions ||= { overview: 0, stages: 0 };
  layout.addEventListener("scroll", () => {
    const activePane = modalEl.dataset.mobileDetailPane;
    if (activePane in scrollPositions) scrollPositions[activePane] = layout.scrollTop;
  }, { passive: true });

  switcher.addEventListener("click", (event) => {
    const button = event.target.closest("[data-mobile-pane]");
    if (!button) return;
    const pane = button.dataset.mobilePane;
    if (!["overview", "stages"].includes(pane)) return;
    const currentPane = modalEl.dataset.mobileDetailPane;
    if (currentPane in scrollPositions) scrollPositions[currentPane] = layout.scrollTop;
    modalEl.dataset.mobileDetailPane = pane;
    switcher.querySelectorAll("[data-mobile-pane]").forEach((option) => {
      option.setAttribute("aria-pressed", String(option === button));
    });
    layout.scrollTop = scrollPositions[pane];
    if (pane === "stages") positionActiveStageTab(modalEl);
  });
}

/** Renders one cohesive section of the job detail modal. */
function renderStageSchedule(stage) {
  const icons = new Map([["phone", "phone"], ["onsite", "building"], ["video", "video"]]);
  const format = `${icon(icons.get(stage.meeting_type) || "video", 12)} ${escapeHtml(meetingFormatLabel(stage.meeting_type))}`;
  const meetingType = getMeetingTypeClass(stage.meeting_type);
  const formatTag = `<span class="meeting-format-pill pill-${meetingType} inline-icon-text">${format}</span>`;
  if (!stage.meeting_date && !stage.meeting_time) {
    return `<div class="stage-schedule-badge-row"><div class="stage-schedule-metadata">${formatTag}</div></div>`;
  }
  const join = renderStageMeetingLink(stage);
  return `<div class="stage-schedule-badge-row"><div class="stage-schedule-metadata">${formatTag}<span class="stage-meeting-date inline-icon-text">${icon("calendar", 12)} <time datetime="${escapeAttr(stage.meeting_date || "")}">${escapeHtml(formatCalendarDate(stage.meeting_date))}</time> • ${escapeHtml(stage.meeting_time || "")}</span>${join}</div></div>`;
}

/** Renders a stage's meeting link, or a prompt for an on-site meeting still missing its Maps link. */
function renderStageMeetingLink(stage) {
  const meetingUrl = safeUrl(stage.meeting_url);
  if (meetingUrl) return `<a href="${escapeAttr(meetingUrl)}" target="_blank" rel="noopener noreferrer" class="btn-join-meeting-inline inline-icon-text">${renderMeetingLinkContent(stage.meeting_type, t("meeting.joinCall"), 12)}</a>`;
  if (stage.meeting_type !== "onsite") return "";
  return `<button type="button" class="btn-add-maps-link inline-icon-text">${icon("pin", 12)} ${escapeHtml(t("meeting.addMapsLink"))}</button>`;
}

/** Maps a meeting format to an allowed style class suffix. */
function getMeetingTypeClass(type) {
  return ["video", "phone", "onsite"].includes(type) ? type : "video";
}

/** Renders action buttons for the active stage. */
function renderStageActionButtons(view) {
  const { activeStage, activeStageIndex, stages } = view;
  const current = activeStage.status === "current";
  const name = stageName(activeStage);
  const moveDown = activeStageIndex < stages.length - 1 ? `<button class="stage-action-btn" id="btn-move-stage-down" title="${escapeAttr(t("stage.moveDownShort"))}" aria-label="${escapeAttr(t("stage.moveDownLabel", { stage: name }))}">${icon("chevronDown", 12)}</button>` : "";
  const currentLabel = current
    ? `${icon("pin", 12)} <span class="stage-current-label-full">${escapeHtml(t("stage.currentStep"))}</span><span class="stage-current-label-compact">${escapeHtml(t("stage.current"))}</span> ${icon("check", 12)}`
    : `${icon("target", 12)} <span class="stage-current-label-full">${escapeHtml(t("stage.setCurrentStep"))}</span><span class="stage-current-label-compact">${escapeHtml(t("stage.setCurrent"))}</span>`;
  const scheduleLabel = escapeHtml(t(activeStage.meeting_date || activeStage.meeting_time ? "stage.editSchedule" : "stage.addSchedule"));
  const scheduleIcon = activeStage.meeting_date || activeStage.meeting_time ? "edit" : "calendar";
  return `<button class="btn-stage-current ${current ? "is-current" : ""}" id="btn-toggle-current-stage" title="${escapeAttr(t("stage.setCurrentTitle"))}"><span class="inline-icon-text">${currentLabel}</span></button><button type="button" class="stage-action-btn btn-edit-meeting-schedule inline-icon-text" title="${scheduleLabel}">${icon(scheduleIcon, 11)} ${scheduleLabel}</button>${moveDown}<button class="stage-action-btn" id="btn-delete-stage" title="${escapeAttr(t("stage.deleteTitle"))}" aria-label="${escapeAttr(t("stage.deleteLabel", { stage: name }))}" style="color: var(--status-rejected);">${icon("close", 12)}</button>`;
}

/** Renders a single question row in the stage workspace. */
function renderStageQuestion(question) {
  const questionLabel = escapeAttr(question.question);
  return `<div class="question-item" data-qid="${escapeAttr(question.id)}"><div class="question-row-top">
    <div class="question-text editable-question-text" data-qid="${escapeAttr(question.id)}" data-raw-value="${questionLabel}" style="font-weight: 500; font-size: 14px;">${escapeHtml(question.question)}</div>
    <div style="display: flex; align-items: center; gap: 4px;"><button type="button" class="question-delete-btn q-del" data-qid="${escapeAttr(question.id)}" title="${escapeAttr(t("questions.delete"))}" aria-label="${escapeAttr(t("questions.deleteLabel", { question: question.question }))}">${icon("close", 11)}</button><button type="button" class="question-drag-handle q-grip" data-qid="${escapeAttr(question.id)}" title="${escapeAttr(t("questions.dragHint"))}" aria-label="${escapeAttr(t("questions.reorderLabel", { question: question.question }))}">${icon("gripLines", 14)}</button></div>
  </div><textarea class="question-answer-box q-notes" data-qid="${escapeAttr(question.id)}" aria-label="${escapeAttr(t("questions.notesLabel", { question: question.question }))}" placeholder="${escapeAttr(t("questions.answerPlaceholder"))}">${escapeHtml(question.answer_notes || "")}</textarea></div>`;
}

/** Renders questions or the stage question empty state. */
function renderStageQuestions(questions) {
  if (!questions.length) return `<div style="text-align: center; padding: 40px 20px; color: var(--text-muted); font-size: 13px;">${escapeHtml(t("questions.empty"))}</div>`;
  return questions.map(renderStageQuestion).join("");
}

/** Renders the question editor for an active stage. */
function renderStageQuestionWorkspace(stage) {
  return `<div class="questions-workspace"><div class="questions-workspace-header"><div class="questions-workspace-title inline-icon-text">
    <span>${icon("chat", 14)} ${escapeHtml(t("questions.heading"))}</span><span style="font-size: 12px; color: var(--text-muted); font-weight: 500;">(${stage.questions.length})</span>
  </div></div><form id="active-add-question-form" class="add-question-form" style="margin-top: 0;"><input type="text" class="add-question-input" aria-label="${escapeAttr(t("questions.newLabel"))}" placeholder="${escapeAttr(t("questions.newPlaceholder"))}" required /><button type="submit" class="btn-primary btn-add-question inline-icon-text" style="padding: 7px 16px; font-size: 12px;">${icon("plus", 12)} ${escapeHtml(t("questions.add"))}</button></form>
  <div class="questions-list" style="overflow-y: auto; max-height: calc(100vh - 350px); padding-right: 4px;">${renderStageQuestions(stage.questions)}</div></div>`;
}

/** Renders the active stage detail banner. */
function renderActiveStageHero(view) {
  const { activeStage, activeStageIndex, stages } = view;
  const title = stageName(activeStage);
  const descriptionText = stageDescriptionText(activeStage.description || "");
  const description = renderOptionalText(descriptionText, escapeHtml(t("stage.noDescription")));
  return `<div class="stage-hero-banner"><div class="stage-hero-left"><span class="stage-type-badge ${getStageTypeClass(activeStage.stage_type)}">${escapeHtml(stageTypeLabel(activeStage.stage_type))}</span><div>
    <div class="stage-hero-title editable-stage-title" data-raw-value="${escapeAttr(title)}">${escapeHtml(title)}</div><div class="stage-hero-desc editable-stage-desc" data-raw-value="${escapeAttr(descriptionText)}">${description}</div>
    </div></div><div class="stage-hero-schedule">${renderStageSchedule(activeStage)}</div><div class="stage-hero-actions">${renderStageActionButtons(view)}</div></div>`;
}

/** Renders the active stage editor or the no-stages prompt. */
function renderActiveStageWorkspace(view) {
  if (!view.activeStage) return `<div class="stage-empty-state"><div class="stage-empty-state-copy"><p>${escapeHtml(t("stage.noneConfigured"))}</p><p class="stage-empty-state-guidance">${escapeHtml(t("stage.emptyGuidance"))}</p></div><button class="btn-primary inline-icon-text stage-empty-state-action" id="btn-init-first-stage">${icon("plus", 12)} ${escapeHtml(t("stage.addFirst"))}</button></div>`;
  return `<div id="stage-panel" class="stage-panel" role="tabpanel" aria-labelledby="stage-tab-${view.activeStageIndex}" tabindex="0">${renderActiveStageHero(view)}<div class="stage-round-content">${renderActiveStageFocus(view)}${renderStageQuestionWorkspace(view.activeStage)}</div></div>`;
}

/** Renders an in-context form for recording a confirmed interview stage. */
function renderStageCreateForm() {
  return `<form class="stage-create-form">
    <p class="stage-create-help">${escapeHtml(t("stage.createHelp"))}</p>
    <div class="stage-create-fields">
      <label class="stage-create-field"><span>${escapeHtml(t("stage.titleField"))}</span><input name="custom_title" type="text" maxlength="120" placeholder="${escapeAttr(t("stage.titleFieldPlaceholder"))}" required /></label>
      <label class="stage-create-field"><span>${escapeHtml(t("stage.typeField"))}</span><select name="stage_type" required>
        <option value="" selected disabled>${escapeHtml(t("stage.chooseType"))}</option>
        <option value="HR">${escapeHtml(t("stageType.option.hr"))}</option>
        <option value="Technical">${escapeHtml(t("stageType.option.technical"))}</option>
        <option value="Cultural">${escapeHtml(t("stageType.option.cultural"))}</option>
        <option value="Offer &amp; Decision">${escapeHtml(t("stageType.option.offerDecision"))}</option>
      </select></label>
      <label class="stage-create-field stage-create-description"><span>${escapeHtml(t("stage.descriptionField"))} <small>${escapeHtml(t("common.optional"))}</small></span><input name="description" type="text" maxlength="500" placeholder="${escapeAttr(t("stage.descriptionPlaceholder"))}" /></label>
    </div>
    <div class="stage-create-actions"><p class="stage-create-error" role="alert" hidden></p><button class="btn-secondary stage-create-cancel" type="button">${escapeHtml(t("common.cancel"))}</button><button class="btn-primary stage-create-save" type="submit">${escapeHtml(t("stage.addSubmit"))}</button></div>
  </form>`;
}

/** Reads a scroll position while preserving a zero value for missing panes. */
function readScrollTop(modalEl, selector) {
  return modalEl.querySelector(selector)?.scrollTop || 0;
}

/** Restores a non-zero scroll position after refreshing the modal. */
function restoreScrollTop(modalEl, selector, position) {
  const element = modalEl.querySelector(selector);
  if (element && position) element.scrollTop = position;
}

/** Wires controls that are common to each modal render. */
function attachModalHandlers(context) {
  const { modalEl, backdropEl, job, onGlobalRefresh, activeStage, stages, stageInterviewers } = context;
  bindSplitPaneResize(modalEl);
  const refreshModal = async () => {
    const listScrollTop = readScrollTop(modalEl, ".questions-list");
    const modalScrollTop = readScrollTop(modalEl, ".modal-split-layout");
    const rightScrollTop = readScrollTop(modalEl, ".split-pane-right");
    const leftScrollTop = readScrollTop(modalEl, ".split-pane-left");

    const updated = await api.getJob(job.id);
    renderModalContent(modalEl, backdropEl, updated, onGlobalRefresh);

    restoreScrollTop(modalEl, ".questions-list", listScrollTop);
    restoreScrollTop(modalEl, ".modal-split-layout", modalScrollTop);
    restoreScrollTop(modalEl, ".split-pane-right", rightScrollTop);
    restoreScrollTop(modalEl, ".split-pane-left", leftScrollTop);

    if (onGlobalRefresh) onGlobalRefresh();
  };

  const closeAction = () => {
    detailLoadRequestId += 1;
    try { window.localStorage.removeItem(OPEN_JOB_STORAGE_KEY); } catch { /* Keep closing available when browser storage is blocked. */ }
    splitResizeObservers.get(modalEl)?.disconnect();
    stageTabResizeObservers.get(modalEl)?.disconnect();
    closeWithFlip(modalEl, backdropEl, () => {
      modalEl.innerHTML = "";
      modalEl.removeAttribute("data-mobile-detail-pane");
      restoreModalFocus();
    });
  };

  modalEl.querySelector(".modal-close-btn")?.addEventListener("click", closeAction);
  modalEl.querySelector(".btn-close-modal")?.addEventListener("click", closeAction);

  const handlers = { ...context, refreshModal, closeAction };
  bindBasicModalActions(handlers);
  bindJobInlineEditors(handlers);
  bindTechnologyAssignmentEditor(handlers);
  bindGeneralAttachmentUpload(handlers);
  bindStageRecruiterFields(handlers);
  bindInterviewerActions(handlers);
  bindJobCvSelection(handlers);
  bindTechnologyCatalogManagement(handlers);
  bindAttachmentAndStageNavigation(handlers);
  if (activeStage) {
    bindActiveStageActions(handlers);
    bindActiveStageQuestionControls(handlers);
    bindActiveStageInlineEditors(handlers);
  }
}

function bindTechnologyAssignmentEditor(context) {
  const { modalEl, job, onGlobalRefresh } = context;
  const input = modalEl.querySelector("#technology-assignment-search");
  const options = modalEl.querySelector("#technology-assignment-options");
  const assigned = modalEl.querySelector(".technology-assigned-list");
  const count = modalEl.querySelector(".technology-count");
  if (!input || !options || !assigned) return;
  let catalog = [];
  const selectedIDs = new Set((job.technologies || []).map((item) => item.id));
  const renderOptions = () => {
    const query = input.value.trim().toLowerCase();
    const matches = catalog.filter((item) => !query || [item.name, ...(item.aliases || [])].some((term) => term.toLowerCase().includes(query)));
    options.hidden = false;
    options.innerHTML = matches.length ? matches.map((item) => `<button type="button" class="technology-option ${selectedIDs.has(item.id) ? "is-selected" : ""}" role="option" aria-selected="${selectedIDs.has(item.id)}" data-technology-id="${escapeAttr(item.id)}"><span>${escapeHtml(item.name)}</span><span>${escapeHtml(t(selectedIDs.has(item.id) ? "detail.technologyAdded" : "common.add"))}</span></button>`).join("") : `<div class="technology-empty">${escapeHtml(t("detail.noMatchingTechnologies"))}</div>`;
  };
  const saveAssignments = async (id, shouldAdd) => {
    if (shouldAdd) selectedIDs.add(id); else selectedIDs.delete(id);
    try {
      const updated = await api.updateJob(job.id, { technology_ids: [...selectedIDs] });
      job.technologies = updated.technologies || [];
      assigned.innerHTML = renderTechnologyChips(job.technologies);
      count.textContent = String(job.technologies.length);
      renderOptions();
      if (onGlobalRefresh) await onGlobalRefresh();
    } catch (error) {
      if (shouldAdd) selectedIDs.delete(id); else selectedIDs.add(id);
      showToast(error.message || t("detail.techStackUpdateFailed"), "error");
      renderOptions();
    }
  };
  void Promise.resolve().then(() => api.getTechnologies()).then((items) => { catalog = Array.isArray(items) ? items : []; renderOptions(); options.hidden = true; }).catch((error) => showToast(error.message || t("detail.catalogLoadFailed"), "error"));
  input.addEventListener("focus", renderOptions);
  input.addEventListener("input", renderOptions);
  options.addEventListener("click", (event) => {
    const button = event.target.closest("[data-technology-id]");
    if (button) void saveAssignments(button.dataset.technologyId, !selectedIDs.has(button.dataset.technologyId));
  });
  assigned.addEventListener("click", (event) => {
    const button = event.target.closest("[data-remove-technology]");
    if (button) void saveAssignments(button.dataset.removeTechnology, false);
  });
}

/** Binds the compact per-job CV version selector; library management stays in Data & Backups. */
function bindJobCvSelection({ modalEl, job, refreshModal }) {
  modalEl.querySelector(".job-cv-manage")?.addEventListener("click", () => {
    window.dispatchEvent(new CustomEvent("war-room:open-cv-library", { detail: { jobId: job.id } }));
  });
  modalEl.querySelector(".job-cv-change")?.addEventListener("click", async () => {
    const row = modalEl.querySelector("#job-cv-row");
    const trigger = row?.querySelector(".job-cv-change");
    if (!row || !trigger || trigger.disabled) return;
    trigger.disabled = true;
    try {
      const response = await api.getCvVersions();
      const versions = Array.isArray(response) ? response : response?.versions || [];
      const latestVersion = versions.reduce((latest, version) => (
        !latest || Number(version.version_number) > Number(latest.version_number) ? version : latest
      ), null);
      const currentId = job.selected_cv_version?.id || "";
      row.innerHTML = `<label class="job-cv-picker-label" for="job-cv-version-select">${escapeHtml(t("detail.cvSent"))}</label>
        <select id="job-cv-version-select" aria-label="${escapeAttr(t("detail.cvPickerLabel"))}">
          <option value="">${escapeHtml(t("detail.noCvOption"))}</option>
          ${versions.map((version) => `<option value="${escapeAttr(version.id)}" ${version.id === currentId ? "selected" : ""}>${escapeHtml(t("cv.version", { version: version.version_number }))}${version.id === latestVersion?.id ? ` · ${escapeHtml(t("cv.latest"))}` : ""} · ${escapeHtml(version.original_name || "CV")} · ${escapeHtml(formatCvUploadDate(version.uploaded_at))}</option>`).join("")}
        </select>
        <button type="button" class="btn-primary job-cv-save">${escapeHtml(t("common.save"))}</button>
        <button type="button" class="btn-secondary job-cv-cancel">${escapeHtml(t("common.cancel"))}</button>
        <span class="job-cv-picker-status" role="status" aria-live="polite"></span>`;
      const select = row.querySelector("#job-cv-version-select");
      select.focus();
      row.querySelector(".job-cv-cancel").addEventListener("click", refreshModal);
      row.querySelector(".job-cv-save").addEventListener("click", async (event) => {
        const saveButton = event.currentTarget;
        saveButton.disabled = true;
        const status = row.querySelector(".job-cv-picker-status");
        try {
          await api.setJobCvVersion(job.id, select.value || null);
          await refreshModal();
        } catch (error) {
          status.textContent = error.message || t("detail.cvUpdateFailed");
          saveButton.disabled = false;
        }
      });
    } catch (error) {
      trigger.disabled = false;
      showToast(error.message || t("cv.loadFailed"), "error");
    }
  });
}

/** Opens catalog management from the job's technology section. */
function bindTechnologyCatalogManagement({ modalEl, job }) {
  modalEl.querySelector(".technology-catalog-manage")?.addEventListener("click", () => {
    window.dispatchEvent(new CustomEvent("war-room:open-technology-library", { detail: { jobId: job.id } }));
  });
}

/** Wires the basicmodal controls. */
function bindBasicModalActions(context) {
  const {modalEl, backdropEl, job, onGlobalRefresh, activeStage, stages, stageInterviewers, refreshModal, closeAction} = context;

  const statusButton = modalEl.querySelector(".editable-job-status");
  statusButton?.addEventListener("click", async (event) => {
    event.preventDefault();
    event.stopPropagation();
    statusButton.disabled = true;
    try {
      await api.updateJob(job.id, { status: getNextJobStatus(job.status) });
      await refreshModal();
    } catch (error) {
      statusButton.disabled = false;
      showToast(error.message, "error");
    }
  });

  modalEl.querySelector(".btn-delete-job")?.addEventListener("click", async () => {
    try {
      const confirmed = await confirmAction({
        title: t("process.deleteTitle"),
        message: t("process.deleteMessage", { company: companyText(job.company_name), position: job.position_title }),
        confirmLabel: t("process.deleteConfirm"),
        danger: true,
      });
      if (!confirmed) return;
      await api.deleteJob(job.id);
      closeAction();
      if (onGlobalRefresh) onGlobalRefresh();
    } catch (err) {
      showToast(err.message, "error");
    }
  });

  modalEl.querySelector(".btn-edit-details")?.addEventListener("click", () => {
    openEditDetailsForm(modalEl, backdropEl, job, refreshModal);
  });


}

/** Wires the jobinline controls. */
function bindJobInlineEditors(context) {
  bindPrimaryJobEditors(context);
  bindJobTextEditors(context);
  bindReachoutToggle(context);
}

/** Wires bindPrimaryJobEditors fields. */
function bindPrimaryJobEditors(context) {
  const { modalEl, job, refreshModal } = context;

  const elTitle = modalEl.querySelector(".editable-position-title");
  if (elTitle) {
    makeInlineEditable(elTitle, {
      placeholder: t("form.positionTitle"),
      onSave: async (newVal) => {
        if (!newVal) return;
        await api.updateJob(job.id, { position_title: newVal });
        refreshModal();
      },
    });
  }

  const elCompany = modalEl.querySelector(".editable-company-name");
  if (elCompany) {
    makeInlineEditable(elCompany, {
      placeholder: t("form.companyName"),
      onSave: async (newVal) => {
        await api.updateJob(job.id, { company_name: newVal || "Unknown" });
        refreshModal();
      },
    });
  }

  const elSalary = modalEl.querySelector(".editable-salary");
  if (elSalary) {
    makeInlineEditable(elSalary, {
      eventName: "click",
      placeholder: t("detail.salaryPlaceholder"),
      onSave: async (newVal) => {
        const parsed = parseSalaryInput(newVal);
        await api.updateJob(job.id, parsed);
        refreshModal();
      },
    });
  }

  const elModalReferral = modalEl.querySelector(".editable-modal-referral");
  if (elModalReferral) {
    elModalReferral.addEventListener("click", async (event) => {
      try {
        event.preventDefault();
        event.stopPropagation();
        elModalReferral.disabled = true;
        const nextVal = !job.is_referral;
        await api.updateJob(job.id, { is_referral: nextVal });
        if (typeof window !== "undefined" && typeof window.showToast === "function") {
          window.showToast(t(nextVal ? "referral.marked" : "referral.unmarked"), "success", 1200);
        }
        refreshModal();
      } catch (err) {
        elModalReferral.disabled = false;
        showToast(err.message, "error");
      }
    });
  }

  bindMetadataCycle(modalEl, job, "employment_type", ["permanent", "b2b", "permanent_b2b"], refreshModal);
  bindMetadataCycle(modalEl, job, "work_arrangement", ["remote", "hybrid", "on_site"], refreshModal);
}

/** Cycles a top-level metadata badge to its next value and persists it. */
function bindMetadataCycle(modalEl, job, field, values, refreshModal) {
  const badge = modalEl.querySelector(`[data-cycle-field="${field}"]`);
  if (!badge) return;
  badge.addEventListener("click", async (event) => {
    event.preventDefault();
    event.stopPropagation();
    badge.disabled = true;
    const currentIndex = values.indexOf(job[field]);
    const nextValue = values[(currentIndex + 1) % values.length];
    try {
      await api.updateJob(job.id, { [field]: nextValue });
      refreshModal();
    } catch (error) {
      badge.disabled = false;
      showToast(error.message, "error");
    }
  });
}

/** Wires bindJobTextEditors fields. */
function bindJobTextEditors(context) {
  const { modalEl, job, refreshModal } = context;
  bindProcessDateFields(context);
  const elKeywords = modalEl.querySelector(".editable-keywords");
  if (elKeywords) {
    makeInlineEditable(elKeywords, {
      placeholder: t("detail.keywordsPlaceholder"),
      maxLength: 100,
      onSave: async (newVal) => {
        await api.updateJob(job.id, { keyword_note: newVal });
        refreshModal();
      },
    });
  }

  bindMarkdownEditors(context);

  const elReasons = modalEl.querySelector(".editable-reasons-to-change");
  if (elReasons) {
    makeInlineEditable(elReasons, {
      multiline: true,
      rows: 5,
      placeholder: t("detail.reasonsPlaceholder"),
      onSave: async (newVal) => {
        await api.updateJob(job.id, { reasons_to_change: newVal });
        refreshModal();
      },
    });
  }

  const elExpectedSalary = modalEl.querySelector(".editable-expected-salary");
  if (elExpectedSalary) {
    makeInlineEditable(elExpectedSalary, {
      placeholder: t("form.expectedSalaryPlaceholder"),
      onSave: async (newVal) => {
        await api.updateJob(job.id, { expected_salary: newVal });
        refreshModal();
      },
    });
  }

  bindEditableJobURL(modalEl, job, refreshModal, ".editable-job-post-url", "job_post_url", "https://linkedin.com/jobs/…");
  bindEditableJobURL(modalEl, job, refreshModal, ".editable-application-url", "application_url", t("form.applicationUrlPlaceholder"));
}

function bindEditableJobURL(modalEl, job, refreshModal, selector, field, placeholder) {
  const element = modalEl.querySelector(selector);
  if (!element) return;
  makeInlineEditable(element, {
    placeholder,
    onSave: async (newVal) => {
      await api.updateJob(job.id, { [field]: newVal });
      refreshModal();
    },
  });
}

/** Saves editable application and first-contact dates when their date picker changes. */
function bindProcessDateFields({ modalEl, job, refreshModal }) {
  modalEl.querySelectorAll("[data-process-date]").forEach((input) => {
    input.addEventListener("change", async () => {
      const field = input.dataset.processDate;
      input.disabled = true;
      try {
        await api.updateJob(job.id, { [field]: input.value });
        refreshModal();
      } catch (error) {
        input.value = job[field] || "";
        input.disabled = false;
        showToast(error.message || t("detail.dateSaveFailed"), "error");
      }
    });
  });
}

/** Binds Markdown editing to the overview, notes, and original description fields. */
function bindMarkdownEditors({ modalEl, job, refreshModal }) {
  const fields = [
    { field: "company_overview", selector: ".editable-overview", label: t("markdown.field.companyOverview"), empty: t("detail.overviewEmpty") },
    { field: "experience_notes", selector: ".editable-experience-notes", label: t("markdown.field.experience"), empty: t("detail.experienceEmpty") },
    { field: "interview_notes", selector: ".editable-interview-notes", label: t("markdown.field.otherNotes"), empty: t("detail.otherNotesEmpty") },
    { field: "description", selector: ".editable-description", label: t("markdown.field.description"), empty: t("detail.descriptionEmpty") },
  ];

  fields.forEach(({ field, selector, label, empty }) => {
    const element = modalEl.querySelector(selector);
    if (!element) return;
    const open = () => {
      if (field === "description") element.style.display = "block";
      openMarkdownFieldEditor({ element, job, field, label, empty, refreshModal });
    };
    modalEl.querySelectorAll(`[data-markdown-field="${field}"]`).forEach((button) => button.addEventListener("click", open));
    element.addEventListener("dblclick", open);
  });
}

/** Opens a Markdown editor and saves only after an explicit confirmation. */
function openMarkdownFieldEditor({ element, job, field, label, empty, refreshModal }) {
  if (element.dataset.editing === "true") return;
  element.dataset.editing = "true";
  const originalValue = element.dataset.rawValue || "";
  element.innerHTML = `
    <div class="markdown-field-editor">
      <div class="markdown-editor-tabs" role="tablist" aria-label="${escapeAttr(t("markdown.editorMode", { field: label }))}">
        <button id="markdown-tab-source" type="button" class="markdown-editor-tab is-active" role="tab" aria-selected="true" aria-controls="markdown-source-panel" tabindex="0">${escapeHtml(t("markdown.source"))}</button>
        <button id="markdown-tab-preview" type="button" class="markdown-editor-tab" role="tab" aria-selected="false" aria-controls="markdown-field-preview" tabindex="-1">${escapeHtml(t("markdown.preview"))}</button>
      </div>
      <div id="markdown-source-panel" class="markdown-source-panel" role="tabpanel" aria-labelledby="markdown-tab-source">
        <textarea id="markdown-field-input" class="markdown-field-input" aria-label="${escapeAttr(t("markdown.inputLabel", { field: label }))}" placeholder="${escapeAttr(empty)}"></textarea>
      </div>
      <div id="markdown-field-preview" class="markdown-field-preview markdown-preview" role="tabpanel" aria-labelledby="markdown-tab-preview" tabindex="0" hidden></div>
      <div class="markdown-editor-footer"><span>${escapeHtml(t("markdown.footer"))}</span><div><button type="button" class="btn-secondary markdown-editor-cancel">${escapeHtml(t("common.cancel"))}</button><button type="button" class="btn-primary markdown-editor-save">${escapeHtml(t("common.save"))}</button></div></div>
    </div>`;
  const editor = element.querySelector(".markdown-field-editor");
  const input = element.querySelector(".markdown-field-input");
  const sourcePanel = element.querySelector("#markdown-source-panel");
  const preview = element.querySelector(".markdown-field-preview");
  const tabs = [...element.querySelectorAll(".markdown-editor-tab")];
  input.value = originalValue;

  let finished = false;
  const cleanup = () => {
    document.removeEventListener("pointerdown", onOutsidePointer, true);
    element.dataset.editing = "false";
  };
  const discard = () => {
    if (finished) return;
    finished = true;
    cleanup();
    element.dataset.rawValue = originalValue;
    element.innerHTML = renderMarkdownField(originalValue, empty);
  };
  const save = async () => {
    if (finished) return;
    const newValue = input.value.trim();
    finished = true;
    cleanup();
    try {
      await api.updateJob(job.id, { [field]: newValue });
      refreshModal();
    } catch (error) {
      finished = false;
      element.dataset.editing = "true";
      document.addEventListener("pointerdown", onOutsidePointer, true);
      showToast(error.message || t("markdown.saveFailed", { field: label.toLocaleLowerCase(getLocale()) }), "error");
    }
  };
  function onOutsidePointer(event) {
    if (!editor.contains(event.target)) discard();
  }
  editor.addEventListener("keydown", (event) => {
    if (event.key !== "Escape") return;
    event.preventDefault();
    event.stopPropagation();
    discard();
  });

  const activateTab = (index) => {
    const isPreview = index === 1;
    sourcePanel.hidden = isPreview;
    preview.hidden = !isPreview;
    if (isPreview) preview.innerHTML = renderMarkdownField(input.value, t("markdown.nothingToPreview"));
    tabs.forEach((item, itemIndex) => {
      item.classList.toggle("is-active", itemIndex === index);
      item.setAttribute("aria-selected", String(itemIndex === index));
      item.tabIndex = itemIndex === index ? 0 : -1;
    });
  };
  tabs.forEach((tab, index) => {
    tab.addEventListener("click", () => activateTab(index));
    tab.addEventListener("keydown", (event) => {
      const targetIndex = event.key === "ArrowRight" ? (index + 1) % tabs.length
        : event.key === "ArrowLeft" ? (index - 1 + tabs.length) % tabs.length
          : event.key === "Home" ? 0 : event.key === "End" ? tabs.length - 1 : -1;
      if (targetIndex < 0) return;
      event.preventDefault();
      activateTab(targetIndex);
      tabs[targetIndex].focus();
    });
  });
  element.querySelector(".markdown-editor-save").addEventListener("click", save);
  element.querySelector(".markdown-editor-cancel").addEventListener("click", discard);
  input.addEventListener("keydown", (event) => {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      save();
    }
  });
  document.addEventListener("pointerdown", onOutsidePointer, true);
  input.focus();
}

/** Wires bindReachoutToggle fields. */
function bindReachoutToggle(context) {
  const { modalEl, job, refreshModal } = context;
  const btnToggleReachout = modalEl.querySelector(".btn-toggle-reachout");
  const reachoutContainer = modalEl.querySelector("#reachout-text-container");
  if (btnToggleReachout && reachoutContainer) {
    btnToggleReachout.addEventListener("click", () => {
      reachoutContainer.style.display = reachoutContainer.style.display === "none" ? "block" : "none";
    });
  }
}



/** Wires the generalattachmentupload controls. */
function bindGeneralAttachmentUpload(context) {
  const {modalEl, backdropEl, job, onGlobalRefresh, activeStage, stages, stageInterviewers, refreshModal, closeAction} = context;
  const fileInputGeneral = modalEl.querySelector("#file-upload-general-input");
  if (fileInputGeneral) {
    fileInputGeneral.addEventListener("change", async () => {
      try {
        if (!fileInputGeneral.files || fileInputGeneral.files.length === 0) return;
        const file = fileInputGeneral.files[0];
        const formData = new FormData();
        formData.append("job_id", job.id);
        formData.append("file", file);

        await api.uploadAttachment(formData);
        refreshModal();
      } catch (err) {
        showToast(err.message, "error");
      }
    });
  }


}

/** Wires the stagerecruiter controls. */
function bindStageRecruiterFields(context) {
  const {modalEl, backdropEl, job, onGlobalRefresh, activeStage, stages, stageInterviewers, refreshModal, closeAction} = context;
  const stageRecruiterSelect = modalEl.querySelector(".stage-recruiter-source-select");
  if (stageRecruiterSelect && activeStage) {
    stageRecruiterSelect.addEventListener("change", async (e) => {
      try {
        await api.updateStage(activeStage.id, { recruiter_type: e.target.value });
        if (activeStageIndex === 0) {
          await api.updateJob(job.id, { recruiter_type: e.target.value });
        }
        refreshModal();
      } catch (err) {
        showToast(err.message, "error");
      }
    });
  }

  const elStageRecruiterName = modalEl.querySelector(".editable-stage-recruiter-name");
  if (elStageRecruiterName && activeStage) {
    makeInlineEditable(elStageRecruiterName, {
      placeholder: t("detail.recruiterNamePlaceholder"),
      onSave: async (newVal) => {
        await api.updateStage(activeStage.id, { recruiter_name: newVal });
        if (activeStageIndex === 0) {
          await api.updateJob(job.id, { recruiter_name: newVal });
        }
        refreshModal();
      },
    });
  }

  const elStageRecruiterAgency = modalEl.querySelector(".editable-stage-recruiter-agency");
  if (elStageRecruiterAgency && activeStage) {
    makeInlineEditable(elStageRecruiterAgency, {
      placeholder: t("detail.agencyPlaceholder"),
      onSave: async (newVal) => {
        await api.updateStage(activeStage.id, { recruiter_agency: newVal });
        if (activeStageIndex === 0) {
          await api.updateJob(job.id, { recruiter_agency: newVal });
        }
        refreshModal();
      },
    });
  }

  const elStageRecruiterContact = modalEl.querySelector(".editable-stage-recruiter-contact");
  if (elStageRecruiterContact && activeStage) {
    makeInlineEditable(elStageRecruiterContact, {
      placeholder: t("detail.contactPlaceholder"),
      onSave: async (newVal) => {
        await api.updateStage(activeStage.id, { recruiter_contact: newVal });
        if (activeStageIndex === 0) {
          await api.updateJob(job.id, { recruiter_contact: newVal });
        }
        refreshModal();
      },
    });
  }


}

/** Wires the interviewer controls. */
function bindInterviewerActions(context) {
  const {modalEl, backdropEl, job, onGlobalRefresh, activeStage, stages, stageInterviewers, refreshModal, closeAction} = context;
  if (activeStage) {
    bindAddStageInterviewerForm({ modalEl, activeStage, stageInterviewers, refreshModal });
    modalEl.querySelectorAll(".editable-stage-interviewer-name").forEach((el) => {
      const idx = parseInt(el.getAttribute("data-idx"), 10);
      makeInlineEditable(el, {
        placeholder: t("interviewer.name"),
        onSave: async (newVal) => {
          if (!newVal || !stageInterviewers[idx]) return;
          const roster = [...stageInterviewers];
          roster[idx] = { ...roster[idx], name: newVal };
          await api.updateStage(activeStage.id, { interviewers: roster });
          refreshModal();
        },
      });
    });

    modalEl.querySelectorAll(".editable-stage-interviewer-role").forEach((el) => {
      const idx = parseInt(el.getAttribute("data-idx"), 10);
      makeInlineEditable(el, {
        placeholder: t("interviewer.role"),
        onSave: async (newVal) => {
          if (!stageInterviewers[idx]) return;
          const roster = [...stageInterviewers];
          roster[idx] = { ...roster[idx], role: newVal };
          await api.updateStage(activeStage.id, { interviewers: roster });
          refreshModal();
        },
      });
    });

    modalEl.querySelectorAll(".editable-stage-interviewer-note").forEach((el) => {
      const idx = parseInt(el.getAttribute("data-idx"), 10);
      makeInlineEditable(el, {
        placeholder: t("interviewer.notesInlinePlaceholder"),
        onSave: async (newVal) => {
          if (!stageInterviewers[idx]) return;
          const roster = [...stageInterviewers];
          roster[idx] = { ...roster[idx], notes: newVal };
          await api.updateStage(activeStage.id, { interviewers: roster });
          refreshModal();
        },
      });
    });

    modalEl.querySelectorAll(".btn-del-stage-interviewer").forEach((btn) => {
      btn.addEventListener("click", async () => {
        try {
          const idx = parseInt(btn.getAttribute("data-idx"), 10);
          const roster = stageInterviewers.filter((_, i) => i !== idx);
          await api.updateStage(activeStage.id, { interviewers: roster });
          refreshModal();
        } catch (err) {
          showToast(err.message, "error");
        }
      });
    });

    const fileInputStage = modalEl.querySelector("#file-upload-stage-input");
    if (fileInputStage) {
      fileInputStage.addEventListener("change", async () => {
        try {
          if (!fileInputStage.files || fileInputStage.files.length === 0) return;
          const file = fileInputStage.files[0];
          const formData = new FormData();
          formData.append("job_id", job.id);
          formData.append("stage_id", activeStage.id);
          formData.append("file", file);

          await api.uploadAttachment(formData);
          refreshModal();
        } catch (err) {
          showToast(err.message, "error");
        }
      });
    }
  }
}

/** Adds an interviewer through the in-app form rather than browser prompts. */
function bindAddStageInterviewerForm({ modalEl, activeStage, stageInterviewers, refreshModal }) {
  const trigger = modalEl.querySelector(".btn-add-stage-interviewer");
  const form = modalEl.querySelector(".stage-interviewer-form");
  if (!trigger || !form) return;
  const nameInput = form.elements.namedItem("name");
  const submitButton = form.querySelector('[type="submit"]');

  trigger.addEventListener("click", () => {
    form.hidden = false;
    trigger.hidden = true;
    nameInput.focus();
  });
  form.querySelector("[data-cancel-interviewer]")?.addEventListener("click", () => {
    form.reset();
    form.hidden = true;
    trigger.hidden = false;
    trigger.focus();
  });
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const interviewer = {
      name: nameInput.value.trim(),
      role: form.elements.namedItem("role").value.trim(),
      notes: form.elements.namedItem("notes").value.trim(),
    };
    if (!interviewer.name) return;
    submitButton.disabled = true;
    try {
      await api.updateStage(activeStage.id, { interviewers: [...stageInterviewers, interviewer] });
      refreshModal();
    } catch (error) {
      submitButton.disabled = false;
      showToast(error.message, "error");
    }
  });
}

/** Wires the attachmentandstage controls. */
function bindAttachmentAndStageNavigation(context) {
  const {modalEl, backdropEl, job, onGlobalRefresh, activeStage, stages, stageInterviewers, refreshModal, closeAction} = context;
  modalEl.querySelectorAll(".btn-del-attachment").forEach((btn) => {
    btn.addEventListener("click", async () => {
      try {
        const attId = btn.getAttribute("data-id");
        const confirmed = await confirmAction({
          title: t("detail.deleteAttachmentTitle"),
          message: t("detail.deleteAttachmentMessage"),
          confirmLabel: t("detail.deleteAttachmentConfirm"),
          danger: true,
        });
        if (!confirmed) return;
        await api.deleteAttachment(attId);
        refreshModal();
      } catch (err) {
        showToast(err.message, "error");
      }
    });
  });

  modalEl.querySelectorAll(".stage-tab-item").forEach((tab) => {
    tab.addEventListener("click", (event) => {
      activeStageIndex = Number(tab.dataset.idx);
      renderModalContent(modalEl, backdropEl, job, onGlobalRefresh);
      if (event.detail === 0) {
        modalEl.querySelector(`#stage-tab-${activeStageIndex}`)?.focus({ preventScroll: true });
      }
    });
    tab.addEventListener("keydown", (event) => {
      const currentIndex = Number(tab.dataset.idx);
      const targetIndex = event.key === "ArrowRight" ? Math.min(currentIndex + 1, stages.length - 1)
        : event.key === "ArrowLeft" ? Math.max(currentIndex - 1, 0)
          : event.key === "Home" ? 0 : event.key === "End" ? stages.length - 1 : -1;
      if (targetIndex < 0 || targetIndex === currentIndex) return;
      event.preventDefault();
      modalEl.querySelector(`#stage-tab-${targetIndex}`)?.click();
    });
  });

  bindStageCreation({ modalEl, job, stages, refreshModal });
}

/** Adds a confirmed interview stage through the detail view. */
function bindStageCreation({ modalEl, job, stages, refreshModal }) {
  const trigger = modalEl.querySelector("#btn-add-stage-tab, #btn-init-first-stage");
  if (!trigger) return;
  trigger.addEventListener("click", () => {
    if (modalEl.querySelector(".stage-create-form")) return;
    const form = document.createElement("div");
    form.innerHTML = renderStageCreateForm();
    const stageForm = form.firstElementChild;
    const insertionPoint = trigger.id === "btn-add-stage-tab" ? trigger.closest(".stage-navigation") : trigger;
    insertionPoint.after(stageForm);
    trigger.hidden = true;
    const titleInput = stageForm.querySelector('[name="custom_title"]');
    const cancel = () => {
      stageForm.remove();
      trigger.hidden = false;
      trigger.focus();
    };
    stageForm.querySelector(".stage-create-cancel").addEventListener("click", cancel);
    stageForm.addEventListener("keydown", (event) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      event.stopPropagation();
      cancel();
    });
    stageForm.addEventListener("submit", async (event) => {
      event.preventDefault();
      const submit = stageForm.querySelector(".stage-create-save");
      const error = stageForm.querySelector(".stage-create-error");
      const data = new FormData(stageForm);
      submit.disabled = true;
      error.hidden = true;
      try {
        await api.createStage({
          job_id: job.id,
          stage_type: data.get("stage_type"),
          custom_title: data.get("custom_title").trim(),
          description: data.get("description").trim(),
          status: "pending",
        });
        activeStageIndex = stages.length;
        await refreshModal();
        window.requestAnimationFrame(() => modalEl.querySelector(`#stage-tab-${activeStageIndex}`)?.focus());
      } catch (err) {
        error.textContent = err.message || t("stage.addFailed");
        error.hidden = false;
        submit.disabled = false;
      }
    });
    titleInput.focus();
  });
}

/** Wires the activestage controls. */
function bindActiveStageActions(context) {
  const {modalEl, backdropEl, job, onGlobalRefresh, activeStage, stages, stageInterviewers, refreshModal, closeAction} = context;

    modalEl.querySelector("#btn-toggle-current-stage")?.addEventListener("click", async () => {
      try {
        await api.setCurrentStage(activeStage.id);
        refreshModal();
      } catch (err) {
        showToast(err.message, "error");
      }
    });

    modalEl.querySelector(".btn-edit-meeting-schedule")?.addEventListener("click", () => {
      openScheduleStageForm(modalEl, backdropEl, job, activeStage, refreshModal);
    });

    modalEl.querySelector(".btn-add-maps-link")?.addEventListener("click", () => {
      openScheduleStageForm(modalEl, backdropEl, job, activeStage, refreshModal);
      modalEl.querySelector("#schedule-meeting-url").focus();
    });

    modalEl.querySelector("#btn-move-stage-down")?.addEventListener("click", async () => {
      try {
        const ids = stages.map((s) => s.id);
        const temp = ids[activeStageIndex];
        ids[activeStageIndex] = ids[activeStageIndex + 1];
        ids[activeStageIndex + 1] = temp;
        activeStageIndex += 1;
        await api.reorderStages(job.id, ids);
        refreshModal();
      } catch (err) {
        showToast(err.message, "error");
      }
    });

    modalEl.querySelector("#btn-delete-stage")?.addEventListener("click", async () => {
      try {
        const confirmed = await confirmAction({
          title: t("stage.deleteConfirmTitle"),
          message: t("stage.deleteMessage", { stage: stageName(activeStage) }),
          confirmLabel: t("stage.deleteConfirm"),
          danger: true,
        });
        if (!confirmed) return;
        await api.deleteStage(activeStage.id);
        activeStageIndex = Math.max(0, activeStageIndex - 1);
        refreshModal();
      } catch (err) {
        showToast(err.message, "error");
      }
    });


}

/** Wires the activestagequestion controls. */
function bindActiveStageQuestionControls(context) {
  const {modalEl, backdropEl, job, onGlobalRefresh, activeStage, stages, stageInterviewers, refreshModal, closeAction} = context;
    const questionForm = modalEl.querySelector("#active-add-question-form");
    questionForm?.addEventListener("submit", async (e) => {
      try {
        e.preventDefault();
        const input = questionForm.querySelector(".add-question-input");
        const val = input.value.trim();
        if (!val) return;

        await api.createQuestion({
          stage_id: activeStage.id,
          question: val,
          answer_notes: "",
        });
        input.value = "";
        refreshModal();
      } catch (err) {
        showToast(err.message, "error");
      }
    });

    modalEl.querySelectorAll(".q-del").forEach((btn) => {
      btn.addEventListener("click", async () => {
        try {
          const qid = btn.getAttribute("data-qid");
          await api.deleteQuestion(qid);
          refreshModal();
        } catch (err) {
          showToast(err.message, "error");
        }
      });
    });

    modalEl.querySelectorAll(".q-notes").forEach((textarea) => {
      textarea.addEventListener("change", async () => {
        try {
          const qid = textarea.getAttribute("data-qid");
          await api.updateQuestion(qid, { answer_notes: textarea.value });
        } catch (err) {
          showToast(err.message, "error");
        }
      });
    });


}

/** Wires the activestageinline controls. */
function bindActiveStageInlineEditors(context) {
  const {modalEl, backdropEl, job, onGlobalRefresh, activeStage, stages, stageInterviewers, refreshModal, closeAction} = context;
    const elStageNotes = modalEl.querySelector(".editable-stage-notes");
    if (elStageNotes) {
      makeInlineEditable(elStageNotes, {
        multiline: true,
        rows: 4,
        maxLength: 10000,
        placeholder: t("stage.notesPlaceholder"),
        onSave: async (newVal) => {
          await api.updateStage(activeStage.id, { notes: newVal });
          refreshModal();
        },
      });
    }

    const elStageTitle = modalEl.querySelector(".editable-stage-title");
    if (elStageTitle) {
      makeInlineEditable(elStageTitle, {
        placeholder: t("stage.titleField"),
        onSave: async (newVal) => {
          if (!newVal) return;
          await api.updateStage(activeStage.id, { custom_title: newVal });
          refreshModal();
        },
      });
    }

    const elStageDesc = modalEl.querySelector(".editable-stage-desc");
    if (elStageDesc) {
      makeInlineEditable(elStageDesc, {
        placeholder: t("stage.descriptionInlinePlaceholder"),
        onSave: async (newVal) => {
          await api.updateStage(activeStage.id, { description: newVal });
          refreshModal();
        },
      });
    }

    modalEl.querySelectorAll(".editable-question-text").forEach((elQ) => {
      const qid = elQ.getAttribute("data-qid");
      makeInlineEditable(elQ, {
        placeholder: t("questions.inlinePlaceholder"),
        onSave: async (newVal) => {
          if (!newVal) return;
          await api.updateQuestion(qid, { question: newVal });
          refreshModal();
        },
      });
    });

    const questionsListEl = modalEl.querySelector(".questions-list");
    if (questionsListEl) {
      enableQuestionReordering(questionsListEl, activeStage.id, () => {
        refreshModal();
      });
    }
}

/**
 * Internal renderer for split-view modal content.
 */
function positionActiveStageTab(modalEl) {
  const tabs = modalEl.querySelector(".stages-tab-bar");
  const activeTab = tabs?.querySelector(".stage-tab-item.active");
  if (!tabs || !activeTab) return;

  const tabsRect = tabs.getBoundingClientRect();
  const activeRect = activeTab.getBoundingClientRect();
  const maxScroll = Math.max(0, tabs.scrollWidth - tabs.clientWidth);
  const currentScroll = tabs.scrollLeft;
  const visibleLeft = tabsRect.left;
  const visibleRight = tabsRect.right;
  let targetScroll = currentScroll;
  if (activeRect.left < visibleLeft) targetScroll += activeRect.left - visibleLeft;
  if (activeRect.right > visibleRight) targetScroll += activeRect.right - visibleRight;
  tabs.scrollLeft = Math.max(0, Math.min(maxScroll, targetScroll));
}

function renderModalContent(modalEl, backdropEl, job, onGlobalRefresh) {
  const view = createModalView(job);
  if (modalEl.dataset.mobileDetailJobId !== String(job.id)) {
    modalEl.mobileDetailScrollPositions = { overview: 0, stages: 0 };
    modalEl.dataset.mobileDetailJobId = String(job.id);
  }
  const mobilePane = modalEl.dataset.mobileDetailPane || (view.stages.length ? "stages" : "overview");
  modalEl.dataset.mobileDetailPane = mobilePane;
  modalEl.innerHTML = `
${renderModalHeader(view)}
    <div class="mobile-detail-view-switch" role="group" aria-label="${escapeAttr(t("detail.mobileSwitchLabel"))}">
      <button class="mobile-detail-view-button" type="button" data-mobile-pane="overview" aria-pressed="${mobilePane === "overview"}">${escapeHtml(t("detail.mobileJobDetails"))}</button>
      <button class="mobile-detail-view-button" type="button" data-mobile-pane="stages" aria-pressed="${mobilePane === "stages"}">${escapeHtml(t("stage.tabsLabel"))}</button>
    </div>
    <div class="modal-split-layout">
      <div class="split-pane-left">
${renderGeneralProcessSection(view)}
        <div class="left-pane-actions">
          <button class="btn-danger btn-delete-job" style="font-size: 12px; padding: 6px 12px;">${escapeHtml(t("detail.deleteProcess"))}</button>
          <button class="btn-secondary btn-close-modal" style="font-size: 12px; padding: 6px 14px;">${escapeHtml(t("common.done"))}</button>
        </div>
      </div>
      <div class="split-pane-resizer" role="separator" tabindex="0" aria-label="${escapeAttr(t("detail.resizeLabel"))}" aria-orientation="vertical" aria-valuemin="${MIN_SPLIT_RATIO}" aria-valuemax="${MAX_SPLIT_RATIO}" aria-valuenow="${DEFAULT_SPLIT_RATIO}">
        <span class="split-pane-resizer-arrows" aria-hidden="true">${icon("chevronLeft", 12)}${icon("chevronRight", 12)}</span>
      </div>
      <div class="split-pane-right">
${renderStageTabs(view)}
${renderActiveStageWorkspace(view)}
      </div>
    </div>
  `;

  positionActiveStageTab(modalEl);
  bindStageTabResizeObserver(modalEl);
  bindMobileDetailPaneSwitch(modalEl);

  attachModalHandlers({ modalEl, backdropEl, job, onGlobalRefresh, ...view });
}

/**
 * Opens inline edit modal for company, title, salary, recruiter, and URLs.
 */
/** Renders the editable general job form. */
function renderEditDetailsMarkup(job, periods = []) {
  const searchPeriodOptions = [
    `<option value="">${escapeHtml(t("searchPeriods.none"))}</option>`,
    ...periods.map((period) => `<option value="${escapeAttr(period.id)}" ${period.id === job.search_period_id ? "selected" : ""}>${escapeHtml(period.name)} · ${escapeHtml(period.start_date)} – ${escapeHtml(period.end_date || t("searchPeriods.present"))}</option>`),
  ].join("");
  return `
    <div class="modal-header">
      <h2 class="modal-title">${escapeHtml(t("editProcess.heading"))}</h2>
      <button class="modal-close-btn" type="button" aria-label="${escapeAttr(t("common.closeDialog"))}">&times;</button>
    </div>
    <form class="edit-process-form process-form">
      <h3 class="edit-section-heading">${escapeHtml(t("editProcess.jobDetails"))}</h3>
      <div class="form-grid form-grid-two">
        <div>
          <label class="meta-label" for="edit-company-name">${escapeHtml(t("form.companyName"))}</label>
          <input id="edit-company-name" type="text" name="company_name" value="${escapeAttr(editableCompanyName(job.company_name))}" placeholder="${escapeAttr(t("company.unknown"))}" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
          <span style="font-size: 11px; color: var(--text-muted);">${escapeHtml(t("editProcess.companyHelp"))}</span>
        </div>
        <div>
          <label class="meta-label" for="edit-position-title">${escapeHtml(t("form.positionTitle"))}</label>
          <input id="edit-position-title" type="text" name="position_title" value="${escapeAttr(job.position_title)}" required style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        </div>
      </div>

      <div class="form-grid form-grid-three">
        <div>
          <label class="meta-label" for="edit-salary-type">${escapeHtml(t("editProcess.salaryRangeType"))}</label>
          <select id="edit-salary-type" name="salary_type" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);">
            <option value="limited" ${selectedAttribute(job.salary_type, "limited")}>${escapeHtml(t("salaryType.limited"))}</option>
            <option value="no_min" ${selectedAttribute(job.salary_type, "no_min")}>${escapeHtml(t("salaryType.noMin"))}</option>
            <option value="no_max" ${selectedAttribute(job.salary_type, "no_max")}>${escapeHtml(t("salaryType.noMax"))}</option>
            <option value="unknown" ${selectedAttribute(job.salary_type, "unknown")}>${escapeHtml(t("salaryType.unknown"))}</option>
          </select>
        </div>
        <div>
          <label class="meta-label" for="edit-salary-min">${escapeHtml(t("editProcess.minimumSalaryEur"))}</label>
          <input id="edit-salary-min" type="number" name="salary_min" value="${formValue(job, "salary_min")}" placeholder="${escapeAttr(t("editProcess.minimumSalaryPlaceholder"))}" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        </div>
        <div>
          <label class="meta-label" for="edit-salary-max">${escapeHtml(t("editProcess.maximumSalaryEur"))}</label>
          <input id="edit-salary-max" type="number" name="salary_max" value="${formValue(job, "salary_max")}" placeholder="${escapeAttr(t("editProcess.maximumSalaryPlaceholder"))}" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        </div>
      </div>

      <div class="form-grid form-grid-three">
        <div>
          <label class="meta-label" for="edit-recruiter-type">${escapeHtml(t("editProcess.recruiterRelationship"))}</label>
          <select id="edit-recruiter-type" name="recruiter_type" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);">
            <option value="none" ${selectedAttribute(job.recruiter_type, "none")}>${escapeHtml(t("editProcess.recruiter.none"))}</option>
            <option value="internal" ${selectedAttribute(job.recruiter_type, "internal")}>${escapeHtml(t("editProcess.recruiter.internal"))}</option>
            <option value="external" ${selectedAttribute(job.recruiter_type, "external")}>${escapeHtml(t("editProcess.recruiter.external"))}</option>
          </select>
        </div>
        <div>
          <label class="meta-label" for="edit-recruiter-name">${escapeHtml(t("form.recruiterNameOptional"))}</label>
          <input id="edit-recruiter-name" type="text" name="recruiter_name" value="${formValue(job, "recruiter_name")}" placeholder="${escapeAttr(t("detail.recruiterNamePlaceholder"))}" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        </div>
        <div>
          <label class="meta-label" for="edit-recruiter-agency">${escapeHtml(t("editProcess.agencyName"))}</label>
          <input id="edit-recruiter-agency" type="text" name="recruiter_agency" value="${formValue(job, "recruiter_agency")}" placeholder="${escapeAttr(t("detail.agencyPlaceholder"))}" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        </div>
      </div>

      <div class="form-grid form-grid-two">
        <div>
          <label class="meta-label" for="edit-job-post-url">${escapeHtml(t("editProcess.linkedInUrl"))}</label>
          <input id="edit-job-post-url" type="url" name="job_post_url" value="${formValue(job, "job_post_url")}" placeholder="https://linkedin.com/jobs/…" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        </div>
        <div>
          <label class="meta-label" for="edit-application-url">${escapeHtml(t("form.applicationPageOptional"))}</label>
          <input id="edit-application-url" type="url" name="application_url" value="${formValue(job, "application_url")}" placeholder="${escapeAttr(t("form.applicationUrlPlaceholder"))}" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        </div>
      </div>
      <div>
        <label class="meta-label" for="edit-company-domain">${escapeHtml(t("form.companyDomain"))}</label>
        <input id="edit-company-domain" type="text" name="company_domain" value="${formValue(job, "company_domain")}" placeholder="example.com" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
      </div>

      ${periods.length ? `<div>
        <label class="meta-label" for="edit-search-period">${escapeHtml(t("form.searchPeriod"))}</label>
        <select id="edit-search-period" name="search_period_id">${searchPeriodOptions}</select>
      </div>` : ""}

      <div style="display: flex; align-items: center; gap: 8px; padding: 4px 0;">
        <input type="checkbox" name="is_referral" id="edit-is-referral" ${checkedAttribute(job.is_referral)} style="accent-color: #c084fc; width: 16px; height: 16px; cursor: pointer;" />
        <label for="edit-is-referral" style="font-size: 13px; color: var(--text-primary); cursor: pointer; display: flex; align-items: center; gap: 6px;">
          ${icon("userCheck", 13)} ${escapeHtml(t("editProcess.referral"))}
        </label>
      </div>

      <div>
        <label class="meta-label" for="edit-role-highlights">${escapeHtml(t("editProcess.roleHighlights"))}</label>
        <input id="edit-role-highlights" type="text" name="keyword_note" maxlength="100" value="${formValue(job, "keyword_note")}" placeholder="${escapeAttr(t("editProcess.roleHighlightsPlaceholder"))}" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary); font-family: var(--font-mono);" />
      </div>

      <div>
        <label class="meta-label inline-icon-text" for="edit-company-overview">${icon("building", 13)} ${escapeHtml(t("form.companyOverview"))}</label>
        <textarea id="edit-company-overview" class="role-description-textarea" name="company_overview" rows="4" aria-describedby="edit-company-overview-help" placeholder="${escapeAttr(t("form.companyOverviewPlaceholder"))}" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary); resize: vertical; line-height: 1.4;">${formValue(job, "company_overview")}</textarea>
        <span id="edit-company-overview-help" class="process-form-helper">${escapeHtml(t("editProcess.markdownSupported"))}</span>
      </div>

      <h3 class="edit-section-heading">${escapeHtml(t("editProcess.myNotes"))}</h3>
      <div>
        <label class="meta-label inline-icon-text" for="edit-reasons-to-change">${icon("compass", 13)} ${escapeHtml(t("editProcess.reasonsToChange"))}</label>
        <textarea id="edit-reasons-to-change" name="reasons_to_change" rows="4" placeholder="${escapeAttr(t("form.reasonsToChangePlaceholder"))}" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary); resize: vertical; line-height: 1.4;">${formValue(job, "reasons_to_change")}</textarea>
      </div>

      <div>
        <label class="meta-label inline-icon-text" for="edit-experience-notes">${icon("user", 13)} ${escapeHtml(t("form.relevantExperience"))}</label>
        <textarea id="edit-experience-notes" name="experience_notes" rows="4" placeholder="${escapeAttr(t("form.relevantExperiencePlaceholder"))}" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary); resize: vertical; line-height: 1.4;">${formValue(job, "experience_notes")}</textarea>
      </div>

      <div>
        <label class="meta-label inline-icon-text" for="edit-expected-salary">${icon("euro", 13)} ${escapeHtml(t("form.expectedSalary"))}</label>
        <input id="edit-expected-salary" type="text" name="expected_salary" value="${formValue(job, "expected_salary")}" placeholder="${escapeAttr(t("form.expectedSalaryPlaceholder"))}" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
      </div>

      <div>
        <label class="meta-label inline-icon-text" for="edit-interview-notes">${icon("fileText", 13)} ${escapeHtml(t("detail.otherNotes"))}</label>
        <textarea id="edit-interview-notes" name="interview_notes" rows="4" placeholder="${escapeAttr(t("form.myNotesPlaceholder"))}" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary); resize: vertical; line-height: 1.4;">${formValue(job, "interview_notes")}</textarea>
      </div>

      <div class="form-actions">
        <button type="button" class="btn-secondary btn-cancel-edit">${escapeHtml(t("common.cancel"))}</button>
        <button type="submit" class="btn-primary">${escapeHtml(t("common.saveChanges"))}</button>
      </div>
    </form>
  `;
}

/** Returns the selected attribute for a matching option value. */
function selectedAttribute(currentValue, expectedValue) {
  return currentValue === expectedValue ? "selected" : "";
}

/** Returns the checked attribute for a truthy checkbox value. */
function checkedAttribute(value) {
  return value ? "checked" : "";
}

/** Escapes an optional job field for use as a form value. */
function formValue(job, field) {
  return escapeAttr(job[field] || "");
}

/** Infers a salary range type from its optional bounds. */
function inferSalaryType(minimum, maximum) {
  if (minimum && maximum) return "limited";
  if (minimum) return "no_max";
  if (maximum) return "no_min";
  return null;
}

/** Synchronizes salary type with the values currently entered in the form. */
function syncSalaryType(minInput, maxInput, salaryTypeSelect) {
  const minimum = minInput?.value ? parseInt(minInput.value, 10) : null;
  const maximum = maxInput?.value ? parseInt(maxInput.value, 10) : null;
  const type = inferSalaryType(minimum, maximum);
  if (type && salaryTypeSelect) salaryTypeSelect.value = type;
}

async function openEditDetailsForm(modalEl, backdropEl, job, onSaved) {
  try {
    const periods = await api.getSearchPeriods();
    modalEl.innerHTML = renderEditDetailsMarkup(job, periods);
  } catch (error) {
    showToast(error.message, "error");
    return;
  }

  const form = modalEl.querySelector(".edit-process-form");
  const cancelBtn = modalEl.querySelector(".btn-cancel-edit");
  const closeBtn = modalEl.querySelector(".modal-close-btn");

  const minInput = form.querySelector('input[name="salary_min"]');
  const maxInput = form.querySelector('input[name="salary_max"]');
  const salaryTypeSelect = form.querySelector('select[name="salary_type"]');

  const updateSalaryTypeFromInputs = () => syncSalaryType(minInput, maxInput, salaryTypeSelect);

  minInput?.addEventListener("input", updateSalaryTypeFromInputs);
  maxInput?.addEventListener("input", updateSalaryTypeFromInputs);

  const cancel = () => onSaved();
  cancelBtn.addEventListener("click", cancel);
  closeBtn.addEventListener("click", cancel);

  form.addEventListener("submit", async (e) => {
    try {
      e.preventDefault();
      const data = new FormData(form);
      const salary = getSalaryUpdateValues(data);

      const payload = {
        company_name: String(data.get("company_name") || "").trim() || "Unknown",
        position_title: data.get("position_title"),
        ...salary,
        recruiter_type: data.get("recruiter_type"),
        recruiter_name: data.get("recruiter_name"),
        recruiter_agency: data.get("recruiter_agency"),
        job_post_url: data.get("job_post_url"),
        application_url: data.get("application_url"),
        company_domain: data.get("company_domain"),
        keyword_note: data.get("keyword_note"),
        company_overview: data.get("company_overview"),
        reasons_to_change: data.get("reasons_to_change"),
        experience_notes: data.get("experience_notes"),
        expected_salary: data.get("expected_salary"),
        interview_notes: data.get("interview_notes"),
        is_referral: data.get("is_referral") === "on",
        search_period_id: data.get("search_period_id") || "",
      };

      await api.updateJob(job.id, payload);
      onSaved();
    } catch (err) {
      showToast(err.message, "error");
    }
  });
}

function getSalaryUpdateValues(data) {
  const minValue = data.get("salary_min");
  const maxValue = data.get("salary_max");
  const salaryMin = minValue ? parseInt(minValue, 10) : null;
  const salaryMax = maxValue ? parseInt(maxValue, 10) : null;
  let salaryType = data.get("salary_type");
  if (salaryMin && salaryMax) salaryType = "limited";
  else if (salaryMin) salaryType = "no_max";
  else if (salaryMax) salaryType = "no_min";
  return { salary_type: salaryType, salary_min: salaryMin, salary_max: salaryMax };
}

/**
 * Opens inline modal to schedule or edit an interview meeting with support for
 * video link (Teams, Google Meet, Zoom), phone call, or on-site meeting.
 */
function formatLocalDateInput(date) {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

function openScheduleStageForm(modalEl, backdropEl, job, stage, onSaved) {
  const defaultDate = stage.meeting_date || formatLocalDateInput(new Date());
  const defaultType = stage.meeting_type || "video";
  const hasExistingSchedule = Boolean(stage.meeting_date || stage.meeting_time || stage.meeting_url);
  const title = t(hasExistingSchedule ? "schedule.form.editTitle" : "schedule.form.newTitle", { stage: stageName(stage) });

  modalEl.innerHTML = getScheduleStageFormMarkup(stage, defaultDate, defaultType, title, hasExistingSchedule);

  bindScheduleStageForm(modalEl, stage, onSaved);
}

function getScheduleStageFormMarkup(stage, defaultDate, defaultType, title, hasExistingSchedule) {
  const formatOption = (value, selectedType, label) => `<option value="${value}" ${selectedType === value ? "selected" : ""}>${escapeHtml(label)}</option>`;
  return `
    <div class="modal-header">
      <h2 class="modal-title inline-icon-text">${icon("calendar", 16)} ${escapeHtml(title)}</h2>
      <button class="modal-close-btn" type="button" aria-label="${escapeAttr(t("common.closeDialog"))}">${icon("close", 14)}</button>
    </div>
    <form id="schedule-stage-form" class="process-form">
      <div class="form-grid form-grid-two">
        <div>
          <label class="meta-label" for="schedule-meeting-date">${escapeHtml(t("schedule.form.date"))}</label>
          <input id="schedule-meeting-date" type="date" name="meeting_date" value="${escapeAttr(defaultDate)}" required style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        </div>
        <div>
          <label class="meta-label" for="schedule-meeting-time">${escapeHtml(t("schedule.form.time"))}</label>
          <input id="schedule-meeting-time" type="text" name="meeting_time" value="${escapeAttr(stage.meeting_time || '')}" placeholder="${escapeAttr(t("schedule.form.timePlaceholder"))}" required style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        </div>
      </div>

      <div class="form-grid">
        <div>
          <label class="meta-label" for="sel-meeting-type">${escapeHtml(t("schedule.form.format"))}</label>
          <select name="meeting_type" id="sel-meeting-type" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);">
            ${formatOption("phone", defaultType, t("schedule.form.phone"))}
            ${formatOption("video", defaultType, t("schedule.form.video"))}
            ${formatOption("onsite", defaultType, t("schedule.form.onsite"))}
          </select>
        </div>
      </div>

      <div id="video-link-row">
        <label class="meta-label" id="schedule-meeting-url-label" for="schedule-meeting-url"></label>
        <input id="schedule-meeting-url" type="url" name="meeting_url" value="${escapeAttr(stage.meeting_url || '')}" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        <span id="schedule-meeting-url-hint" style="font-size: 11px; color: var(--text-muted); margin-top: 4px; display: block;"></span>
      </div>

      <div>
        <label class="meta-label" for="schedule-meeting-notes">${escapeHtml(t("stage.conversationNotes"))}</label>
        <textarea id="schedule-meeting-notes" name="notes" rows="3" placeholder="${escapeAttr(t("schedule.form.notesPlaceholder"))}" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary); resize: vertical;">${escapeHtml(stage.notes || '')}</textarea>
      </div>

      <div class="schedule-form-actions${hasExistingSchedule ? "" : " schedule-form-actions-new"}">
        ${hasExistingSchedule ? `<button type="button" class="btn-secondary btn-clear-schedule" style="color: var(--status-rejected);">${escapeHtml(t("schedule.form.clear"))}</button>` : ""}
        <div class="form-actions">
          <button type="button" class="btn-secondary btn-cancel-schedule">${escapeHtml(t("common.cancel"))}</button>
          <button type="submit" class="btn-primary">${escapeHtml(t(hasExistingSchedule ? "common.saveChanges" : "schedule.form.save"))}</button>
        </div>
      </div>
    </form>
  `;
}

function bindScheduleStageForm(modalEl, stage, onSaved) {
  const form = modalEl.querySelector("#schedule-stage-form");
  const cancelBtn = modalEl.querySelector(".btn-cancel-schedule");
  const closeBtn = modalEl.querySelector(".modal-close-btn");
  const clearBtn = modalEl.querySelector(".btn-clear-schedule");

  bindMeetingLinkField(form, stage);

  const cancel = () => onSaved();
  cancelBtn.addEventListener("click", cancel);
  closeBtn.addEventListener("click", cancel);

  clearBtn?.addEventListener("click", () => clearStageSchedule(stage, onSaved));
  form.addEventListener("submit", (event) => saveStageSchedule(event, form, stage, onSaved));
}

/** Shows the link field matching the selected format and resets links from another format. */
function bindMeetingLinkField(form, stage) {
  const select = form.querySelector("#sel-meeting-type");
  const input = form.querySelector("#schedule-meeting-url");
  const initialType = select.value;
  select.addEventListener("change", () => {
    // A Meet link is not a Maps location, so a saved link only applies to its own format.
    input.value = select.value === initialType ? stage.meeting_url || "" : "";
    applyMeetingLinkField(form, select.value);
  });
  applyMeetingLinkField(form, initialType);
}

/** Applies a meeting format's link field copy, hiding the field for formats without a link. */
function applyMeetingLinkField(form, meetingType) {
  const linkField = MEETING_LINK_FIELDS.get(meetingType);
  const input = form.querySelector("#schedule-meeting-url");
  form.querySelector("#video-link-row").hidden = !linkField;
  input.disabled = !linkField;
  if (!linkField) return;
  form.querySelector("#schedule-meeting-url-label").textContent = t(linkField.labelKey);
  form.querySelector("#schedule-meeting-url-hint").textContent = t(linkField.hintKey);
  input.placeholder = linkField.placeholder;
}

async function clearStageSchedule(stage, onSaved) {
  try {
    const confirmed = await confirmAction({
      title: t("schedule.form.clearTitle"),
      message: t("schedule.form.clearMessage"),
      confirmLabel: t("schedule.form.clearConfirm"),
    });
    if (!confirmed) return;
    await api.scheduleMeeting(stage.id, {
      meeting_date: null,
      meeting_time: null,
      meeting_url: null,
      meeting_type: stage.meeting_type || "video",
    });
    onSaved();
  } catch (err) {
    showToast(err.message, "error");
  }
}

async function saveStageSchedule(event, form, stage, onSaved) {
  try {
    event.preventDefault();
    const data = new FormData(form);
    const meetingType = data.get("meeting_type");
    await api.scheduleMeeting(stage.id, {
      meeting_date: data.get("meeting_date"),
      meeting_time: data.get("meeting_time"),
      meeting_type: meetingType,
      meeting_url: MEETING_LINK_FIELDS.has(meetingType) ? data.get("meeting_url") || "" : "",
      notes: data.get("notes"),
    });
    onSaved();
  } catch (err) {
    showToast(err.message, "error");
  }
}
