/**
 * @fileoverview Automatic Google Drive backup schedule in Data & backups.
 * The schedule is chosen in plain words (Off, Every day, Every week on a weekday, at a time)
 * and stored as structured fields with the browser's time zone; no cron syntax is ever shown.
 */

import { api } from "../api.js";
import { LANGUAGE_CHANGE_EVENT, getLocale, t } from "../i18n.js";
import { icon } from "../icons.js";
import { escapeAttr, escapeHtml } from "../utils/sanitize.js";

/** Weekdays in display order, Monday first; values follow JavaScript (0 is Sunday). */
const WEEKDAY_ORDER = [1, 2, 3, 4, 5, 6, 0];
const FREQUENCIES = ["off", "daily", "weekly"];
const DEFAULT_SCHEDULE = Object.freeze({ frequency: "off", weekday: 1, time: "09:00", time_zone: "UTC" });

/** Last-run results that mean the backup did not reach Drive. */
const PROBLEM_RESULTS = new Set(["failed", "google_access_expired", "drive_not_connected", "drive_not_configured"]);

/**
 * Returns the browser's IANA time zone, such as Europe/Madrid.
 * @returns {string} The zone name, or UTC when the browser does not report one.
 */
export function browserTimeZone() {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}

/**
 * Names a weekday in the selected language.
 * @param {number} day - 0 (Sunday) through 6 (Saturday).
 * @returns {string} The full weekday name.
 */
export function weekdayName(day) {
  // 1 January 2023 was a Sunday, so adding day gives that weekday.
  return new Intl.DateTimeFormat(getLocale(), { weekday: "long", timeZone: "UTC" }).format(Date.UTC(2023, 0, 1 + day));
}

/**
 * Formats an HH:MM time of day the way the selected language writes times.
 * @param {string} time - 24-hour HH:MM.
 * @returns {string} The localized time, or the input when it is not HH:MM.
 */
export function formatTimeOfDay(time) {
  const match = /^(\d{2}):(\d{2})$/.exec(time || "");
  if (!match) return time || "";
  return new Intl.DateTimeFormat(getLocale(), { hour: "numeric", minute: "2-digit", timeZone: "UTC" })
    .format(Date.UTC(2023, 0, 2, Number(match[1]), Number(match[2])));
}

/** Words "at {time}"; Spanish and Catalan use a singular article for one o'clock. */
function atTime(time) {
  const key = time.startsWith("01:") ? "backupSchedule.atOne" : "backupSchedule.at";
  return t(key, { time: formatTimeOfDay(time) });
}

/**
 * Describes a schedule in one plain-language sentence, such as "Backs up every Monday at 9:00 AM."
 * @param {{frequency: string, weekday: number, time: string, time_zone?: string}} schedule - The schedule.
 * @param {string} [currentZone] - The viewer's time zone; a different schedule zone is named.
 * @returns {string} The sentence.
 */
export function scheduleSummary(schedule, currentZone = browserTimeZone()) {
  if (schedule.frequency === "daily" || schedule.frequency === "weekly") {
    const at = atTime(schedule.time);
    const sentence = schedule.frequency === "daily"
      ? t("backupSchedule.summaryDaily", { at })
      : t("backupSchedule.summaryWeekly", { weekday: weekdayName(Number(schedule.weekday)), at });
    if (!schedule.time_zone || schedule.time_zone === currentZone) return sentence;
    return t("backupSchedule.summaryInZone", { summary: sentence, zone: schedule.time_zone });
  }
  return t("backupSchedule.summaryOff");
}

/** Formats an instant as a weekday, date, and time in the schedule's zone. */
function formatRunDate(value, timeZone) {
  const options = { weekday: "long", day: "numeric", month: "long", hour: "numeric", minute: "2-digit" };
  try {
    return new Intl.DateTimeFormat(getLocale(), { ...options, timeZone }).format(new Date(value));
  } catch {
    return new Intl.DateTimeFormat(getLocale(), options).format(new Date(value));
  }
}

/**
 * Describes when the next automatic backup runs.
 * @param {{frequency: string, next_run_at?: string|null, time_zone?: string}} schedule - The saved schedule.
 * @returns {string} The sentence, or "" when backups are off.
 */
export function nextRunText(schedule) {
  if (schedule.frequency === "off" || !schedule.next_run_at) return "";
  return t("backupSchedule.nextRun", { date: formatRunDate(schedule.next_run_at, schedule.time_zone) });
}

/**
 * Describes the result of the last automatic backup.
 * @param {{frequency: string, last_run_at?: string|null, last_run_result?: string, time_zone?: string}} schedule - The saved schedule.
 * @returns {{text: string, problem: boolean}} The sentence ("" when there is nothing to report) and whether it is a failure.
 */
export function lastRunText(schedule) {
  if (!schedule.last_run_at || !schedule.last_run_result) {
    return { text: schedule.frequency === "off" ? "" : t("backupSchedule.noRunYet"), problem: false };
  }
  const date = formatRunDate(schedule.last_run_at, schedule.time_zone);
  if (schedule.last_run_result === "ok") return { text: t("backupSchedule.lastRunOk", { date }), problem: false };
  const result = PROBLEM_RESULTS.has(schedule.last_run_result) ? schedule.last_run_result : "failed";
  return { text: t(`backupSchedule.lastRun.${result}`, { date }), problem: true };
}

/**
 * Explains why a saved schedule cannot upload yet.
 * @param {{configured?: boolean, connected?: boolean}|null} driveStatus - The Google Drive status, if known.
 * @returns {string} The explanation, or "" when Drive is ready.
 */
export function readinessNote(driveStatus) {
  if (!driveStatus) return "";
  if (!driveStatus.configured) return t("backupSchedule.notConfigured");
  if (!driveStatus.connected) return t("backupSchedule.notConnected");
  return "";
}

function frequencyOptions(selected) {
  return FREQUENCIES.map((frequency) => `<label><input type="radio" name="backup-frequency" value="${frequency}" ${frequency === selected ? "checked" : ""} /><span>${escapeHtml(t(`backupSchedule.frequency.${frequency}`))}</span></label>`).join("");
}

function weekdayOptions(selected) {
  return WEEKDAY_ORDER.map((day) => `<option value="${day}" ${day === Number(selected) ? "selected" : ""}>${escapeHtml(weekdayName(day))}</option>`).join("");
}

/** Reads the schedule the form currently shows. */
function readForm(form, timeZone) {
  const frequency = form.querySelector("input[name=backup-frequency]:checked")?.value || "off";
  const time = form.querySelector("#backup-schedule-time").value;
  return {
    frequency,
    weekday: Number(form.querySelector("#backup-schedule-weekday").value),
    // Off ignores the time, so a cleared field still saves.
    time: frequency === "off" && !/^\d{2}:\d{2}$/.test(time) ? DEFAULT_SCHEDULE.time : time,
    time_zone: timeZone,
  };
}

/** Shows only the fields the chosen frequency needs, and the matching summary. */
function syncForm(form, state) {
  const draft = readForm(form, browserTimeZone());
  form.querySelector("#backup-schedule-weekday-field").hidden = draft.frequency !== "weekly";
  form.querySelector("#backup-schedule-time-field").hidden = draft.frequency === "off";
  form.querySelector("#backup-schedule-time").required = draft.frequency !== "off";
  const summary = draft.frequency === "off" || /^\d{2}:\d{2}$/.test(draft.time) ? scheduleSummary(draft) : "";
  form.querySelector("#backup-schedule-summary").textContent = summary;
  state.draft = draft;
}

function statusHtml(state) {
  if (state.demo) return `<p class="backup-schedule-note">${escapeHtml(t("backupSchedule.demoUnavailable"))}</p>`;
  if (state.loadFailed) return `<p class="backup-schedule-note account-error">${escapeHtml(t("backupSchedule.loadFailed"))}</p>`;
  const saved = state.saved;
  const next = nextRunText(saved);
  const last = lastRunText(saved);
  const ready = saved.frequency === "off" ? "" : readinessNote(state.driveStatus);
  return [
    next && `<p id="backup-schedule-next" class="backup-schedule-note">${icon("clock", 13)} ${escapeHtml(next)}</p>`,
    last.text && `<p id="backup-schedule-last" class="backup-schedule-note${last.problem ? " account-error" : ""}">${escapeHtml(last.text)}</p>`,
    ready && `<p id="backup-schedule-ready" class="backup-schedule-note">${escapeHtml(ready)}</p>`,
  ].filter(Boolean).join("");
}

function render(container, state) {
  const shown = state.draft || state.saved;
  const disabled = state.demo || state.loadFailed ? "disabled" : "";
  container.innerHTML = `<div class="backup-schedule" data-ready="${state.ready ? "true" : "false"}">
    <h4 class="backup-schedule-heading">${icon("clock", 15)} <span>${escapeHtml(t("backupSchedule.heading"))}</span></h4>
    <p class="backup-schedule-intro">${escapeHtml(t("backupSchedule.intro"))}</p>
    <form id="backup-schedule-form" class="backup-schedule-form">
      <fieldset class="backup-schedule-fields" ${disabled}>
        <fieldset class="backup-schedule-frequency"><legend>${escapeHtml(t("backupSchedule.howOften"))}</legend>
          <div class="backup-schedule-options">${frequencyOptions(shown.frequency)}</div>
        </fieldset>
        <div class="backup-schedule-when">
          <label id="backup-schedule-weekday-field"><span>${escapeHtml(t("backupSchedule.day"))}</span>
            <select id="backup-schedule-weekday">${weekdayOptions(shown.weekday)}</select></label>
          <label id="backup-schedule-time-field"><span>${escapeHtml(t("backupSchedule.time"))}</span>
            <input id="backup-schedule-time" type="time" step="60" value="${escapeAttr(shown.time)}" /></label>
        </div>
        <p id="backup-schedule-summary" class="backup-schedule-summary" aria-live="polite"></p>
        <button class="btn-primary" id="btn-save-backup-schedule" type="submit">${escapeHtml(t("backupSchedule.save"))}</button>
      </fieldset>
      <div class="data-transfer-status" id="backup-schedule-status" role="status" aria-live="polite">${escapeHtml(state.message || "")}</div>
    </form>
    <div class="backup-schedule-state">${statusHtml(state)}</div>
  </div>`;
  const form = container.querySelector("#backup-schedule-form");
  form.querySelector("#backup-schedule-weekday").value = String(shown.weekday);
  syncForm(form, state);
  form.addEventListener("change", () => syncForm(form, state));
  form.addEventListener("input", () => syncForm(form, state));
  form.addEventListener("submit", (event) => {
    event.preventDefault();
    void save(container, state, form);
  });
}

async function save(container, state, form) {
  if (state.saving || state.demo) return;
  state.saving = true;
  const button = form.querySelector("#btn-save-backup-schedule");
  button.disabled = true;
  try {
    state.saved = await api.saveBackupSchedule(readForm(form, browserTimeZone()));
    state.draft = null;
    state.message = t("backupSchedule.saved");
  } catch (error) {
    state.message = error?.message || t("backupSchedule.saveFailed");
  } finally {
    state.saving = false;
  }
  if (container.isConnected) render(container, state);
}

/**
 * Renders the schedule control into container and loads the saved schedule.
 * In the browser demo it shows the control disabled with an explanation and calls no API.
 * @param {HTMLElement} container - Where to render.
 * @param {{demo?: boolean, driveStatus?: object|null}} options - Demo mode and the current Drive status.
 * @returns {Promise<void>} Resolves once the saved schedule is shown.
 */
export async function mountBackupSchedule(container, { demo = false, driveStatus = null } = {}) {
  const state = { demo, ready: demo, driveStatus, saved: { ...DEFAULT_SCHEDULE }, draft: null, message: "", loadFailed: false, saving: false };
  const rerender = () => {
    if (!container.isConnected) {
      window.removeEventListener(LANGUAGE_CHANGE_EVENT, rerender);
      return;
    }
    state.message = "";
    render(container, state);
  };
  window.addEventListener(LANGUAGE_CHANGE_EVENT, rerender);
  render(container, state);
  if (demo) return;
  try {
    state.saved = await api.getBackupSchedule();
    state.draft = null;
  } catch {
    state.loadFailed = true;
  }
  state.ready = true;
  if (container.isConnected) render(container, state);
}
