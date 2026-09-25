/**
 * @fileoverview Universal double-click inline editing utility.
 * Allows candidates to double click on any text element across the application
 * (company name, role title, recruiter info, links, notes, questions) to edit it inline.
 * Pressing Enter or clicking outside confirms and saves the change.
 * Pressing Escape cancels without saving.
 */

/**
 * Attaches double-click inline editing to a target DOM element.
 *
 * @param {HTMLElement} element - The DOM element to make editable.
 * @param {Object} options
 * @param {() => string} [options.getValue] - Function returning the raw value to edit
 * @param {boolean} [options.multiline=false] - Whether to use a textarea instead of an input
 * @param {string} [options.placeholder=''] - Placeholder text for empty state
 * @param {number} [options.maxLength] - Maximum character length
 * @param {number} [options.rows] - Default rows for textarea
 * @param {string} [options.eventName='dblclick'] - Event that starts inline editing.
 * @param {boolean} [options.updateText=true] - Optimistically update element text immediately
 * @param {(val: string) => string} [options.formatDisplay] - Formatter function for display value
 * @param {(newValue: string) => Promise<void> | void} options.onSave - Callback when value is confirmed
 * @param {() => void} [options.onCancel] - Callback when edit is cancelled
 */
export function makeInlineEditable(element, options = {}) {
  if (!element) return;
  element.classList.add("editable-text");
  element.addEventListener(options.eventName || "dblclick", (event) => startInlineEditing(element, options, event));
}

/** Resolves the current value before replacing the display with an editor. */
function getInlineValue(element, options) {
  if (options.getValue) return options.getValue();
  return element.getAttribute("data-raw-value") ?? element.textContent.trim();
}

/** Creates and configures an input control for the edit mode. */
function createInlineInput(rawValue, options) {
  const multiline = Boolean(options.multiline);
  const input = document.createElement(multiline ? "textarea" : "input");
  if (multiline) {
    input.className = "inline-edit-textarea";
    input.rows = options.rows || Math.max(3, Math.min(12, rawValue.split("\n").length + 1));
  } else {
    input.type = "text";
    input.className = "inline-edit-input";
  }
  if (options.placeholder) input.placeholder = options.placeholder;
  if (options.maxLength) input.maxLength = options.maxLength;
  input.value = rawValue;
  return input;
}

/** Displays a toast if the global toast function is available. */
function showInlineToast(message, type, duration) {
  if (typeof window !== "undefined" && typeof window.showToast === "function") {
    window.showToast(message, type, duration);
  }
}

/** Applies the edited display value while retaining the raw value attribute. */
function updateInlineDisplay(element, options, newValue) {
  if (options.updateText === false) return;
  if (options.formatDisplay) element.textContent = options.formatDisplay(newValue);
  else element.textContent = newValue || options.placeholder || "";
  element.setAttribute("data-raw-value", newValue);
}

/** Creates the idempotent commit callback for one editor. */
function createCommitHandler(element, input, rawValue, originalDisplay, options, state) {
  return async function commit() {
    if (state.finished) return;
    state.finished = true;
    const newValue = input.value.trim();
    input.remove();
    element.style.display = originalDisplay;
    element.removeAttribute("data-is-editing");
    if (newValue === rawValue) {
      options.onCancel?.();
      return;
    }
    updateInlineDisplay(element, options, newValue);
    try {
      await options.onSave?.(newValue);
      showInlineToast("Saved!", "success", 1200);
    } catch (error) {
      console.error("Failed to save inline edit:", error);
      showInlineToast("Failed to save", "error", 2000);
    }
  };
}

/** Creates the idempotent cancel callback for one editor. */
function createCancelHandler(element, input, originalDisplay, options, state) {
  return () => {
    if (state.finished) return;
    state.finished = true;
    input.remove();
    element.style.display = originalDisplay;
    element.removeAttribute("data-is-editing");
    options.onCancel?.();
  };
}

/** Starts editing and attaches commit/cancel handlers. */
function startInlineEditing(element, options, event) {
  event.preventDefault();
  event.stopPropagation();
  if (element.getAttribute("data-is-editing") === "true") return;
  element.setAttribute("data-is-editing", "true");
  const rawValue = getInlineValue(element, options);
  const input = createInlineInput(rawValue, options);
  const originalDisplay = element.style.display || "";
  element.style.display = "none";
  element.parentNode.insertBefore(input, element);
  input.focus();
  input.select();
  const state = { finished: false };
  const commit = createCommitHandler(element, input, rawValue, originalDisplay, options, state);
  const cancel = createCancelHandler(element, input, originalDisplay, options, state);
  bindInlineInputEvents(input, Boolean(options.multiline), commit, cancel);
}

/** Binds keyboard and blur events to one inline editor. */
function bindInlineInputEvents(input, multiline, commit, cancel) {
  input.addEventListener("keydown", (event) => {
    if (event.key === "Enter" && (!multiline || !event.shiftKey)) {
      event.preventDefault();
      commit();
    } else if (event.key === "Escape") {
      event.preventDefault();
      cancel();
    }
  });
  input.addEventListener("blur", commit);
}

/**
 * Parses user-typed salary input (e.g. "80k - 95k", "up to 90000", "€70,000–€85,000")
 * into schema-compatible fields: { salary_type, salary_min, salary_max }.
 *
 * @param {string} str
 * @returns {{ salary_type: string, salary_min: number|null, salary_max: number|null }}
 */
export function parseSalaryInput(str) {
  if (!str) return { salary_type: "unknown", salary_min: null, salary_max: null };
  let cleaned = str.trim().toLowerCase();
  if (
    cleaned === "unknown" ||
    cleaned === "undisclosed" ||
    cleaned === "salary undisclosed" ||
    cleaned === "none" ||
    cleaned === "-"
  ) {
    return { salary_type: "unknown", salary_min: null, salary_max: null };
  }

  // Remove currency symbols so currency-prefixed ranges parse cleanly
  cleaned = cleaned.replace(/[$€£]/g, "").trim();

  const parseNum = (val) => {
    if (!val) return null;
    val = val.replace(/[^0-9.k]/gi, "").trim();
    if (val.toLowerCase().endsWith("k")) {
      return Math.round(parseFloat(val.slice(0, -1)) * 1000);
    }
    const n = parseFloat(val);
    return isNaN(n) ? null : Math.round(n);
  };

  // Find all numbers with optional 'k'
  const matches = Array.from(cleaned.matchAll(/([\d.,]+k?)/gi))
    .map((m) => parseNum(m[1]))
    .filter((n) => n !== null && n > 0);

  // If two or more numbers are present, it's a range (limited)
  if (matches.length >= 2) {
    const min = Math.min(matches[0], matches[1]);
    const max = Math.max(matches[0], matches[1]);
    return {
      salary_type: "limited",
      salary_min: min,
      salary_max: max,
    };
  }

  // If exactly one number is present
  if (matches.length === 1) {
    const val = matches[0];
    const isMin = /(?:from|>|min)/i.test(cleaned);
    if (isMin) {
      return { salary_type: "no_max", salary_min: val, salary_max: null };
    }
    return { salary_type: "no_min", salary_min: null, salary_max: val };
  }

  return { salary_type: "unknown", salary_min: null, salary_max: null };
}
