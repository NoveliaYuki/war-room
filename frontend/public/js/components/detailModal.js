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
import { enableQuestionReordering } from "./questionList.js";
import { makeInlineEditable, parseSalaryInput } from "../inlineEdit.js";
import { escapeHtml, escapeAttr, safeUrl } from "../utils/sanitize.js";
import { formatSalary } from "../utils/salary.js";

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
  const layout = modalEl.querySelector(".modal-split-layout");
  const handle = modalEl.querySelector(".split-pane-resizer");
  if (!layout || !handle) return;

  let ratio = readSplitRatio();
  const applyRatio = (nextRatio, persist = true, limits = getSplitRatioLimits(layout.getBoundingClientRect().width)) => {
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

  applyRatio(ratio, false);
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
  return companyName.toLowerCase() === "unknown"
    ? `<span class="inline-icon-text">${icon("building", 13)} Unknown Company (Undisclosed)</span>`
    : escapeHtml(companyName);
}

/** Returns the raw salary value used by inline editing. */
function getSalaryRawValue(job) {
  if (job.salary_min && job.salary_max) return `${job.salary_min} - ${job.salary_max}`;
  return job.salary_max || job.salary_min || "Salary undisclosed";
}

/** Renders a job status option and marks its current value. */
function renderStatusOption(status, value, label) {
  return `<option value="${value}" ${status === value ? "selected" : ""}>Status: ${label}</option>`;
}

/** Escapes optional note text or returns its empty-state markup. */
function renderOptionalText(value, emptyMarkup) {
  return value ? escapeHtml(value) : emptyMarkup;
}

/** Renders a job link only when it uses a safe web URL. */
function renderJobPostLink(value) {
  if (safeUrl(value)) return `<a href="${escapeAttr(safeUrl(value))}" target="_blank" rel="noopener noreferrer" class="inline-icon-text" style="word-break: break-all; color: var(--accent-blue);">${escapeHtml(value)} ${icon("arrowUpRight", 11)}</a>`;
  return value ? escapeHtml(value) : '<span style="color: var(--text-muted); font-style: italic; font-size: 12px;">No job post link recorded</span>';
}

/** Renders the optional original job description section. */
function renderJobDescription(job) {
  if (!job.description) return "";
  return `<div class="detail-section">
    <div class="section-title">
      <span class="inline-icon-text">${icon("clipboard", 13)} Original Reachout / Description</span>
      <button class="btn-secondary btn-toggle-reachout" style="padding: 2px 8px; font-size: 11px;">Toggle</button>
    </div>
    <div id="reachout-text-container" class="editable-description" data-raw-value="${escapeAttr(job.description)}" style="background: var(--bg-surface-elevated); border: 1px solid var(--border-subtle); border-radius: var(--radius-sm); padding: 10px 14px; font-size: 12px; line-height: 1.45; color: var(--text-secondary); white-space: pre-wrap; max-height: 160px; overflow-y: auto; display: none;">${escapeHtml(job.description)}</div>
  </div>`;
}

/** Renders the general attachment chips. */
function renderGeneralAttachments(attachments) {
  if (!attachments.length) return '<div style="font-size: 12px; color: var(--text-muted); font-style: italic;">No additional files attached (e.g. job spec PDF, company research, notes).</div>';
  return attachments.map((att) => `<div class="attachment-chip">
    <div class="attachment-meta"><span>${icon("fileText", 14)}</span><div>
      <div class="attachment-name" title="${escapeAttr(att.original_name)}">${escapeHtml(att.original_name)}</div>
      <div class="attachment-size">${(att.file_size / 1024).toFixed(1)} KB • General File</div>
    </div></div>
    <div style="display: flex; gap: 6px;">
      <a href="/api/attachments/${encodeURIComponent(att.id)}/download" class="stage-action-btn" title="Download" download>${icon("download", 12)}</a>
      <button class="stage-action-btn btn-del-attachment" data-id="${escapeAttr(att.id)}" title="Remove">${icon("close", 11)}</button>
    </div>
  </div>`).join("");
}

/** Renders recruiter metadata for the active stage. */
function renderStageRecruiterDetails(view) {
  const { stageRecruiterType, stageRecruiterName, stageRecruiterAgency, stageRecruiterContact } = view;
  const contactUrl = safeUrl(stageRecruiterContact);
  const contactValue = !stageRecruiterContact
    ? '<span style="color: var(--text-muted); font-style: italic;">None recorded</span>'
    : contactUrl
      ? `<a href="${escapeAttr(contactUrl)}" target="_blank" rel="noopener noreferrer" class="inline-icon-text">${stageRecruiterContact.toLowerCase().includes("linkedin") ? "LinkedIn Profile" : "Contact Link"} ${icon("externalLink", 11)}</a>`
      : escapeHtml(stageRecruiterContact);
  return `<div class="meta-grid">
    <div class="meta-item"><span class="meta-label">Relationship / Role</span>
      <select class="stage-recruiter-source-select" style="background: var(--bg-surface-elevated); border: 1px solid var(--border-subtle); border-radius: var(--radius-xs); padding: 3px 8px; font-size: 12px; color: var(--text-primary); cursor: pointer; margin-top: 2px;">
        <option value="none" ${stageRecruiterType === "none" ? "selected" : ""}>Direct / Company Member</option>
        <option value="internal" ${stageRecruiterType === "internal" ? "selected" : ""}>Internal Recruiter</option>
        <option value="external" ${stageRecruiterType === "external" ? "selected" : ""}>External Agency / Headhunter</option>
      </select>
    </div>
    <div class="meta-item"><span class="meta-label">Person's Name</span><span class="meta-value editable-stage-recruiter-name" data-raw-value="${escapeAttr(stageRecruiterName)}">${renderOptionalText(stageRecruiterName, '<span style="color: var(--text-muted); font-style: italic;">No contact name</span>')}</span></div>
    <div class="meta-item"><span class="meta-label">Agency / Department</span><span class="meta-value editable-stage-recruiter-agency" data-raw-value="${escapeAttr(stageRecruiterAgency)}">${renderOptionalText(stageRecruiterAgency, '<span style="color: var(--text-muted); font-style: italic;">None</span>')}</span></div>
    <div class="meta-item"><span class="meta-label">Contact / LinkedIn URL</span><span class="meta-value editable-stage-recruiter-contact" data-raw-value="${escapeAttr(stageRecruiterContact)}">${contactValue}</span></div>
  </div>`;
}

/** Renders one interviewer's editable information. */
function renderInterviewerRow(interviewer, index) {
  return `<div class="interviewer-row" data-idx="${index}"><div class="interviewer-info">
    <span class="interviewer-name editable-stage-interviewer-name" data-idx="${index}" data-raw-value="${escapeAttr(interviewer.name)}">${escapeHtml(interviewer.name)}</span>
    <span class="interviewer-role editable-stage-interviewer-role" data-idx="${index}" data-raw-value="${escapeAttr(interviewer.role || "")}">• ${interviewer.role ? escapeHtml(interviewer.role) : "Add role"}</span>
    <span class="interviewer-note editable-stage-interviewer-note" data-idx="${index}" data-raw-value="${escapeAttr(interviewer.notes || "")}">${interviewer.notes ? `(${escapeHtml(interviewer.notes)})` : '<span style="color: var(--text-muted); font-style: italic;">(Add note)</span>'}</span>
  </div><button class="stage-action-btn btn-del-stage-interviewer" data-idx="${index}" style="color: var(--text-muted);" title="Remove interviewer">${icon("close", 11)}</button></div>`;
}

/** Renders the interviewer roster or its empty state. */
function renderInterviewerRoster(interviewers) {
  if (!interviewers.length) return '<div style="font-size: 12px; color: var(--text-muted); font-style: italic;">No interviewers listed for this round yet. Click "+ Add Interviewer" to add.</div>';
  return interviewers.map(renderInterviewerRow).join("");
}

/** Renders attachment rows for an active stage. */
function renderStageAttachmentRows(view) {
  const { stageAttachments, activeStageIndex } = view;
  if (!stageAttachments.length) {
    return activeStageIndex === 0
      ? '<div style="font-size: 12px; color: var(--text-muted); font-style: italic;">No screening documents attached.</div>'
      : '<div style="font-size: 12px; color: var(--text-muted); font-style: italic;">No files attached for this round (e.g. challenge solutions, notes, presentation).</div>';
  }
  return stageAttachments.map((att) => `<div class="attachment-chip"><div class="attachment-meta"><span>${icon("fileText", 14)}</span><div>
    <div class="attachment-name" title="${escapeAttr(att.original_name)}">${escapeHtml(att.original_name)}</div>
    <div class="attachment-size">${(att.file_size / 1024).toFixed(1)} KB • ${activeStageIndex === 0 ? "Screening Document" : `Step ${activeStageIndex + 1}`}</div>
  </div></div><div style="display: flex; gap: 6px;">
    <a href="/api/attachments/${encodeURIComponent(att.id)}/download" class="stage-action-btn" title="Download" download>${icon("download", 12)}</a>
    <button class="stage-action-btn btn-del-attachment" data-id="${escapeAttr(att.id)}" title="Remove">${icon("close", 11)}</button>
  </div></div>`).join("");
}

let activeStageIndex = 0;

/**
 * Renders the modal content and mounts event listeners.
 * Accepts either (arg1: jobId, arg2: onGlobalRefresh)
 * or (arg1: modalEl, arg2: backdropEl, arg3: jobId, arg4: onGlobalRefresh)
 */
export async function openDetailModal(arg1, arg2, arg3, arg4) {
  cancelPendingFlipClose();
  let modalEl;
  let backdropEl;
  let jobId;
  let onGlobalRefresh;

  if (typeof arg1 === "string") {
    jobId = arg1;
    onGlobalRefresh = arg2;
    modalEl = document.querySelector("#detail-modal");
    backdropEl = document.querySelector("#modal-backdrop");
  } else {
    modalEl = arg1 || document.querySelector("#detail-modal");
    backdropEl = arg2 || document.querySelector("#modal-backdrop");
    jobId = arg3;
    onGlobalRefresh = arg4;
  }

  if (backdropEl) {
    backdropEl.classList.add("active");
  }

  try {
    const job = await api.getJob(jobId);
    if (!job) {
      console.error("Job process not found:", jobId);
      return;
    }
    const currentIdx = job.stages ? job.stages.findIndex((s) => s.status === "current") : -1;
    activeStageIndex = currentIdx !== -1 ? currentIdx : 0;
    renderModalContent(modalEl, backdropEl, job, onGlobalRefresh);
  } catch (err) {
    console.error("Failed to load job details:", err);
  }
}

/** Renders one cohesive section of the job detail modal. */
function renderModalHeader(view) {
  const { job, avatarHtml } = view;
  return `

    <div class="modal-header">
      <div class="modal-header-main">
        <div class="modal-avatar">${avatarHtml}</div>
        <div class="modal-title-area">
          <div class="modal-company ${job.company_name.toLowerCase() === 'unknown' ? 'is-unknown' : ''}">
            <span class="editable-company-name" data-raw-value="${escapeAttr(job.company_name)}">${renderCompanyLabel(job.company_name)}</span>
            <span class="status-pill ${escapeAttr(job.status)}">${escapeHtml(job.status)}</span>
          </div>
          <h2 class="modal-title editable-position-title" data-raw-value="${escapeAttr(job.position_title)}">${escapeHtml(job.position_title)}</h2>
          <div class="modal-meta-pills">
            <button type="button" class="salary-tag editable-salary ${job.salary_type === 'unknown' ? 'undisclosed' : ''} inline-icon-text" data-raw-value="${escapeAttr(getSalaryRawValue(job))}" aria-label="Salary: ${escapeAttr(formatSalary(job.salary_type, job.salary_min, job.salary_max, job.salary_currency))}. Click to edit." title="Click to edit salary">
              ${icon("euro", 13)} ${escapeHtml(formatSalary(job.salary_type, job.salary_min, job.salary_max, job.salary_currency))}
            </button>
            <button type="button" class="referral-tag ${job.is_referral ? 'is-referral' : 'not-referral'} editable-modal-referral inline-icon-text" data-id="${escapeAttr(job.id)}" title="Click to toggle referral status" aria-label="${job.is_referral ? 'Referral' : 'No referral'}. Click to toggle.">
              ${icon("userCheck", 12)} ${job.is_referral ? 'Referral' : 'No referral'}
            </button>
            ${renderJobMetadataTag("employment-type-tag", "Employment type", "employment_type", job.employment_type, {
              permanent: "Permanent",
              b2b: "B2B",
              permanent_b2b: "Permanent / B2B",
            })}
            ${renderJobMetadataTag("work-arrangement-tag", "Work arrangement", "work_arrangement", job.work_arrangement, {
              remote: "Remote",
              hybrid: "Hybrid",
              on_site: "On-site",
            })}
          </div>
        </div>
      </div>
      <div class="modal-header-actions">
        <select class="job-status-picker">
          ${renderStatusOption(job.status, "ongoing", "Ongoing")}
          ${renderStatusOption(job.status, "accepted", "Accepted")}
          ${renderStatusOption(job.status, "rejected", "Rejected")}
        </select>
        <button class="modal-close-btn" title="Close (Esc)">${icon("close", 14)}</button>
      </div>
    </div>


  `;
}

/** Renders employment and work arrangement badges in the job detail header. */
function renderJobMetadataTag(className, title, field, value, labels) {
  const label = labels[value] || "Not specified";
  return `<button type="button" class="${className}" data-cycle-field="${field}" title="Click to change ${escapeAttr(title.toLowerCase())}" aria-label="${escapeAttr(title)}: ${escapeAttr(label)}. Click to change.">${escapeHtml(label)}</button>`;
}

/** Renders employer and role information inside the general process section. */
function renderJobDetailsSection(view) {
  const { job, generalAttachments } = view;
  return `
        <section class="process-subsection job-details-group">
          <div class="process-subsection-header"><span class="group-title inline-icon-text">${icon("building", 13)} Job Details</span></div>

          <div class="detail-section">
            <div class="section-title">
              <span>Role Highlights</span>
            </div>
            <div class="keyword-note-box editable-keywords" data-raw-value="${escapeAttr(job.keyword_note || '')}" style="background: var(--bg-surface-elevated); border: 1px solid var(--border-subtle); border-radius: var(--radius-sm); padding: 10px 14px; font-family: var(--font-mono); font-size: 12px; color: #7dd3fc; line-height: 1.4;">
              ${renderOptionalText(job.keyword_note, '<span style="color: var(--text-muted); font-style: italic;">No keywords added</span>')}
            </div>
          </div>

          <div class="detail-section">
            <div class="section-title">
              <span class="inline-icon-text">${icon("building", 13)} Company & Role Overview</span>
            </div>
            <div id="company-overview-display" class="editable-overview" data-raw-value="${escapeAttr(job.company_overview || '')}" style="background: var(--bg-surface-elevated); border: 1px solid var(--border-subtle); border-radius: var(--radius-sm); padding: 12px 14px; font-size: 13px; line-height: 1.5; color: var(--text-primary); white-space: pre-wrap;">${renderOptionalText(job.company_overview, '<span style="color: var(--text-muted); font-style: italic; font-size: 12px;">Summarize what the company does and what the role involves.</span>')}</div>
          </div>

          <div class="detail-section">
            <div class="section-title">
              <span class="inline-icon-text">${icon("externalLink", 12)} Job Post & Spec Link</span>
            </div>
            <div class="meta-item" style="background: var(--bg-surface-elevated); padding: 9px 12px; border: 1px solid var(--border-subtle); border-radius: var(--radius-sm);">
              <span class="meta-value editable-job-post-url" data-raw-value="${escapeAttr(job.job_post_url || '')}">
                ${renderJobPostLink(job.job_post_url)}
              </span>
            </div>
          </div>

          ${renderJobDescription(job)}

          <div class="detail-section">
            <div class="section-title">
              <span class="inline-icon-text">${icon("fileText", 13)} Files (${generalAttachments.length})</span>
              <label class="btn-secondary inline-icon-text" style="padding: 2px 8px; font-size: 11px; cursor: pointer;">
                ${icon("plus", 11)} Attach
                <input type="file" id="file-upload-general-input" style="display: none;" />
              </label>
            </div>
            <div class="attachments-grid">
              ${renderGeneralAttachments(generalAttachments)}
            </div>
          </div>
        </section>
  `;
}

/** Renders personal preparation notes within the general process section. */
function renderMyNotesSection(job) {
  return `
        <section class="process-subsection my-notes-group">
          <div class="process-subsection-header"><span class="group-title inline-icon-text">${icon("fileText", 13)} My Notes</span></div>

          <div class="detail-section">
            <div class="section-title"><span>${icon("compass", 13)} Why I Want to Change Company</span></div>
            <div id="reasons-to-change-display" class="note-value editable-reasons-to-change" data-raw-value="${escapeAttr(job.reasons_to_change || '')}">${renderOptionalText(job.reasons_to_change, '<span class="empty-note">Add your motivations and talking points.</span>')}</div>
          </div>

          <div class="detail-section">
            <div class="section-title"><span>${icon("user", 13)} Relevant Experience</span></div>
            <div id="experience-notes-display" class="note-value editable-experience-notes" data-raw-value="${escapeAttr(job.experience_notes || '')}">${renderOptionalText(job.experience_notes, '<span class="empty-note">Add experience and examples relevant to this role.</span>')}</div>
          </div>

          <div class="detail-section">
            <div class="section-title"><span>${icon("euro", 13)} Expected Salary for This Offer</span></div>
            <div id="expected-salary-display" class="note-value note-value-compact editable-expected-salary" data-raw-value="${escapeAttr(job.expected_salary || '')}">${renderOptionalText(job.expected_salary, '<span class="empty-note">Add your target for this specific opportunity.</span>')}</div>
          </div>

          <div class="detail-section">
            <div class="section-title"><span>${icon("fileText", 13)} My Notes / Anything Else</span></div>
            <div id="interview-notes-display" class="note-value editable-interview-notes" data-raw-value="${escapeAttr(job.interview_notes || '')}">${renderOptionalText(job.interview_notes, '<span class="empty-note">Write anything else you want to remember about this selection process.</span>')}</div>
          </div>
        </section>
  `;
}

/** Groups job facts and personal notes for the selection process. */
function renderGeneralProcessSection(view) {
  return `
    <div class="left-pane-section-group general-process-group">
      <div class="group-header">
        <span class="group-title inline-icon-text">${icon("building", 13)} General Selection Process</span>
        <button class="btn-secondary btn-edit-details inline-icon-text" style="padding: 2px 8px; font-size: 11px;">${icon("edit", 11)} Edit Details</button>
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
              <span class="group-title inline-icon-text">${icon("user", 13)} Round Details</span>
              <span style="color: var(--text-muted); font-size: 11px;">•</span>
              <span class="stage-round-subtitle" title="Step ${activeStageIndex + 1}: ${escapeAttr(activeStage.custom_title || activeStage.stage_type)}">Step ${activeStageIndex + 1}: ${escapeHtml(activeStage.custom_title || activeStage.stage_type)}</span>
            </div>
          </div>

          <div class="detail-section">
            <div class="section-title"><span class="inline-icon-text">${icon("fileText", 13)} Conversation Notes</span></div>
            <div class="note-value editable-stage-notes" data-raw-value="${escapeAttr(activeStage.notes || "")}">${renderOptionalText(activeStage.notes, '<span class="empty-note">Double-click to add notes from this conversation.</span>')}</div>
          </div>

          <div class="detail-section">
            <div class="section-title"><span class="inline-icon-text">${icon("user", 13)} Recruiter / Contact for this Round</span></div>
            ${renderStageRecruiterDetails(view)}
          </div>

          <div class="detail-section">
            <div class="section-title">
              <span class="inline-icon-text">${icon("users", 13)} Interviewers in this Round (${stageInterviewers.length})</span>
              <button class="btn-secondary btn-add-stage-interviewer inline-icon-text" style="padding: 2px 8px; font-size: 11px;">${icon("plus", 11)} Add Interviewer</button>
            </div>
            <div class="interviewers-list">${renderInterviewerRoster(stageInterviewers)}</div>
          </div>

          <div class="detail-section">
            <div class="section-title">
              <span class="inline-icon-text">${icon("fileText", 13)} ${activeStageIndex === 0 ? 'Screening Documents' : `Step ${activeStageIndex + 1} Documents`} (${stageAttachments.length})</span>
              ${activeStageIndex > 0 ? `<label class="btn-secondary inline-icon-text" style="padding: 2px 8px; font-size: 11px; cursor: pointer;">
                ${icon("plus", 11)} Attach
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
        <div class="stages-tab-bar">
          ${stages
            .map(
              (s, idx) => `
            <button class="stage-tab-item ${idx === activeStageIndex ? 'active' : ''}" data-idx="${idx}">
              <span class="stage-tab-index">${idx + 1}</span>
              <span>${escapeHtml(s.custom_title || s.stage_type)}</span>
              ${s.status === 'current' ? '<span class="stage-current-chip">Current</span>' : ''}
              ${s.status === 'completed' ? '<span class="stage-tab-status-icon">✓</span>' : ''}
              <span class="stage-tab-qcount">${(s.questions || []).length} Qs</span>
            </button>
          `
            )
            .join('')}
          <button class="btn-tab-add-stage inline-icon-text" id="btn-add-stage-tab">${icon("plus", 11)} Add Stage</button>
        </div>


  `;
}
/** Renders one cohesive section of the job detail modal. */
function renderStageSchedule(stage) {
  const interviewMode = stage.meeting_type === "onsite" ? "In person" : "Remote";
  const nextMode = stage.meeting_type === "onsite" ? "remote" : "in person";
  const modeTag = `<button type="button" class="interview-mode-tag ${stage.meeting_type === "onsite" ? "is-in-person" : "is-remote"}" aria-label="Interview mode: ${interviewMode}. Switch to ${nextMode}." title="Switch to ${nextMode}">${interviewMode}</button>`;
  if (!stage.meeting_date && !stage.meeting_time) {
    return `<div class="stage-schedule-badge-row">${modeTag}<button class="stage-action-btn btn-edit-meeting-schedule inline-icon-text" style="font-size: 11px; color: var(--accent-blue); padding: 2px 8px;">${icon("plus", 11)} ${icon("calendar", 12)} Schedule Interview Date & Time</button></div>`;
  }
  const formats = new Map([
    ["phone", `${icon("phone", 12)} Phone Call`],
    ["onsite", `${icon("building", 12)} On-Site`],
    ["video", `${icon("video", 12)} Video Call`],
  ]);
  const meetingUrl = safeUrl(stage.meeting_url);
  const join = meetingUrl ? `<a href="${escapeAttr(meetingUrl)}" target="_blank" rel="noopener noreferrer" class="btn-join-meeting-inline inline-icon-text">${icon("video", 12)} Join Call ${icon("arrowUpRight", 11)}</a>` : "";
  const format = formats.get(stage.meeting_type) || formats.get("video");
  const meetingType = getMeetingTypeClass(stage.meeting_type);
  return `<div class="stage-schedule-badge-row">${modeTag}<span class="inline-icon-text">${icon("calendar", 12)} ${escapeHtml(stage.meeting_date || "")} • ${escapeHtml(stage.meeting_time || "")}</span>
    <span class="meeting-format-pill pill-${meetingType} inline-icon-text">${format}</span>${join}
    <button class="stage-action-btn btn-edit-meeting-schedule inline-icon-text" style="font-size: 11px; padding: 2px 8px; color: var(--accent-blue);">${icon("edit", 11)} Edit Schedule</button></div>`;
}

/** Maps a meeting format to an allowed style class suffix. */
function getMeetingTypeClass(type) {
  return ["video", "phone", "onsite"].includes(type) ? type : "video";
}

/** Renders action buttons for the active stage. */
function renderStageActionButtons(view) {
  const { activeStage, activeStageIndex, stages } = view;
  const current = activeStage.status === "current";
  const moveUp = activeStageIndex > 0 ? `<button class="stage-action-btn" id="btn-move-stage-up" title="Move Up">${icon("chevronUp", 12)}</button>` : "";
  const moveDown = activeStageIndex < stages.length - 1 ? `<button class="stage-action-btn" id="btn-move-stage-down" title="Move Down">${icon("chevronDown", 12)}</button>` : "";
  const currentLabel = current ? `${icon("pin", 12)} Current Step ${icon("check", 12)}` : `${icon("target", 12)} Set as Current Step`;
  return `<button class="btn-stage-current ${current ? "is-current" : ""}" id="btn-toggle-current-stage" title="Click to mark this as your current step in the process"><span class="inline-icon-text">${currentLabel}</span></button>${moveUp}${moveDown}<button class="stage-action-btn" id="btn-delete-stage" title="Delete Stage" style="color: var(--status-rejected);">${icon("close", 12)}</button>`;
}

/** Renders a single question row in the stage workspace. */
function renderStageQuestion(question) {
  return `<div class="question-item" data-qid="${escapeAttr(question.id)}"><div class="question-row-top">
    <div class="question-text editable-question-text" data-qid="${escapeAttr(question.id)}" data-raw-value="${escapeAttr(question.question)}" style="font-weight: 500; font-size: 14px;">${escapeHtml(question.question)}</div>
    <div style="display: flex; align-items: center; gap: 4px;"><button class="question-delete-btn q-del" data-qid="${escapeAttr(question.id)}" title="Delete question">${icon("close", 11)}</button><button type="button" class="question-drag-handle q-grip" data-qid="${escapeAttr(question.id)}" title="Hold and drag to reorder questions">${icon("gripLines", 14)}</button></div>
  </div><textarea class="question-answer-box q-notes" data-qid="${escapeAttr(question.id)}" placeholder="Log interviewer's answers or your notes here...">${escapeHtml(question.answer_notes || "")}</textarea></div>`;
}

/** Renders questions or the stage question empty state. */
function renderStageQuestions(questions) {
  if (!questions.length) return '<div style="text-align: center; padding: 40px 20px; color: var(--text-muted); font-size: 13px;">No questions added yet for this interview. Add your questions above!</div>';
  return questions.map(renderStageQuestion).join("");
}

/** Renders the question editor for an active stage. */
function renderStageQuestionWorkspace(stage) {
  return `<div class="questions-workspace"><div class="questions-workspace-header"><div class="questions-workspace-title inline-icon-text">
    <span>${icon("chat", 14)} Questions to Ask this Interviewer</span><span style="font-size: 12px; color: var(--text-muted); font-weight: 500;">(${stage.questions.length})</span>
  </div></div><form id="active-add-question-form" class="add-question-form" style="margin-top: 0;"><input type="text" class="add-question-input" placeholder="Type a question you want to ask in this interview..." required /><button type="submit" class="btn-primary btn-add-question inline-icon-text" style="padding: 7px 16px; font-size: 12px;">${icon("plus", 12)} Add Question</button></form>
  <div class="questions-list" style="overflow-y: auto; max-height: calc(100vh - 350px); padding-right: 4px;">${renderStageQuestions(stage.questions)}</div></div>`;
}

/** Renders the active stage detail banner. */
function renderActiveStageHero(view) {
  const { activeStage, activeStageIndex, stages } = view;
  const title = activeStage.custom_title || activeStage.stage_type;
  const description = renderOptionalText(activeStage.description, "No stage description");
  return `<div class="stage-hero-banner"><div class="stage-hero-left"><span class="stage-type-badge ${getStageTypeClass(activeStage.stage_type)}">${escapeHtml(activeStage.stage_type)}</span><div>
    <div class="stage-hero-title editable-stage-title" data-raw-value="${escapeAttr(title)}">${escapeHtml(title)}</div><div class="stage-hero-desc editable-stage-desc" data-raw-value="${escapeAttr(activeStage.description || "")}">${description}</div>
    ${renderStageSchedule(activeStage)}</div></div><div class="stage-hero-actions">${renderStageActionButtons(view)}</div></div>`;
}

/** Renders the active stage editor or the no-stages prompt. */
function renderActiveStageWorkspace(view) {
  if (!view.activeStage) return `<div class="stage-empty-state"><p>No interview stages configured yet.</p><button class="btn-primary inline-icon-text stage-empty-state-action" id="btn-init-first-stage">${icon("plus", 12)} Add First Stage</button></div>`;
  return `${renderActiveStageHero(view)}${renderActiveStageFocus(view)}${renderStageQuestionWorkspace(view.activeStage)}`;
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
    const rightScrollTop = readScrollTop(modalEl, ".split-pane-right");
    const leftScrollTop = readScrollTop(modalEl, ".split-pane-left");

    const updated = await api.getJob(job.id);
    renderModalContent(modalEl, backdropEl, updated, onGlobalRefresh);

    restoreScrollTop(modalEl, ".questions-list", listScrollTop);
    restoreScrollTop(modalEl, ".split-pane-right", rightScrollTop);
    restoreScrollTop(modalEl, ".split-pane-left", leftScrollTop);

    if (onGlobalRefresh) onGlobalRefresh();
  };

  const closeAction = () => {
    closeWithFlip(modalEl, backdropEl, () => {
      modalEl.innerHTML = "";
    });
  };

  modalEl.querySelector(".modal-close-btn")?.addEventListener("click", closeAction);
  modalEl.querySelector(".btn-close-modal")?.addEventListener("click", closeAction);

  const handlers = { ...context, refreshModal, closeAction };
  bindBasicModalActions(handlers);
  bindJobInlineEditors(handlers);
  bindGeneralAttachmentUpload(handlers);
  bindStageRecruiterFields(handlers);
  bindInterviewerActions(handlers);
  bindAttachmentAndStageNavigation(handlers);
  if (activeStage) {
    bindActiveStageActions(handlers);
    bindActiveStageQuestionControls(handlers);
    bindActiveStageInlineEditors(handlers);
  }
}

/** Wires the basicmodal controls. */
function bindBasicModalActions(context) {
  const {modalEl, backdropEl, job, onGlobalRefresh, activeStage, stages, stageInterviewers, refreshModal, closeAction} = context;
  modalEl.querySelector(".modal-close-btn")?.addEventListener("click", closeAction);
  modalEl.querySelector(".btn-close-modal")?.addEventListener("click", closeAction);

  modalEl.querySelector(".job-status-picker")?.addEventListener("change", async (e) => {
    try {
      await api.updateJob(job.id, { status: e.target.value });
      refreshModal();
    } catch (err) {
      showToast(err.message, "error");
    }
  });

  modalEl.querySelector(".btn-delete-job")?.addEventListener("click", async () => {
    try {
      if (confirm(`Permanently delete selection process for "${job.company_name} - ${job.position_title}"?`)) {
        await api.deleteJob(job.id);
        closeAction();
        if (onGlobalRefresh) onGlobalRefresh();
      }
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
      placeholder: "Position Title",
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
      placeholder: "Company Name",
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
      placeholder: "e.g. 70k - 90k, up to 85k, undisclosed",
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
        const nextVal = job.is_referral ? 0 : 1;
        await api.updateJob(job.id, { is_referral: nextVal });
        if (typeof window !== "undefined" && typeof window.showToast === "function") {
          window.showToast(nextVal ? "Marked as Referral" : "Marked as Non-referral", "success", 1200);
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
  const elKeywords = modalEl.querySelector(".editable-keywords");
  if (elKeywords) {
    makeInlineEditable(elKeywords, {
      placeholder: "Keywords, tech stack, 100-char summary...",
      maxLength: 100,
      onSave: async (newVal) => {
        await api.updateJob(job.id, { keyword_note: newVal });
        refreshModal();
      },
    });
  }

  const elOverview = modalEl.querySelector(".editable-overview");
  if (elOverview) {
    makeInlineEditable(elOverview, {
      multiline: true,
      rows: 5,
      placeholder: "What does this company do? What is remarkable/special about this position for you?",
      onSave: async (newVal) => {
        await api.updateJob(job.id, { company_overview: newVal });
        refreshModal();
      },
    });
  }

  const elReasons = modalEl.querySelector(".editable-reasons-to-change");
  if (elReasons) {
    makeInlineEditable(elReasons, {
      multiline: true,
      rows: 5,
      placeholder: "Why do you want to change company? Write your key talking points and motivations here...",
      onSave: async (newVal) => {
        await api.updateJob(job.id, { reasons_to_change: newVal });
        refreshModal();
      },
    });
  }

  const elExperience = modalEl.querySelector(".editable-experience-notes");
  if (elExperience) {
    makeInlineEditable(elExperience, {
      multiline: true,
      rows: 5,
      placeholder: "Which experience and examples are most relevant to this role?",
      onSave: async (newVal) => {
        await api.updateJob(job.id, { experience_notes: newVal });
        refreshModal();
      },
    });
  }

  const elExpectedSalary = modalEl.querySelector(".editable-expected-salary");
  if (elExpectedSalary) {
    makeInlineEditable(elExpectedSalary, {
      placeholder: "e.g. €85k base plus equity; flexible depending on the offer",
      onSave: async (newVal) => {
        await api.updateJob(job.id, { expected_salary: newVal });
        refreshModal();
      },
    });
  }

  const elNotes = modalEl.querySelector(".editable-interview-notes");
  if (elNotes) {
    makeInlineEditable(elNotes, {
      multiline: true,
      rows: 6,
      placeholder: "Notes from calls (funding round, revenue/ARR, team size, culture, debriefs)...",
      onSave: async (newVal) => {
        await api.updateJob(job.id, { interview_notes: newVal });
        refreshModal();
      },
    });
  }

  const elDesc = modalEl.querySelector(".editable-description");
  if (elDesc) {
    makeInlineEditable(elDesc, {
      multiline: true,
      rows: 6,
      placeholder: "Job description or original message...",
      onSave: async (newVal) => {
        await api.updateJob(job.id, { description: newVal });
        refreshModal();
      },
    });
  }

  const elJobPostUrl = modalEl.querySelector(".editable-job-post-url");
  if (elJobPostUrl) {
    makeInlineEditable(elJobPostUrl, {
      placeholder: "https://... (Job spec or careers link)",
      onSave: async (newVal) => {
        await api.updateJob(job.id, { job_post_url: newVal || null });
        refreshModal();
      },
    });
  }
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
      placeholder: "Recruiter / contact name",
      onSave: async (newVal) => {
        await api.updateStage(activeStage.id, { recruiter_name: newVal || null });
        if (activeStageIndex === 0) {
          await api.updateJob(job.id, { recruiter_name: newVal || null });
        }
        refreshModal();
      },
    });
  }

  const elStageRecruiterAgency = modalEl.querySelector(".editable-stage-recruiter-agency");
  if (elStageRecruiterAgency && activeStage) {
    makeInlineEditable(elStageRecruiterAgency, {
      placeholder: "Agency or department",
      onSave: async (newVal) => {
        await api.updateStage(activeStage.id, { recruiter_agency: newVal || null });
        if (activeStageIndex === 0) {
          await api.updateJob(job.id, { recruiter_agency: newVal || null });
        }
        refreshModal();
      },
    });
  }

  const elStageRecruiterContact = modalEl.querySelector(".editable-stage-recruiter-contact");
  if (elStageRecruiterContact && activeStage) {
    makeInlineEditable(elStageRecruiterContact, {
      placeholder: "Email, phone or LinkedIn URL",
      onSave: async (newVal) => {
        await api.updateStage(activeStage.id, { recruiter_contact: newVal || null });
        if (activeStageIndex === 0) {
          await api.updateJob(job.id, { recruiter_contact: newVal || null });
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
    modalEl.querySelectorAll(".editable-stage-interviewer-name").forEach((el) => {
      const idx = parseInt(el.getAttribute("data-idx"), 10);
      makeInlineEditable(el, {
        placeholder: "Interviewer Name",
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
        placeholder: "Role / Specialty",
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
        placeholder: "Notes about interviewer",
        onSave: async (newVal) => {
          if (!stageInterviewers[idx]) return;
          const roster = [...stageInterviewers];
          roster[idx] = { ...roster[idx], notes: newVal };
          await api.updateStage(activeStage.id, { interviewers: roster });
          refreshModal();
        },
      });
    });

    modalEl.querySelector(".btn-add-stage-interviewer")?.addEventListener("click", async () => {
      try {
        const name = prompt("Interviewer Name:");
        if (!name) return;
        const role = prompt("Role / Specialty (e.g. Engineering Lead, VP of Product):") || "";
        const notes = prompt("Quick notes (optional):") || "";

        const roster = [...stageInterviewers, { name, role, notes }];
        await api.updateStage(activeStage.id, { interviewers: roster });
        refreshModal();
      } catch (err) {
        showToast(err.message, "error");
      }
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

/** Wires the attachmentandstage controls. */
function bindAttachmentAndStageNavigation(context) {
  const {modalEl, backdropEl, job, onGlobalRefresh, activeStage, stages, stageInterviewers, refreshModal, closeAction} = context;
  modalEl.querySelectorAll(".btn-del-attachment").forEach((btn) => {
    btn.addEventListener("click", async () => {
      try {
        const attId = btn.getAttribute("data-id");
        if (confirm("Delete this attachment?")) {
          await api.deleteAttachment(attId);
          refreshModal();
        }
      } catch (err) {
        showToast(err.message, "error");
      }
    });
  });

  modalEl.querySelectorAll(".stage-tab-item").forEach((tab) => {
    tab.addEventListener("click", () => {
      activeStageIndex = parseInt(tab.getAttribute("data-idx"), 10);
      renderModalContent(modalEl, backdropEl, job, onGlobalRefresh);
    });
  });

  const addStageBtn = modalEl.querySelector("#btn-add-stage-tab") || modalEl.querySelector("#btn-init-first-stage");
  if (addStageBtn) {
    addStageBtn.addEventListener("click", async () => {
      try {
        const title = prompt("Stage Title (e.g. Technical Deep Dive, Architecture Review):");
        if (!title) return;
        const type = prompt("Stage Type (HR, Technical, Cultural, Offer & Decision):", "Technical") || "Technical";
        const desc = prompt("Stage description (optional):") || "";

        await api.createStage({
          job_id: job.id,
          stage_type: type,
          custom_title: title,
          description: desc,
          status: "pending",
        });
        activeStageIndex = stages.length; // Jump to new stage
        refreshModal();
      } catch (err) {
        showToast(err.message, "error");
      }
    });
  }


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

    modalEl.querySelector(".interview-mode-tag")?.addEventListener("click", async () => {
      try {
        const meetingType = activeStage.meeting_type === "onsite" ? "video" : "onsite";
        await api.updateStage(activeStage.id, { meeting_type: meetingType });
        await refreshModal();
      } catch (err) {
        showToast(err.message, "error");
      }
    });

    modalEl.querySelector("#btn-move-stage-up")?.addEventListener("click", async () => {
      try {
        const ids = stages.map((s) => s.id);
        const temp = ids[activeStageIndex];
        ids[activeStageIndex] = ids[activeStageIndex - 1];
        ids[activeStageIndex - 1] = temp;
        activeStageIndex -= 1;
        await api.reorderStages(job.id, ids);
        refreshModal();
      } catch (err) {
        showToast(err.message, "error");
      }
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
        if (confirm(`Delete stage "${activeStage.custom_title || activeStage.stage_type}"?`)) {
          await api.deleteStage(activeStage.id);
          activeStageIndex = Math.max(0, activeStageIndex - 1);
          refreshModal();
        }
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
        placeholder: "Add notes from this conversation...",
        onSave: async (newVal) => {
          await api.updateStage(activeStage.id, { notes: newVal });
          refreshModal();
        },
      });
    }

    const elStageTitle = modalEl.querySelector(".editable-stage-title");
    if (elStageTitle) {
      makeInlineEditable(elStageTitle, {
        placeholder: "Stage Title",
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
        placeholder: "Stage description",
        onSave: async (newVal) => {
          await api.updateStage(activeStage.id, { description: newVal });
          refreshModal();
        },
      });
    }

    modalEl.querySelectorAll(".editable-question-text").forEach((elQ) => {
      const qid = elQ.getAttribute("data-qid");
      makeInlineEditable(elQ, {
        placeholder: "Interview Question",
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
function renderModalContent(modalEl, backdropEl, job, onGlobalRefresh) {
  const view = createModalView(job);
  modalEl.innerHTML = `
${renderModalHeader(view)}
    <div class="modal-split-layout">
      <div class="split-pane-left">
${renderGeneralProcessSection(view)}
        <div class="left-pane-actions">
          <button class="btn-danger btn-delete-job" style="font-size: 12px; padding: 6px 12px;">Delete Selection Process</button>
          <button class="btn-secondary btn-close-modal" style="font-size: 12px; padding: 6px 14px;">Done</button>
        </div>
      </div>
      <div class="split-pane-resizer" role="separator" tabindex="0" aria-label="Resize job details and interview workspace" aria-orientation="vertical" aria-valuemin="${MIN_SPLIT_RATIO}" aria-valuemax="${MAX_SPLIT_RATIO}" aria-valuenow="${DEFAULT_SPLIT_RATIO}">
        <span class="split-pane-resizer-arrows" aria-hidden="true">${icon("chevronLeft", 12)}${icon("chevronRight", 12)}</span>
      </div>
      <div class="split-pane-right">
${renderStageTabs(view)}
${renderActiveStageWorkspace(view)}
      </div>
    </div>
  `;

  attachModalHandlers({ modalEl, backdropEl, job, onGlobalRefresh, ...view });
}

/**
 * Opens inline edit modal for company, title, salary, recruiter, and URLs.
 */
/** Renders the editable general job form. */
function renderEditDetailsMarkup(job) {
  return `
    <div class="modal-header">
      <h2 class="modal-title">Edit Selection Process Details</h2>
      <button class="modal-close-btn">&times;</button>
    </div>
    <form class="edit-process-form" style="padding: 24px 28px; display: flex; flex-direction: column; gap: 16px; overflow-y: auto;">
      <h3 class="edit-section-heading">Job details</h3>
      <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 16px;">
        <div>
          <label class="meta-label">Company Name</label>
          <input type="text" name="company_name" value="${escapeAttr(job.company_name)}" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
          <span style="font-size: 11px; color: var(--text-muted);">Leave empty or 'Unknown' if undisclosed by recruiter</span>
        </div>
        <div>
          <label class="meta-label">Position Title</label>
          <input type="text" name="position_title" value="${escapeAttr(job.position_title)}" required style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        </div>
      </div>

      <div style="display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 16px;">
        <div>
          <label class="meta-label">Salary Range Type</label>
          <select name="salary_type" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);">
            <option value="limited" ${selectedAttribute(job.salary_type, "limited")}>Limited Range (Min - Max)</option>
            <option value="no_min" ${selectedAttribute(job.salary_type, "no_min")}>Up to Max (No Minimum)</option>
            <option value="no_max" ${selectedAttribute(job.salary_type, "no_max")}>From Min (No Maximum)</option>
            <option value="unknown" ${selectedAttribute(job.salary_type, "unknown")}>Unknown / Undisclosed</option>
          </select>
        </div>
        <div>
          <label class="meta-label">Minimum Salary (€)</label>
          <input type="number" name="salary_min" value="${formValue(job, "salary_min")}" placeholder="e.g. 70000" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        </div>
        <div>
          <label class="meta-label">Maximum Salary (€)</label>
          <input type="number" name="salary_max" value="${formValue(job, "salary_max")}" placeholder="e.g. 95000" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        </div>
      </div>

      <div style="display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 16px;">
        <div>
          <label class="meta-label">Recruiter Relationship</label>
          <select name="recruiter_type" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);">
            <option value="none" ${selectedAttribute(job.recruiter_type, "none")}>Direct / None</option>
            <option value="internal" ${selectedAttribute(job.recruiter_type, "internal")}>Internal Company Recruiter</option>
            <option value="external" ${selectedAttribute(job.recruiter_type, "external")}>External Headhunter / Agency</option>
          </select>
        </div>
        <div>
          <label class="meta-label">Recruiter Name (Optional)</label>
          <input type="text" name="recruiter_name" value="${formValue(job, "recruiter_name")}" placeholder="Recruiter / contact name" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        </div>
        <div>
          <label class="meta-label">Agency Name (If External)</label>
          <input type="text" name="recruiter_agency" value="${formValue(job, "recruiter_agency")}" placeholder="Agency or department" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        </div>
      </div>

      <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 16px;">
        <div>
          <label class="meta-label">Job Post URL (LinkedIn or Careers Page)</label>
          <input type="url" name="job_post_url" value="${formValue(job, "job_post_url")}" placeholder="https://linkedin.com/jobs/view/..." style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        </div>
        <div>
          <label class="meta-label">Company Domain (For Logo)</label>
          <input type="text" name="company_domain" value="${formValue(job, "company_domain")}" placeholder="example.com" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        </div>
      </div>

      <div style="display: flex; align-items: center; gap: 8px; padding: 4px 0;">
        <input type="checkbox" name="is_referral" id="edit-is-referral" ${checkedAttribute(job.is_referral)} style="accent-color: #c084fc; width: 16px; height: 16px; cursor: pointer;" />
        <label for="edit-is-referral" style="font-size: 13px; color: var(--text-primary); cursor: pointer; display: flex; align-items: center; gap: 6px;">
          ${icon("userCheck", 13)} Candidate Referral (Mark this process as a direct referral)
        </label>
      </div>

      <div>
        <label class="meta-label">Role Highlights (MAX 100 CHARACTERS)</label>
        <input type="text" name="keyword_note" maxlength="100" value="${formValue(job, "keyword_note")}" placeholder="Location • Focus area • Key details" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary); font-family: var(--font-mono);" />
      </div>

      <div>
        <label class="meta-label inline-icon-text">${icon("building", 13)} Company & Role Overview</label>
        <textarea name="company_overview" rows="4" placeholder="Summarize what the company does and what the role involves." style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary); resize: vertical; line-height: 1.4;">${formValue(job, "company_overview")}</textarea>
      </div>

      <h3 class="edit-section-heading">My notes</h3>
      <div>
        <label class="meta-label inline-icon-text">${icon("compass", 13)} Why I Want to Change Company (Top FAQ & Talking Points)</label>
        <textarea name="reasons_to_change" rows="4" placeholder="Why do you want to change company? Key motivations, strategic rationale, growth drivers..." style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary); resize: vertical; line-height: 1.4;">${formValue(job, "reasons_to_change")}</textarea>
      </div>

      <div>
        <label class="meta-label inline-icon-text">${icon("user", 13)} Relevant Experience</label>
        <textarea name="experience_notes" rows="4" placeholder="Experience and examples that match this role..." style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary); resize: vertical; line-height: 1.4;">${formValue(job, "experience_notes")}</textarea>
      </div>

      <div>
        <label class="meta-label inline-icon-text">${icon("euro", 13)} Expected Salary for This Offer</label>
        <input type="text" name="expected_salary" value="${formValue(job, "expected_salary")}" placeholder="e.g. €85k base plus equity; flexible depending on the offer" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
      </div>

      <div>
        <label class="meta-label inline-icon-text">${icon("fileText", 13)} My Notes / Anything Else</label>
        <textarea name="interview_notes" rows="4" placeholder="Write anything else you want to remember about this selection process..." style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary); resize: vertical; line-height: 1.4;">${formValue(job, "interview_notes")}</textarea>
      </div>

      <div style="display: flex; justify-content: flex-end; gap: 10px; margin-top: 12px;">
        <button type="button" class="btn-secondary btn-cancel-edit">Cancel</button>
        <button type="submit" class="btn-primary">Save Changes</button>
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

function openEditDetailsForm(modalEl, backdropEl, job, onSaved) {
  modalEl.innerHTML = renderEditDetailsMarkup(job);

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

      const minVal = data.get("salary_min");
      const maxVal = data.get("salary_max");
      const parsedMin = minVal ? parseInt(minVal, 10) : null;
      const parsedMax = maxVal ? parseInt(maxVal, 10) : null;

      let derivedSalaryType = data.get("salary_type");
      if (parsedMin && parsedMax) {
        derivedSalaryType = "limited";
      } else if (parsedMin && !parsedMax) {
        derivedSalaryType = "no_max";
      } else if (!parsedMin && parsedMax) {
        derivedSalaryType = "no_min";
      }

      const payload = {
        company_name: data.get("company_name"),
        position_title: data.get("position_title"),
        salary_type: derivedSalaryType,
        salary_min: parsedMin,
        salary_max: parsedMax,
        recruiter_type: data.get("recruiter_type"),
        recruiter_name: data.get("recruiter_name"),
        recruiter_agency: data.get("recruiter_agency"),
        job_post_url: data.get("job_post_url"),
        company_domain: data.get("company_domain"),
        keyword_note: data.get("keyword_note"),
        company_overview: data.get("company_overview"),
        reasons_to_change: data.get("reasons_to_change"),
        experience_notes: data.get("experience_notes"),
        expected_salary: data.get("expected_salary"),
        interview_notes: data.get("interview_notes"),
        is_referral: data.get("is_referral") === "on",
      };

      await api.updateJob(job.id, payload);
      onSaved();
    } catch (err) {
      showToast(err.message, "error");
    }
  });
}

/**
 * Opens inline modal to schedule or edit an interview meeting with support for
 * video link (Teams, Google Meet, Zoom), phone call, or on-site meeting.
 */
function openScheduleStageForm(modalEl, backdropEl, job, stage, onSaved) {
  const defaultDate = stage.meeting_date || new Date().toISOString().slice(0, 10);
  const defaultType = stage.meeting_type || "video";

  modalEl.innerHTML = `
    <div class="modal-header">
      <h2 class="modal-title inline-icon-text">${icon("calendar", 16)} Schedule Interview: ${escapeHtml(stage.custom_title || stage.stage_type)}</h2>
      <button class="modal-close-btn">${icon("close", 14)}</button>
    </div>
    <form id="schedule-stage-form" style="padding: 24px 28px; display: flex; flex-direction: column; gap: 16px; overflow-y: auto;">
      <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 16px;">
        <div>
          <label class="meta-label">Interview Date</label>
          <input type="date" name="meeting_date" value="${escapeAttr(defaultDate)}" required style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        </div>
        <div>
          <label class="meta-label">Interview Time / Window</label>
          <input type="text" name="meeting_time" value="${escapeAttr(stage.meeting_time || '')}" placeholder="e.g. 15:30 - 16:00 or 11:00 AM CEST" required style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        </div>
      </div>

      <div style="display: grid; grid-template-columns: 1fr; gap: 16px;">
        <div>
          <label class="meta-label">Interview format</label>
          <select name="meeting_type" id="sel-meeting-type" style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);">
            <option value="video" ${defaultType === 'video' ? 'selected' : ''}>Remote · Video call</option>
            <option value="phone" ${defaultType === 'phone' ? 'selected' : ''}>Remote · Phone call</option>
            <option value="onsite" ${defaultType === 'onsite' ? 'selected' : ''}>In person · On-site</option>
          </select>
        </div>
      </div>

      <div id="video-link-row">
        <label class="meta-label">Meeting Video URL (Teams, Meet, Zoom)</label>
        <input type="url" name="meeting_url" value="${escapeAttr(stage.meeting_url || '')}" placeholder="https://meet.example.com/..." style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary);" />
        <span style="font-size: 11px; color: var(--text-muted); margin-top: 4px; display: block;">Displays a direct 1-click 'Join Meeting' button in your schedule and active stage banner.</span>
      </div>

      <div>
        <label class="meta-label">Conversation Notes</label>
        <textarea name="notes" rows="3" placeholder="Record notes from the conversation..." style="width: 100%; background: var(--bg-surface-elevated); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 8px 12px; color: var(--text-primary); resize: vertical;">${escapeHtml(stage.notes || '')}</textarea>
      </div>

      <div style="display: flex; justify-content: space-between; align-items: center; margin-top: 12px;">
        <button type="button" class="btn-secondary btn-clear-schedule" style="color: var(--status-rejected);">Clear Schedule</button>
        <div style="display: flex; gap: 10px;">
          <button type="button" class="btn-secondary btn-cancel-schedule">Cancel</button>
          <button type="submit" class="btn-primary">Save Schedule</button>
        </div>
      </div>
    </form>
  `;

  const form = modalEl.querySelector("#schedule-stage-form");
  const cancelBtn = modalEl.querySelector(".btn-cancel-schedule");
  const closeBtn = modalEl.querySelector(".modal-close-btn");
  const clearBtn = modalEl.querySelector(".btn-clear-schedule");

  const cancel = () => onSaved();
  cancelBtn.addEventListener("click", cancel);
  closeBtn.addEventListener("click", cancel);

  clearBtn.addEventListener("click", async () => {
    try {
      if (confirm("Clear scheduled meeting date and time for this stage?")) {
        await api.scheduleMeeting(stage.id, {
          meeting_date: null,
          meeting_time: null,
          meeting_url: null,
          meeting_type: stage.meeting_type || "video",
        });
        onSaved();
      }
    } catch (err) {
      showToast(err.message, "error");
    }
  });

  form.addEventListener("submit", async (e) => {
    try {
      e.preventDefault();
      const data = new FormData(form);
      await api.scheduleMeeting(stage.id, {
        meeting_date: data.get("meeting_date"),
        meeting_time: data.get("meeting_time"),
        meeting_type: data.get("meeting_type"),
        meeting_url: data.get("meeting_url"),
        notes: data.get("notes"),
      });
      onSaved();
    } catch (err) {
      showToast(err.message, "error");
    }
  });
}
