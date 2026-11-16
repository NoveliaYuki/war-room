/**
 * @fileoverview Interface language selection and message lookup.
 * Every dictionary is bundled with the app, so switching languages never waits on the network.
 */

import en from "./locales/en.js";
import es from "./locales/es.js";
import ca from "./locales/ca.js";

/** Browser storage key for a language the user picked explicitly. */
export const LANGUAGE_STORAGE_KEY = "war-room.language";

/** Event dispatched on window after the interface language changes. */
export const LANGUAGE_CHANGE_EVENT = "war-room:languagechange";

/** Supported interface languages, each named in its own language. */
export const LANGUAGES = Object.freeze([
  Object.freeze({ code: "en", name: "English" }),
  Object.freeze({ code: "es", name: "Español" }),
  Object.freeze({ code: "ca", name: "Català" }),
]);

const DICTIONARIES = { en, es, ca };
const DEFAULT_LANGUAGE = "en";
const TRANSLATED_ATTRIBUTES = ["aria-label", "title", "placeholder"];
const pluralRulesCache = new Map();

/**
 * Returns the supported language for a BCP 47 tag, such as "ca" for "ca-ES".
 * @param {unknown} tag - Language tag to match.
 * @returns {string|null} A supported language code, or null.
 */
export function matchLanguage(tag) {
  const primary = String(tag || "").trim().toLowerCase().split(/[-_]/)[0];
  return Object.hasOwn(DICTIONARIES, primary) ? primary : null;
}

/**
 * Picks the saved language, then the first supported browser language, then English.
 * Mirrored by public/js/language.js, which runs before the first paint.
 * @param {string|null} saved - Explicitly chosen language, if any.
 * @param {readonly string[]} browserLanguages - Browser preferences in priority order.
 * @returns {string} A supported language code.
 */
export function resolveLanguage(saved, browserLanguages = []) {
  const savedLanguage = matchLanguage(saved);
  if (savedLanguage) return savedLanguage;
  for (const tag of browserLanguages) {
    const language = matchLanguage(tag);
    if (language) return language;
  }
  return DEFAULT_LANGUAGE;
}

function readSavedLanguage() {
  try { return window.localStorage.getItem(LANGUAGE_STORAGE_KEY); } catch { return null; }
}

function readBrowserLanguages() {
  const languages = window.navigator?.languages;
  if (Array.isArray(languages) && languages.length) return languages;
  return [window.navigator?.language].filter(Boolean);
}

let currentLanguage = resolveLanguage(readSavedLanguage(), readBrowserLanguages());

/** Returns the active interface language code. */
export function getLanguage() {
  return currentLanguage;
}

/**
 * Returns the locale used by Intl formatters. A browser regional variant of the
 * active language (for example en-GB) keeps its date and number conventions.
 * @returns {string} A BCP 47 locale tag.
 */
export function getLocale() {
  return readBrowserLanguages().find((tag) => matchLanguage(tag) === currentLanguage) || currentLanguage;
}

function selectPlural(entry, count) {
  if (typeof entry === "string") return entry;
  if (!pluralRulesCache.has(currentLanguage)) pluralRulesCache.set(currentLanguage, new Intl.PluralRules(currentLanguage));
  const category = pluralRulesCache.get(currentLanguage).select(Number(count));
  return entry[category] ?? entry.other;
}

function interpolate(template, params) {
  return template.replace(/\{(\w+)\}/g, (match, name) => (Object.hasOwn(params, name) ? String(params[name]) : match));
}

/**
 * Translates a message key into the active language.
 * Plural messages are objects keyed by Intl.PluralRules category and select on params.count.
 * @param {string} key - Message key.
 * @param {Record<string, unknown>} [params] - Values for {placeholder} slots.
 * @returns {string} Plain text; callers must still escape it before inserting HTML.
 */
export function t(key, params = {}) {
  const entry = DICTIONARIES[currentLanguage][key] ?? en[key];
  if (entry === undefined) return key;
  return interpolate(selectPlural(entry, params.count), params);
}

function readParams(element) {
  if (!element.dataset.i18nParams) return {};
  try { return JSON.parse(element.dataset.i18nParams); } catch { return {}; }
}

function translateAttributes(root, attribute) {
  root.querySelectorAll(`[data-i18n-${attribute}]`).forEach((element) => {
    element.setAttribute(attribute, t(element.getAttribute(`data-i18n-${attribute}`), readParams(element)));
  });
}

/**
 * Re-renders every keyed text node and attribute below a root.
 * data-i18n sets textContent, so it belongs only on elements without child elements.
 * @param {ParentNode} [root=document] - Subtree to translate.
 */
export function applyTranslations(root = document) {
  root.querySelectorAll("[data-i18n]").forEach((element) => {
    element.textContent = t(element.dataset.i18n, readParams(element));
  });
  TRANSLATED_ATTRIBUTES.forEach((attribute) => translateAttributes(root, attribute));
}

function storeParams(element, params) {
  if (Object.keys(params).length) element.dataset.i18nParams = JSON.stringify(params);
  else delete element.dataset.i18nParams;
}

/**
 * Sets an element's text from a key and keeps it current across language changes.
 * @param {HTMLElement} element - Element without child elements.
 * @param {string} key - Message key.
 * @param {Record<string, unknown>} [params] - Values for {placeholder} slots.
 */
export function setTranslatedText(element, key, params = {}) {
  element.dataset.i18n = key;
  storeParams(element, params);
  element.textContent = t(key, params);
}

/**
 * Sets a translated attribute and keeps it current across language changes.
 * @param {HTMLElement} element - Element to label.
 * @param {"aria-label"|"title"|"placeholder"} attribute - Attribute to set.
 * @param {string} key - Message key.
 * @param {Record<string, unknown>} [params] - Values for {placeholder} slots; shared with the element's other keys.
 */
export function setTranslatedAttribute(element, attribute, key, params = {}) {
  element.setAttribute(`data-i18n-${attribute}`, key);
  storeParams(element, params);
  element.setAttribute(attribute, t(key, params));
}

/**
 * Switches the interface language, translates the document, and notifies listeners.
 * @param {string} language - Supported language code.
 * @param {{persist?: boolean}} [options] - Whether to remember the choice in this browser.
 * @returns {boolean} Whether the language changed.
 */
export function setLanguage(language, { persist = true } = {}) {
  const next = matchLanguage(language);
  if (!next) return false;
  if (persist) {
    try { window.localStorage.setItem(LANGUAGE_STORAGE_KEY, next); } catch { /* The choice applies to this page when storage is blocked. */ }
  }
  if (next === currentLanguage) return false;
  currentLanguage = next;
  document.documentElement.lang = next;
  applyTranslations(document);
  window.dispatchEvent(new CustomEvent(LANGUAGE_CHANGE_EVENT, { detail: { language: next } }));
  return true;
}

/** Translates the static page and reveals it once it matches the active language. */
export function initializeDocumentLanguage() {
  document.documentElement.lang = currentLanguage;
  applyTranslations(document);
  delete document.documentElement.dataset.i18nPending;
}
