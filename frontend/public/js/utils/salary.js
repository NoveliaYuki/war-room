import { t } from "../i18n.js";

/**
 * Formats salary fields into the compact label shown on job cards and details.
 *
 * @param {string} type - Salary range type from the backend.
 * @param {number|null} min - Optional lower salary bound.
 * @param {number|null} max - Optional upper salary bound.
 * @param {string} [currency='EUR'] - ISO currency code.
 * @returns {string} Formatted salary range or undisclosed label.
 */
export function formatSalary(type, min, max, currency = "EUR") {
  const symbols = { EUR: "€", GBP: "£", USD: "$" };
  const symbol = symbols[currency] || "€";
  if (min != null && max != null) return formatSalaryRange(min, max, symbol);
  if (min != null || type === "no_max") return formatSalaryBound(min, symbol, "salary.from");
  if (max != null || type === "no_min") return formatSalaryBound(max, symbol, "salary.upTo");
  return t("salary.undisclosed");
}

/** Formats equal or ranged salary bounds. */
function formatSalaryRange(min, max, symbol) {
  const compactMin = compactSalary(min);
  if (Number(min) === Number(max)) return `${symbol}${compactMin}`;
  return `${symbol}${compactMin}-${compactSalary(max)}`;
}

/** Formats one salary bound with its translated "from" or "up to" wording. */
function formatSalaryBound(value, symbol, key) {
  if (value == null) return t("salary.undisclosed");
  return t(key, { amount: `${symbol}${compactSalary(value)}` });
}

/** Converts a numeric salary to a compact display value. */
function compactSalary(value) {
  const amount = Number(value);
  if (!Number.isFinite(amount)) return String(value);
  if (Math.abs(amount) < 1000) return String(amount);
  const thousands = amount / 1000;
  return `${Number.isInteger(thousands) ? thousands : thousands.toFixed(1)}k`;
}
