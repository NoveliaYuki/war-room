/**
 * @fileoverview Form validation bubbles in the selected language.
 * Browsers word their built-in bubbles in the browser's own language, so each failed field gets a translated message.
 */

import { getLocale, t } from "../i18n.js";

/** Formats a numeric attribute for the selected language, or returns it unchanged. */
function formatLimit(value) {
  const number = Number(value);
  return value !== "" && Number.isFinite(number) ? new Intl.NumberFormat(getLocale()).format(number) : value;
}

/** Explains a field the user left empty. */
function missingMessage(field) {
  return field.type === "checkbox" || field.type === "radio" || field.tagName === "SELECT" ? t("validation.choose") : t("validation.required");
}

/** Explains a value the browser could not accept as the field's type. */
function typeMessage(field) {
  if (field.type === "url") return t("validation.url");
  if (field.type === "email") return t("validation.email");
  if (field.type === "number") return t("validation.number");
  if (field.type === "date") return t("validation.date");
  return t("validation.invalid");
}

/**
 * Returns the translated message for a field's current validity problem.
 *
 * @param {HTMLInputElement|HTMLSelectElement|HTMLTextAreaElement} field - A field that failed validation.
 * @returns {string} The message, or "" when the field is valid.
 */
export function validationMessage(field) {
  const { validity } = field;
  if (validity.valid) return "";
  if (validity.valueMissing) return missingMessage(field);
  if (validity.typeMismatch || validity.badInput) return typeMessage(field);
  return limitMessage(field, validity);
}

/** Explains a value outside the field's range, step, length, or pattern. */
function limitMessage(field, validity) {
  if (validity.rangeUnderflow) return t("validation.min", { min: formatLimit(field.min) });
  if (validity.rangeOverflow) return t("validation.max", { max: formatLimit(field.max) });
  if (validity.stepMismatch) return t("validation.step", { step: formatLimit(field.step) });
  if (validity.tooShort) return t("validation.tooShort", { min: formatLimit(field.minLength) });
  if (validity.tooLong) return t("validation.tooLong", { max: formatLimit(field.maxLength) });
  if (validity.patternMismatch) return t("validation.pattern");
  return t("validation.invalid");
}

/** Clears a translated message so the browser checks the field afresh. */
function clearMessage(field) {
  if (typeof field?.setCustomValidity === "function") field.setCustomValidity("");
}

/** Clears every translated message in the form a submit button belongs to. */
function clearFormMessages(event) {
  const button = event.target?.closest?.("button, input[type='submit'], input[type='image']");
  if (!button?.form || button.type !== "submit" && button.type !== "image") return;
  [...button.form.elements].forEach(clearMessage);
}

/**
 * Words the browser's validation bubbles in the selected language for every form in the document.
 *
 * @param {Document} [root] - The document to watch.
 */
export function bindValidationMessages(root = document) {
  // The message is set while the browser reports the problem, and cleared on the next edit or submit so the field is checked again.
  root.addEventListener("invalid", (event) => {
    const field = event.target;
    clearMessage(field);
    const message = validationMessage(field);
    if (message) field.setCustomValidity(message);
  }, true);
  root.addEventListener("input", (event) => clearMessage(event.target), true);
  root.addEventListener("change", (event) => clearMessage(event.target), true);
  root.addEventListener("click", clearFormMessages, true);
}
