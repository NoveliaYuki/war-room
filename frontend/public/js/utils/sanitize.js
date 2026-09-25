/**
 * @fileoverview Safe HTML entity encoding and sanitization utilities.
 * Protects the UI against Cross-Site Scripting (XSS) when rendering dynamic user text.
 */

/**
 * Escapes unsafe HTML characters in a string.
 *
 * @param {string|number|null|undefined} str - The raw user input.
 * @returns {string} Sanitized string safe for innerHTML interpolation.
 */
export function escapeHtml(str) {
  if (str == null) return "";
  return String(str)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

/**
 * Escapes unsafe characters for use inside HTML attribute values.
 *
 * @param {string|number|null|undefined} str - The raw user input.
 * @returns {string} Sanitized string safe for attribute interpolation.
 */
export function escapeAttr(str) {
  return escapeHtml(str);
}

/**
 * Accepts an HTTP(S) URL or a relative URL safe for browser navigation.
 *
 * @param {string|null|undefined} value - Untrusted URL value.
 * @returns {string|null} Valid URL value, or null when its scheme is unsafe.
 */
export function safeUrl(value) {
  if (value == null || String(value).trim() === "") return null;
  const candidate = String(value).trim();
  try {
    const parsed = new URL(candidate, "https://war-room.invalid");
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") return null;
    return candidate;
  } catch {
    return null;
  }
}
