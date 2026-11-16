import { getLocale, t } from "../i18n.js";

/** Formats an ISO calendar date in the viewer's locale without shifting its day; `weekday` may be true or "short". */
export function formatCalendarDate(dateString, { weekday = false } = {}) {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(dateString || "");
  if (!match) return t("date.unavailable");

  const [, yearString, monthString, dayString] = match;
  const year = Number(yearString);
  const month = Number(monthString);
  const day = Number(dayString);
  const date = new Date(Date.UTC(year, month - 1, day));
  if (date.getUTCFullYear() !== year || date.getUTCMonth() !== month - 1 || date.getUTCDate() !== day) {
    return t("date.unavailable");
  }

  return new Intl.DateTimeFormat(getLocale(), {
    ...(weekday ? { weekday: weekday === "short" ? "short" : "long" } : {}),
    month: "short",
    day: "numeric",
    year: "numeric",
    timeZone: "UTC",
  }).format(date);
}
