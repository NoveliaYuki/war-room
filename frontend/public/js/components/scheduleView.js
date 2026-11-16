import { renderScheduleLoadingState } from "./loadingViews.js";
import { captureLoadingView, revealLoadedContent } from "../utils/loadingTransition.js";
import { showToast } from "../utils/toast.js";
import { escapeHtml, escapeAttr, safeUrl } from "../utils/sanitize.js";
import { formatCalendarDate } from "../utils/date.js";
import { renderMeetingLinkContent } from "../utils/meetingLink.js";
/**
 * @fileoverview Everyday Schedule & Meetings View Component.
 * Answers every day whether you have a meeting to attend or not,
 * groups upcoming interviews chronologically, and links directly to stage question banks.
 */

import { api } from "../api.js";
import { renderCompanyAvatar } from "../avatar.js";
import { openDetailModal } from "./detailModal.js";
import { icon } from "../icons.js";
import { t } from "../i18n.js";
import { isUndisclosedCompany, meetingFormatLabel, stageTypeLabel } from "../utils/labels.js";

const STAGE_TYPE_CLASSES = new Map([
  ["HR", "HR"],
  ["Technical", "Technical"],
  ["Cultural", "Cultural"],
  ["Offer & Decision", "Offer-Decision"],
]);
const MEETING_FORMAT_ICONS = new Map([["phone", "phone"], ["onsite", "building"], ["video", "video"]]);
const MEETING_NOTE_CONTAINERS = new WeakSet();
const PAST_MEETINGS_STATE_KEY = "war-room.past-meetings-expanded";

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
 * Formats a date string nicely (e.g. "Tuesday, Sep 22, 2026"); pass "short" for "Tue, Sep 22, 2026".
 */
function formatNiceDate(dateStr, weekday = true) {
  if (!dateStr) return t("schedule.unscheduledDate");
  return formatCalendarDate(dateStr, { weekday });
}

/** Formats a meeting count with the matching singular or plural noun. */
function formatMeetingCount(count) {
  return t("schedule.meetingCount", { count });
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
  return `${icon(MEETING_FORMAT_ICONS.get(type) || "video", 12)} ${escapeHtml(meetingFormatLabel(type))}`;
}

/** Shows an undisclosed company by its translated label. */
function companyDisplayName(companyName) {
  return isUndisclosedCompany(companyName) ? t("company.unknownUndisclosed") : companyName;
}

/** Renders the safe company label shown in a meeting card. */
function renderMeetingCompany(companyName) {
  return `${icon("building", 13)} ${escapeHtml(companyDisplayName(companyName))}`;
}

/** Renders recruiter contact details when they are available. */
function renderMeetingRecruiter(meeting) {
  if (!meeting.recruiter_name) return "";
  const contact = meeting.recruiter_contact ? `<span class="meeting-recruiter-contact">(${escapeHtml(meeting.recruiter_contact)})</span>` : "";
  return `<div style="font-size: 12px; color: var(--text-muted); display: flex; align-items: center; gap: 6px; flex-wrap: wrap;"><span class="inline-icon-text">${icon("user", 12)} ${escapeHtml(t("schedule.recruiter", { name: meeting.recruiter_name }))}</span>${contact}</div>`;
}

/** Renders meeting notes when they exist. */
function renderMeetingNotes(notes, { labeled = true } = {}) {
  if (!notes) return "";
  return `<div class="meeting-note" data-meeting-note>
    <span class="meeting-note-content is-clamped">${escapeHtml(labeled ? t("schedule.note", { notes }) : notes)}</span>
    <button class="meeting-note-toggle" type="button" aria-expanded="false" hidden>${escapeHtml(t("schedule.showFullNote"))}</button>
  </div>`;
}

/** Updates long-note controls after rendering or a viewport resize. */
function updateMeetingNotePreviews(containerEl) {
  containerEl.querySelectorAll("[data-meeting-note]").forEach((note) => {
    const content = note.querySelector(".meeting-note-content");
    const toggle = note.querySelector(".meeting-note-toggle");
    const expanded = note.classList.contains("is-expanded");
    content.classList.toggle("is-clamped", !expanded);
    toggle.hidden = !expanded && content.scrollHeight <= content.clientHeight + 1;
    toggle.textContent = t(expanded ? "schedule.showLess" : "schedule.showFullNote");
    toggle.setAttribute("aria-expanded", String(expanded));
  });
}

/** Adds one delegated click handler for expanding full meeting notes. */
function bindMeetingNotePreviews(containerEl) {
  if (!MEETING_NOTE_CONTAINERS.has(containerEl)) {
    containerEl.addEventListener("click", (event) => {
      const toggle = event.target.closest(".meeting-note-toggle");
      if (!toggle || !containerEl.contains(toggle)) return;
      toggle.closest("[data-meeting-note]").classList.toggle("is-expanded");
      updateMeetingNotePreviews(containerEl);
    });
    window.addEventListener("resize", () => updateMeetingNotePreviews(containerEl));
    MEETING_NOTE_CONTAINERS.add(containerEl);
  }
  updateMeetingNotePreviews(containerEl);
}

/** Restores and saves the past-meetings disclosure state for this tab session. */
function bindPastMeetingsDisclosure(containerEl) {
  const disclosure = containerEl.querySelector(".past-meetings-group");
  if (!disclosure) return;

  try {
    disclosure.open = window.sessionStorage.getItem(PAST_MEETINGS_STATE_KEY) === "true";
  } catch {
    return;
  }

  disclosure.addEventListener("toggle", () => {
    try {
      window.sessionStorage.setItem(PAST_MEETINGS_STATE_KEY, String(disclosure.open));
    } catch {
      // Keep the disclosure usable for this render when session storage is unavailable.
    }
  });
}

/** Renders the obviously fictional panel assigned to a demo meeting. */
function renderMeetingInterviewers(interviewers = []) {
  const names = interviewers.map((person) => person.name).filter(Boolean);
  if (!names.length) return "";
  return `<div class="inline-icon-text" style="font-size: 12px; color: var(--text-muted);">${icon("users", 12)} ${escapeHtml(t("schedule.interviewers", { names: names.join(", ") }))}</div>`;
}

/** Renders the join or Maps link, or phone call details, for a meeting. */
function renderMeetingJoinAction(meeting) {
  const url = safeUrl(meeting.meeting_url);
  if (url) return `<a href="${escapeAttr(url)}" target="_blank" rel="noopener noreferrer" class="btn-join-call" style="text-decoration: none;"><span class="inline-icon-text">${renderMeetingLinkContent(meeting.meeting_type, t("meeting.joinMeeting"), 13)}</span></a>`;
  if (meeting.meeting_type !== "phone") return "";
  const phone = meeting.recruiter_contact ? `<span style="font-size: 11px; opacity: 0.85;">(${escapeHtml(meeting.recruiter_contact.split("|")[0].trim())})</span>` : "";
  return `<div class="phone-call-indicator inline-icon-text">${icon("phone", 12)} ${escapeHtml(t("meeting.phoneCall"))}${phone}</div>`;
}

/** Renders a meeting's title as the keyboard entry point to its process. */
function renderMeetingTitle(meeting, className = "") {
  const label = t("schedule.openMeeting", { position: meeting.position_title, stage: stageTypeLabel(meeting.stage_type) });
  return `<button type="button" class="meeting-open-trigger ${className}" data-open-meeting aria-label="${escapeAttr(label)}">${escapeHtml(meeting.position_title)}</button>`;
}

/** Renders a meeting scheduled for today. */
function renderTodayMeeting(meeting) {
  const time = meeting.meeting_time || t("schedule.timeTbd");
  const joinAction = renderMeetingJoinAction(meeting);
  return `<div class="today-meeting-card" data-meeting-card data-jobid="${escapeAttr(meeting.job_id)}" data-stageid="${escapeAttr(meeting.stage_id || "")}">
    <div style="display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 8px;"><div style="display: flex; align-items: center; gap: 8px;">
      <span class="meeting-time-badge inline-icon-text">${icon("clock", 12)} ${escapeHtml(time)}</span>
      <span class="meeting-format-pill pill-${getMeetingTypeClass(meeting.meeting_type)} inline-icon-text">${renderMeetingFormat(meeting.meeting_type)}</span>
    </div><span class="stage-type-badge ${getStageTypeClass(meeting.stage_type)}">${escapeHtml(stageTypeLabel(meeting.stage_type))}</span></div>
    <div><div style="font-size: 16px; font-weight: 700; color: var(--text-primary);">${renderMeetingTitle(meeting)}</div>
      <div class="inline-icon-text" style="font-size: 13px; color: var(--text-secondary); margin-top: 2px;">${renderMeetingCompany(meeting.company_name)}</div></div>
    ${renderMeetingRecruiter(meeting)}${renderMeetingInterviewers(meeting.stage_interviewers)}${renderMeetingNotes(meeting.stage_notes)}
    <div class="today-meeting-actions">${joinAction}
      <button class="btn-primary btn-open-meeting inline-icon-text" data-open-meeting>${icon("chat", 13)} ${escapeHtml(t("schedule.openPrep"))} ${icon("arrowUpRight", 11)}</button>
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
 * @param {Object} [options] - Initial loading behavior and meetings already fetched by the app.
 */
export async function renderScheduleView(containerEl, modalEl, backdropEl, onGlobalRefresh, { showLoading = true, meetings: prefetchedMeetings } = {}) {
  if (showLoading) renderScheduleLoadingState(containerEl);

  const meetings = prefetchedMeetings ?? await api.getMeetings();
  const loadingView = showLoading ? captureLoadingView(containerEl) : null;
  containerEl.innerHTML = renderScheduleMarkup(meetings);

  bindMeetingNotePreviews(containerEl);
  bindPastMeetingsDisclosure(containerEl);

  containerEl.querySelectorAll("[data-meeting-card]").forEach((card) => {
    card.addEventListener("click", (event) => {
      if (!shouldOpenMeeting(event)) return;
      const { jobid: jobId, stageid: stageId } = card.dataset;
      void openDetailModal(modalEl, backdropEl, jobId, onGlobalRefresh, { stageId });
    });
  });
  revealLoadedContent(containerEl, loadingView);
}

/** Leaves links, note toggles, and text selection to their own behavior. */
function shouldOpenMeeting(event) {
  const control = event.target.closest("a, button, summary");
  if (control && !control.hasAttribute("data-open-meeting")) return false;
  return !window.getSelection()?.toString();
}

/** Builds the schedule sections from one meeting snapshot. */
function renderScheduleMarkup(meetings) {
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

  return `
    <div class="schedule-container">
      <div class="today-banner ${hasMeetingsToday ? 'has-meetings' : ''}">
        <div class="today-banner-header">
          <div class="today-status-title inline-icon-text">
            <span>${hasMeetingsToday ? icon("zap", 18) : icon("sparkles", 18)}</span>
            <span>${escapeHtml(t("schedule.today", { date: niceToday }))}</span>
          </div>
          <span class="${hasMeetingsToday ? 'today-badge-alert' : 'today-badge-calm'}">
            ${escapeHtml(hasMeetingsToday ? t("schedule.scheduledToday", { count: todayMeetings.length }) : t("schedule.noMeetingsToday"))}
          </span>
        </div>

        ${
          hasMeetingsToday
            ? `
          <div style="font-size: 14px; color: var(--text-secondary); margin-bottom: 4px;">
            ${escapeHtml(t("schedule.todayIntro"))}
          </div>
          <div class="today-meetings-grid">
            ${todayMeetings.map(renderTodayMeeting).join("")}
          </div>
        `
            : `
          <div style="font-size: 14px; color: var(--text-secondary);">
            ${escapeHtml(t("schedule.todayEmpty"))}
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
              <span>${escapeHtml(t("schedule.tomorrow", { date: formatNiceDate(tomorrowStr) }))}</span>
            </h3>
            <span class="day-group-count">${formatMeetingCount(tomorrowMeetings.length)}</span>
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
              <span>${escapeHtml(t("schedule.upcoming"))}</span>
            </h3>
            <span class="day-group-count">${formatMeetingCount(upcomingMeetings.length)}</span>
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
        <details class="schedule-day-group past-meetings-group">
          <summary class="day-group-header past-meetings-summary">
            <h3 class="day-group-title">
              <span>${escapeHtml(t("schedule.past"))}</span>
            </h3>
            <span class="day-group-count">${pastMeetings.length}</span>
            <span class="past-meetings-chevron" aria-hidden="true">${icon("chevronDown", 16)}</span>
          </summary>
          <div class="meetings-list">
            ${renderMeetingsRows(pastMeetings)}
          </div>
        </details>
      `
          : ''
      }

      ${
        meetings.length === 0
          ? `
        <div class="empty-state">
          <div class="empty-icon">${icon("calendar", 36)}</div>
          <h3 class="empty-title">${escapeHtml(t("schedule.emptyTitle"))}</h3>
          <p class="empty-desc">${escapeHtml(t("schedule.emptyBody"))}</p>
        </div>
      `
          : ''
      }
    </div>
  `;

}

/** Renders a row's join link or phone indicator, or nothing when it has neither. */
function renderMeetingRowAction(m, meetingUrl) {
  if (meetingUrl) {
    return `<div class="meeting-row-right"><a href="${escapeAttr(meetingUrl)}" target="_blank" rel="noopener noreferrer" class="btn-join-call" style="font-size: 12px; padding: 6px 12px; text-decoration: none;">
      <span class="inline-icon-text">${renderMeetingLinkContent(m.meeting_type, t("meeting.joinCall"), 12)}</span>
    </a></div>`;
  }
  if (m.meeting_type !== 'phone') return '';
  return `<div class="meeting-row-right"><span class="phone-call-indicator inline-icon-text" style="font-size: 11px; padding: 4px 10px;">${icon("phone", 11)} ${escapeHtml(t("meeting.phone"))}</span></div>`;
}

/**
 * Renders meeting rows list.
 */
function renderMeetingsRows(list = []) {
  return list
    .map((m) => {
      const avatarHtml = renderCompanyAvatar(m.company_name, m.avatar_seed, 48, m.company_domain, m.job_id);
      const meetingUrl = safeUrl(m.meeting_url);
      return `
      <div class="meeting-row-card${meetingUrl ? ' has-meeting-url' : ''}" data-meeting-card data-jobid="${escapeAttr(m.job_id)}" data-stageid="${escapeAttr(m.stage_id || '')}">
        <div class="meeting-row-left">
          <div class="meeting-row-avatar">${avatarHtml}</div>
          <div class="meeting-row-info">
            <div class="meeting-row-heading">
              <div class="meeting-row-identity">
                ${renderMeetingTitle(m, "meeting-row-position")}
                <span class="meeting-row-company">@ ${escapeHtml(companyDisplayName(m.company_name))}</span>
              </div>
              <div class="meeting-row-badges">
              <span class="stage-type-badge ${getStageTypeClass(m.stage_type)}">${escapeHtml(stageTypeLabel(m.stage_type))}</span>
              <span class="meeting-format-pill pill-${getMeetingTypeClass(m.meeting_type)} inline-icon-text">
                ${renderMeetingFormat(m.meeting_type)}
              </span>
              </div>
            </div>
            <div class="meeting-row-meta">
              <span class="meeting-time-badge inline-icon-text">
                ${icon("calendar", 12)} <span class="meeting-time-part">${escapeHtml(formatNiceDate(m.meeting_date, "short"))}</span> <span class="meeting-time-part">• ${escapeHtml(m.meeting_time || t("schedule.timeTbd"))}</span>
              </span>
              ${m.recruiter_name ? `<span class="inline-icon-text">${icon("user", 12)} ${escapeHtml(m.recruiter_name)}</span>` : ''}
              ${renderMeetingInterviewers(m.stage_interviewers)}
              ${renderMeetingNotes(m.stage_notes, { labeled: false })}
            </div>
          </div>
        </div>
        ${renderMeetingRowAction(m, meetingUrl)}
      </div>
    `;
    })
    .join('');
}
