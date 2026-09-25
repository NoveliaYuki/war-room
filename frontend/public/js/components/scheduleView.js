import { showToast } from "../utils/toast.js";
import { escapeHtml, escapeAttr, safeUrl } from "../utils/sanitize.js";
/**
 * @fileoverview Everyday Schedule & Meetings View Component.
 * Answers every day whether you have a meeting to attend or not,
 * groups upcoming interviews chronologically, and links directly to stage question banks.
 */

import { api } from "../api.js";
import { renderCompanyAvatar } from "../avatar.js";
import { openDetailModal } from "./detailModal.js";
import { icon } from "../icons.js";

const STAGE_TYPE_CLASSES = new Map([
  ["HR", "HR"],
  ["Technical", "Technical"],
  ["Cultural", "Cultural"],
  ["Offer & Decision", "Offer-Decision"],
]);
const MEETING_FORMAT_LABELS = new Map([
  ["phone", `${icon("phone", 12)} Phone Call`],
  ["onsite", `${icon("building", 12)} In-Person / On-Site`],
  ["video", `${icon("video", 12)} Video Call`],
]);

/**
 * Gets local YYYY-MM-DD string.
 */
function getLocalDateString(offsetDays = 0) {
  const d = new Date();
  d.setDate(d.getDate() + offsetDays);
  const year = d.getFullYear();
  const month = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

/**
 * Formats a date string nicely (e.g. "Tuesday, Sep 22, 2026").
 */
function formatNiceDate(dateStr) {
  if (!dateStr) return "Unscheduled Date";
  const [year, month, day] = dateStr.split("-").map(Number);
  const d = new Date(year, month - 1, day);
  return d.toLocaleDateString("en-US", {
    weekday: "long",
    month: "short",
    day: "numeric",
    year: "numeric",
  });
}

function getStageTypeClass(type) {
  return STAGE_TYPE_CLASSES.get(type) || "HR";
}

/**
 * Maps an API meeting format to a known CSS class suffix.
 * @param {string} type - Meeting format from the API.
 * @returns {string} A safe CSS class suffix.
 */
function getMeetingTypeClass(type) {
  return ["video", "phone", "onsite"].includes(type) ? type : "video";
}

/** Renders the visible meeting format label. */
function renderMeetingFormat(type) {
  return MEETING_FORMAT_LABELS.get(type) || MEETING_FORMAT_LABELS.get("video");
}

/** Renders the safe company label shown in a meeting card. */
function renderMeetingCompany(companyName) {
  if (companyName.toLowerCase() === "unknown") return `${icon("building", 13)} Unknown Company (Undisclosed)`;
  return `${icon("building", 13)} ${escapeHtml(companyName)}`;
}

/** Renders recruiter contact details when they are available. */
function renderMeetingRecruiter(meeting) {
  if (!meeting.recruiter_name) return "";
  const contact = meeting.recruiter_contact ? `<span style="color: var(--text-secondary); font-family: var(--font-mono); font-size: 11px;">(${escapeHtml(meeting.recruiter_contact)})</span>` : "";
  return `<div style="font-size: 12px; color: var(--text-muted); display: flex; align-items: center; gap: 6px; flex-wrap: wrap;"><span class="inline-icon-text">${icon("user", 12)} Recruiter: ${escapeHtml(meeting.recruiter_name)}</span>${contact}</div>`;
}

/** Renders meeting notes when they exist. */
function renderMeetingNotes(notes) {
  if (!notes) return "";
  return `<div style="font-size: 12px; color: var(--text-secondary); background: rgba(255, 255, 255, 0.04); border: 1px solid var(--border-subtle); padding: 8px 10px; border-radius: var(--radius-xs); font-family: var(--font-mono);">Note: ${escapeHtml(notes)}</div>`;
}

/** Renders the obviously fictional panel assigned to a demo meeting. */
function renderMeetingInterviewers(interviewers = []) {
  const names = interviewers.map((person) => person.name).filter(Boolean);
  if (!names.length) return "";
  return `<div class="inline-icon-text" style="font-size: 12px; color: var(--text-muted);">${icon("users", 12)} Interviewers: ${escapeHtml(names.join(", "))}</div>`;
}

/** Renders the join link or phone call details for a meeting. */
function renderMeetingJoinAction(meeting) {
  const url = safeUrl(meeting.meeting_url);
  if (url) return `<a href="${escapeAttr(url)}" target="_blank" rel="noopener noreferrer" class="btn-join-call" style="text-decoration: none;"><span class="inline-icon-text">${icon("video", 13)} Join Meeting ${icon("arrowUpRight", 11)}</span></a>`;
  if (meeting.meeting_type !== "phone") return "";
  const phone = meeting.recruiter_contact ? `<span style="font-size: 11px; opacity: 0.85;">(${escapeHtml(meeting.recruiter_contact.split("|")[0].trim())})</span>` : "";
  return `<div class="phone-call-indicator inline-icon-text">${icon("phone", 12)} Phone Call${phone}</div>`;
}

/** Renders a meeting scheduled for today. */
function renderTodayMeeting(meeting) {
  const time = meeting.meeting_time || "Time TBD";
  return `<div class="today-meeting-card" data-jobid="${escapeAttr(meeting.job_id)}">
    <div style="display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 8px;"><div style="display: flex; align-items: center; gap: 8px;">
      <span class="meeting-time-badge inline-icon-text">${icon("clock", 12)} ${escapeHtml(time)}</span>
      <span class="meeting-format-pill pill-${getMeetingTypeClass(meeting.meeting_type)} inline-icon-text">${renderMeetingFormat(meeting.meeting_type)}</span>
    </div><span class="stage-type-badge ${getStageTypeClass(meeting.stage_type)}">${escapeHtml(meeting.stage_type)}</span></div>
    <div><div style="font-size: 16px; font-weight: 700; color: var(--text-primary);">${escapeHtml(meeting.position_title)}</div>
      <div class="inline-icon-text" style="font-size: 13px; color: var(--text-secondary); margin-top: 2px;">${renderMeetingCompany(meeting.company_name)}</div></div>
    ${renderMeetingRecruiter(meeting)}${renderMeetingInterviewers(meeting.stage_interviewers)}${renderMeetingNotes(meeting.stage_notes)}
    <div style="display: flex; align-items: center; gap: 8px; margin-top: 4px; flex-wrap: wrap;">${renderMeetingJoinAction(meeting)}
      <button class="btn-primary btn-open-meeting inline-icon-text" data-jobid="${escapeAttr(meeting.job_id)}" style="padding: 6px 12px; font-size: 12px; justify-content: center;">${icon("chat", 13)} Open Questions & Prep ${icon("arrowUpRight", 11)}</button>
    </div>
  </div>`;
}

/**
 * Renders the everyday schedule view into the container.
 *
 * @param {HTMLElement} containerEl - The container element to mount to.
 * @param {HTMLElement} modalEl - Target modal element.
 * @param {HTMLElement} backdropEl - Modal backdrop.
 * @param {Function} onGlobalRefresh - Global state refresh callback.
 */
export async function renderScheduleView(containerEl, modalEl, backdropEl, onGlobalRefresh) {
  containerEl.innerHTML = `
    <div style="text-align: center; padding: 40px; color: var(--text-muted);">
      Loading your everyday schedule...
    </div>
  `;

  const meetings = await api.getMeetings();
  const todayStr = getLocalDateString(0);
  const tomorrowStr = getLocalDateString(1);

  const todayMeetings = [];
  const tomorrowMeetings = [];
  const upcomingMeetings = [];
  const pastMeetings = [];

  meetings.forEach((m) => {
    const d = m.meeting_date;
    if (!d) {
      upcomingMeetings.push(m);
      return;
    }
    if (d === todayStr) {
      todayMeetings.push(m);
    } else if (d === tomorrowStr) {
      tomorrowMeetings.push(m);
    } else if (d > todayStr) {
      upcomingMeetings.push(m);
    } else {
      pastMeetings.push(m);
    }
  });

  const hasMeetingsToday = todayMeetings.length > 0;
  const niceToday = formatNiceDate(todayStr);

  containerEl.innerHTML = `
    <div class="schedule-container">
      <div class="today-banner ${hasMeetingsToday ? 'has-meetings' : ''}">
        <div class="today-banner-header">
          <div class="today-status-title inline-icon-text">
            <span>${hasMeetingsToday ? icon("zap", 18) : icon("sparkles", 18)}</span>
            <span>Today (${niceToday})</span>
          </div>
          <span class="${hasMeetingsToday ? 'today-badge-alert' : 'today-badge-calm'}">
            ${hasMeetingsToday ? `${todayMeetings.length} Meeting(s) Scheduled Today` : 'No Meetings Today'}
          </span>
        </div>

        ${
          hasMeetingsToday
            ? `
          <div style="font-size: 14px; color: var(--text-secondary); margin-bottom: 4px;">
            You have upcoming interview sessions to attend today. Review your questions and notes ahead of time:
          </div>
          <div class="today-meetings-grid">
            ${todayMeetings.map(renderTodayMeeting).join("")}
          </div>
        `
            : `
          <div style="font-size: 14px; color: var(--text-secondary);">
            No interview calls or meetings scheduled for today. Enjoy your deep focus time or explore new selection processes!
          </div>
        `
        }
      </div>

      ${
        tomorrowMeetings.length > 0
          ? `
        <div class="schedule-day-group">
          <div class="day-group-header">
            <h3 class="day-group-title">
              <span>Tomorrow (${formatNiceDate(tomorrowStr)})</span>
            </h3>
            <span class="day-group-count">${tomorrowMeetings.length} meeting(s)</span>
          </div>
          <div class="meetings-list">
            ${renderMeetingsRows(tomorrowMeetings)}
          </div>
        </div>
      `
          : ''
      }

      ${
        upcomingMeetings.length > 0
          ? `
        <div class="schedule-day-group">
          <div class="day-group-header">
            <h3 class="day-group-title">
              <span>Upcoming Dates</span>
            </h3>
            <span class="day-group-count">${upcomingMeetings.length} meeting(s)</span>
          </div>
          <div class="meetings-list">
            ${renderMeetingsRows(upcomingMeetings)}
          </div>
        </div>
      `
          : ''
      }

      ${
        pastMeetings.length > 0
          ? `
        <div class="schedule-day-group" style="opacity: 0.75;">
          <div class="day-group-header">
            <h3 class="day-group-title">
              <span>Past Meetings</span>
            </h3>
            <span class="day-group-count">${pastMeetings.length}</span>
          </div>
          <div class="meetings-list">
            ${renderMeetingsRows(pastMeetings)}
          </div>
        </div>
      `
          : ''
      }

      ${
        meetings.length === 0
          ? `
        <div class="empty-state">
          <div class="empty-icon">${icon("calendar", 36)}</div>
          <h3 class="empty-title">No scheduled meetings yet</h3>
          <p class="empty-desc">Open any selection process card and set an interview date and time to track your meetings here.</p>
        </div>
      `
          : ''
      }
    </div>
  `;

  containerEl.querySelectorAll(".btn-open-meeting").forEach((btn) => {
    btn.addEventListener("click", async () => {
      const jobId = btn.getAttribute("data-jobid");
      backdropEl.classList.add("active");
      await openDetailModal(modalEl, backdropEl, jobId, onGlobalRefresh);
    });
  });
}

/**
 * Renders meeting rows list.
 */
function renderMeetingsRows(list = []) {
  return list
    .map((m) => {
      const avatarHtml = renderCompanyAvatar(m.company_name, m.avatar_seed, 48, m.company_domain, m.job_id);
      return `
      <div class="meeting-row-card">
        <div class="meeting-row-left">
          <div class="meeting-row-avatar">${avatarHtml}</div>
          <div class="meeting-row-info">
            <div class="meeting-row-title">
              <span>${escapeHtml(m.position_title)}</span>
              <span style="color: var(--text-secondary); font-weight: 400;">@ ${m.company_name.toLowerCase() === 'unknown' ? 'Unknown Company (Undisclosed)' : escapeHtml(m.company_name)}</span>
              <span class="stage-type-badge ${getStageTypeClass(m.stage_type)}">${escapeHtml(m.stage_type)}</span>
              <span class="meeting-format-pill pill-${getMeetingTypeClass(m.meeting_type)} inline-icon-text">
                ${
                  m.meeting_type === 'phone'
                    ? `${icon("phone", 12)} Phone Call`
                    : m.meeting_type === 'onsite'
                    ? `${icon("building", 12)} On-Site`
                    : `${icon("video", 12)} Video Call`
                }
              </span>
            </div>
            <div class="meeting-row-meta">
              <span class="meeting-time-badge inline-icon-text">
                ${icon("calendar", 12)} ${formatNiceDate(m.meeting_date)} • ${escapeHtml(m.meeting_time || 'Time TBD')}
              </span>
              ${m.recruiter_name ? `<span class="inline-icon-text">${icon("user", 12)} ${escapeHtml(m.recruiter_name)}</span>` : ''}
              ${renderMeetingInterviewers(m.stage_interviewers)}
              ${m.stage_notes ? `<span style="color: #7dd3fc; font-family: var(--font-mono); font-size: 12px;">${escapeHtml(m.stage_notes)}</span>` : ''}
            </div>
          </div>
        </div>
        <div class="meeting-row-right">
          ${
            safeUrl(m.meeting_url)
              ? `<a href="${escapeAttr(safeUrl(m.meeting_url))}" target="_blank" rel="noopener noreferrer" class="btn-join-call" style="font-size: 12px; padding: 6px 12px; text-decoration: none;">
                  <span class="inline-icon-text">
                    ${icon("video", 12)} Join Call ${icon("arrowUpRight", 11)}
                  </span>
                </a>`
              : m.meeting_type === 'phone'
              ? `<span class="phone-call-indicator inline-icon-text" style="font-size: 11px; padding: 4px 10px;">
                  ${icon("phone", 11)} Phone
                </span>`
              : ''
          }
          <button class="btn-secondary btn-open-meeting inline-icon-text" data-jobid="${escapeAttr(m.job_id)}" style="font-size: 12px; padding: 6px 14px;">
            Open Process ${icon("arrowUpRight", 11)}
          </button>
        </div>
      </div>
    `;
    })
    .join('');
}
