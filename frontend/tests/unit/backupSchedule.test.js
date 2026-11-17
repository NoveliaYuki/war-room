import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const { api } = vi.hoisted(() => ({
  api: { getBackupSchedule: vi.fn(), saveBackupSchedule: vi.fn() },
}));
vi.mock("../../public/js/api.js", () => ({ api }));

import { setLanguage } from "../../public/js/i18n.js";
import {
  browserTimeZone, formatTimeOfDay, lastRunText, mountBackupSchedule, nextRunText, readinessNote, scheduleSummary, weekdayName,
} from "../../public/js/components/backupSchedule.js";

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

function container() {
  document.body.innerHTML = '<section><div id="schedule"></div></section>';
  return document.querySelector("#schedule");
}

function choose(element, frequency) {
  element.querySelectorAll("input[name=backup-frequency]").forEach((radio) => { radio.checked = radio.value === frequency; });
  element.querySelector("#backup-schedule-form").dispatchEvent(new Event("change"));
}

function saved(overrides = {}) {
  return { frequency: "weekly", weekday: 1, time: "09:00", time_zone: browserTimeZone(), next_run_at: "2026-11-16T08:00:00Z", last_run_at: null, last_run_result: "", ...overrides };
}

describe("backup schedule wording", () => {
  beforeEach(() => setLanguage("en", { persist: false }));
  afterEach(() => setLanguage("en", { persist: false }));

  it("names weekdays and times in the selected language", () => {
    expect(weekdayName(1)).toBe("Monday");
    expect(weekdayName(0)).toBe("Sunday");
    setLanguage("es", { persist: false });
    expect(weekdayName(1)).toBe("lunes");
    expect(formatTimeOfDay("09:05")).toBe("9:05");
    setLanguage("ca", { persist: false });
    expect(weekdayName(6)).toBe("dissabte");
    expect(formatTimeOfDay("bad")).toBe("bad");
    expect(formatTimeOfDay("")).toBe("");
  });

  it("describes a schedule in a plain sentence without cron syntax", () => {
    const zone = browserTimeZone();
    expect(scheduleSummary({ frequency: "off" })).toBe("Automatic backups are off.");
    expect(scheduleSummary({ frequency: "daily", time: "18:30", time_zone: zone })).toBe(`Backs up every day at ${formatTimeOfDay("18:30")}.`);
    expect(scheduleSummary({ frequency: "weekly", weekday: 1, time: "09:00", time_zone: zone })).toBe(`Backs up every Monday at ${formatTimeOfDay("09:00")}.`);
    expect(scheduleSummary({ frequency: "weekly", weekday: 1, time: "09:00", time_zone: "Asia/Tokyo" }, "Europe/Madrid")).toContain("Times are in Asia/Tokyo.");
    expect(scheduleSummary({ frequency: "weekly", weekday: 1, time: "09:00" }, "Europe/Madrid")).not.toContain("Times are in");
    setLanguage("es", { persist: false });
    expect(scheduleSummary({ frequency: "weekly", weekday: 1, time: "09:00", time_zone: zone })).toBe("Hace una copia cada lunes a las 9:00.");
    expect(scheduleSummary({ frequency: "daily", time: "01:30", time_zone: zone })).toBe("Hace una copia cada día a la 1:30.");
    for (const text of [scheduleSummary({ frequency: "daily", time: "00:00" }), scheduleSummary({ frequency: "off" })]) {
      expect(text).not.toMatch(/\*/);
    }
  });

  it("describes the next run and every last-run result", () => {
    expect(nextRunText({ frequency: "off", next_run_at: null })).toBe("");
    expect(nextRunText(saved({ time_zone: "Europe/Madrid" }))).toMatch(/^Next backup: Monday, November 16.*9:00/);
    expect(nextRunText(saved({ time_zone: "Not/AZone" }))).toMatch(/^Next backup: /);
    expect(lastRunText(saved({ frequency: "off" }))).toEqual({ text: "", problem: false });
    expect(lastRunText(saved())).toEqual({ text: "No automatic backup has run yet.", problem: false });
    expect(lastRunText(saved({ last_run_at: "2026-11-16T08:00:00Z", last_run_result: "" })).text).toBe("No automatic backup has run yet.");
    expect(lastRunText(saved({ last_run_at: "2026-11-16T08:00:00Z", last_run_result: "ok" }))).toMatchObject({ problem: false, text: expect.stringMatching(/^Last automatic backup: .*completed\.$/) });
    const expired = lastRunText(saved({ last_run_at: "2026-11-16T08:00:00Z", last_run_result: "google_access_expired" }));
    expect(expired.problem).toBe(true);
    expect(expired.text).toContain("Sign out and sign in again");
    expect(lastRunText(saved({ last_run_at: "2026-11-16T08:00:00Z", last_run_result: "drive_not_connected" })).text).toContain("is not connected");
    expect(lastRunText(saved({ last_run_at: "2026-11-16T08:00:00Z", last_run_result: "drive_not_configured" })).text).toContain("not set up");
    expect(lastRunText(saved({ last_run_at: "2026-11-16T08:00:00Z", last_run_result: "something_new" })).text).toContain("server log");
  });

  it("explains when Drive is not ready", () => {
    expect(readinessNote(null)).toBe("");
    expect(readinessNote({ configured: false })).toContain("not set up on this server");
    expect(readinessNote({ configured: true, connected: false })).toContain("once Google Drive is connected");
    expect(readinessNote({ configured: true, connected: true })).toBe("");
  });

  it("falls back to UTC when the browser reports no time zone", () => {
    const spy = vi.spyOn(Intl, "DateTimeFormat").mockImplementation(() => ({ resolvedOptions: () => ({}) }));
    expect(browserTimeZone()).toBe("UTC");
    spy.mockImplementation(() => { throw new Error("no Intl"); });
    expect(browserTimeZone()).toBe("UTC");
    spy.mockRestore();
  });
});

describe("backup schedule control", () => {
  beforeEach(() => {
    setLanguage("en", { persist: false });
    api.getBackupSchedule.mockReset();
    api.saveBackupSchedule.mockReset();
  });

  it("is disabled with an explanation in the demo and calls no API", async () => {
    const element = container();
    await mountBackupSchedule(element, { demo: true });
    expect(api.getBackupSchedule).not.toHaveBeenCalled();
    expect(element.querySelector(".backup-schedule-fields").disabled).toBe(true);
    expect(element.textContent).toContain("not available in the browser demo");
    expect(element.querySelector("#backup-schedule-summary").textContent).toBe("Automatic backups are off.");
  });

  it("loads the saved schedule and shows only the fields it needs", async () => {
    api.getBackupSchedule.mockResolvedValue(saved({ last_run_at: "2026-11-09T08:00:00Z", last_run_result: "google_access_expired" }));
    const element = container();
    await mountBackupSchedule(element, { driveStatus: { configured: true, connected: false } });
    expect(element.querySelector("input[value=weekly]").checked).toBe(true);
    expect(element.querySelector("#backup-schedule-weekday-field").hidden).toBe(false);
    expect(element.querySelector("#backup-schedule-weekday").value).toBe("1");
    expect([...element.querySelectorAll("#backup-schedule-weekday option")].map((option) => option.textContent)).toEqual(["Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"]);
    expect(element.querySelector("#backup-schedule-summary").textContent).toContain("every Monday");
    expect(element.querySelector("#backup-schedule-next").textContent).toContain("Next backup:");
    expect(element.querySelector("#backup-schedule-last").classList.contains("account-error")).toBe(true);
    expect(element.querySelector("#backup-schedule-ready").textContent).toContain("once Google Drive is connected");

    choose(element, "daily");
    expect(element.querySelector("#backup-schedule-weekday-field").hidden).toBe(true);
    expect(element.querySelector("#backup-schedule-summary").textContent).toContain("every day");

    choose(element, "off");
    expect(element.querySelector("#backup-schedule-time-field").hidden).toBe(true);
    expect(element.querySelector("#backup-schedule-time").required).toBe(false);
    expect(element.querySelector("#backup-schedule-summary").textContent).toBe("Automatic backups are off.");

    choose(element, "weekly");
    const time = element.querySelector("#backup-schedule-time");
    time.value = "";
    time.dispatchEvent(new Event("input", { bubbles: true }));
    expect(element.querySelector("#backup-schedule-summary").textContent).toBe("");
  });

  it("saves the chosen schedule with the browser time zone", async () => {
    api.getBackupSchedule.mockResolvedValue(saved({ frequency: "off", next_run_at: null }));
    api.saveBackupSchedule.mockImplementation(async (schedule) => ({ ...saved(), ...schedule, last_run_result: "" }));
    const element = container();
    await mountBackupSchedule(element);
    choose(element, "weekly");
    element.querySelector("#backup-schedule-weekday").value = "3";
    element.querySelector("#backup-schedule-time").value = "18:45";
    element.querySelector("#backup-schedule-form").dispatchEvent(new Event("submit", { cancelable: true }));
    await flush();
    expect(api.saveBackupSchedule).toHaveBeenCalledWith({ frequency: "weekly", weekday: 3, time: "18:45", time_zone: browserTimeZone() });
    expect(element.querySelector("#backup-schedule-status").textContent).toBe("Schedule saved.");
    expect(element.querySelector("#backup-schedule-summary").textContent).toContain("every Wednesday");
  });

  it("saves Off even when the hidden time was cleared", async () => {
    api.getBackupSchedule.mockResolvedValue(saved());
    api.saveBackupSchedule.mockResolvedValue(saved({ frequency: "off", next_run_at: null }));
    const element = container();
    await mountBackupSchedule(element);
    choose(element, "off");
    element.querySelector("#backup-schedule-time").value = "";
    element.querySelector("#backup-schedule-form").dispatchEvent(new Event("submit", { cancelable: true }));
    await flush();
    expect(api.saveBackupSchedule).toHaveBeenCalledWith(expect.objectContaining({ frequency: "off", time: "09:00" }));
  });

  it("shows a save failure and ignores a second submit while saving", async () => {
    api.getBackupSchedule.mockResolvedValue(saved());
    let reject;
    api.saveBackupSchedule.mockImplementation(() => new Promise((_resolve, fail) => { reject = fail; }));
    const element = container();
    await mountBackupSchedule(element);
    const form = element.querySelector("#backup-schedule-form");
    form.dispatchEvent(new Event("submit", { cancelable: true }));
    form.dispatchEvent(new Event("submit", { cancelable: true }));
    expect(api.saveBackupSchedule).toHaveBeenCalledTimes(1);
    reject(new Error("Choose a day of the week."));
    await flush();
    expect(element.querySelector("#backup-schedule-status").textContent).toBe("Choose a day of the week.");

    api.saveBackupSchedule.mockRejectedValue({});
    element.querySelector("#backup-schedule-form").dispatchEvent(new Event("submit", { cancelable: true }));
    await flush();
    expect(element.querySelector("#backup-schedule-status").textContent).toBe("Could not save the backup schedule");
  });

  it("reports a schedule that cannot load and keeps the form disabled", async () => {
    api.getBackupSchedule.mockRejectedValue(new Error("offline"));
    const element = container();
    await mountBackupSchedule(element);
    expect(element.querySelector(".backup-schedule-fields").disabled).toBe(true);
    expect(element.textContent).toContain("Could not load your backup schedule");
  });

  it("re-renders in a new language and stops once removed", async () => {
    api.getBackupSchedule.mockResolvedValue(saved());
    const element = container();
    await mountBackupSchedule(element);
    setLanguage("ca", { persist: false });
    expect(element.querySelector(".backup-schedule-heading").textContent).toContain("Còpies automàtiques");
    expect(element.querySelector("#backup-schedule-summary").textContent).toContain("dilluns");
    document.body.innerHTML = "";
    setLanguage("en", { persist: false });
    expect(element.textContent).toContain("Còpies automàtiques");
  });

  it("does not render after the dialog closes while loading", async () => {
    let resolve;
    api.getBackupSchedule.mockImplementation(() => new Promise((done) => { resolve = done; }));
    const element = container();
    const mounted = mountBackupSchedule(element);
    document.body.innerHTML = "";
    resolve(saved({ frequency: "daily" }));
    await mounted;
    expect(element.querySelector("input[value=daily]").checked).toBe(false);
  });
});
