/**
 * @fileoverview Language selectors in the account menu and on the sign-in screen.
 */

import { LANGUAGES, LANGUAGE_CHANGE_EVENT, getLanguage, setLanguage } from "../i18n.js";
import { escapeAttr, escapeHtml } from "../utils/sanitize.js";

/** Renders one option per language; menu switches use radio menu items, others toggle buttons. */
function renderLanguageOptions(container) {
  const inMenu = container.dataset.languageSwitch === "menu";
  container.innerHTML = LANGUAGES.map(({ code, name }) => `<button type="button" class="language-option" lang="${escapeAttr(code)}" data-language-option="${escapeAttr(code)}" ${inMenu ? 'role="menuitemradio" aria-checked="false"' : 'aria-pressed="false"'}>${escapeHtml(name)}</button>`).join("");
}

/** Marks the active language in every selector on the page. */
function syncLanguageOptions() {
  const language = getLanguage();
  document.querySelectorAll("[data-language-option]").forEach((option) => {
    const state = option.getAttribute("role") === "menuitemradio" ? "aria-checked" : "aria-pressed";
    option.setAttribute(state, String(option.dataset.languageOption === language));
  });
}

/** Renders and wires every language selector on the page. */
export function bindLanguageSwitches() {
  document.querySelectorAll("[data-language-switch]").forEach((container) => {
    renderLanguageOptions(container);
    container.addEventListener("click", (event) => {
      const option = event.target.closest("[data-language-option]");
      if (option) setLanguage(option.dataset.languageOption);
    });
  });
  window.addEventListener(LANGUAGE_CHANGE_EVENT, syncLanguageOptions);
  syncLanguageOptions();
}
