/**
 * Sets the page language before the first paint. When it is not English, the page
 * stays hidden until boot.js translates it, so English text never flashes first.
 * Mirrors resolveLanguage in i18n.js: saved choice, then browser languages, then English.
 */
(() => {
  const supported = ["en", "es", "ca"];
  const match = (tag) => {
    const primary = String(tag || "").trim().toLowerCase().split(/[-_]/)[0];
    return supported.includes(primary) ? primary : null;
  };
  let saved = null;
  try {
    saved = window.localStorage.getItem("war-room.language");
  } catch {
    // Browser language detection still applies when storage is blocked.
  }
  const browser = window.navigator.languages?.length ? window.navigator.languages : [window.navigator.language];
  const language = match(saved) || browser.map(match).find(Boolean) || "en";
  document.documentElement.lang = language;
  if (language !== "en") document.documentElement.dataset.i18nPending = "";
})();
