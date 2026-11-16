/**
 * @fileoverview Converts the Markdown subset War Room renders into readable plain text,
 * for places that cannot render Markdown, such as a spreadsheet cell.
 */

// Links, autolinks, code, and emphasis. Underscore emphasis must sit at word boundaries so snake_case survives.
// A backslash-escaped marker is literal text.
const INLINE = /\[([^\]]+)\]\(([^\s)]+)\)|<(https?:\/\/[^\s>]+)>|`([^`]+)`|(?<!\\)\*\*(.+?)\*\*|(?<![\\\w])__(.+?)__(?!\w)|(?<!\\)\*(?!\s)(.+?)\*|(?<![\\\w])_(?!\s)(.+?)_(?!\w)/g;
const ESCAPED = /\\([\\`*_{}[\]()#+\-.!>|])/g;
const RULE = /^\s{0,3}([-*_])(?:\s*\1){2,}\s*$/;
const FENCE = /^\s*(?:```|~~~)/;

/** Rewrites one inline match as text: a link becomes "label (url)", and markers around code or emphasis are dropped. */
function inlineMatch(match, label, url, autolink, code, ...emphasis) {
  if (label !== undefined) return label === url ? url : `${inlineText(label)} (${url})`;
  if (autolink !== undefined) return autolink;
  if (code !== undefined) return code;
  return inlineText(emphasis.slice(0, 4).find((text) => text !== undefined));
}

function inlineText(text) {
  return text.replace(INLINE, inlineMatch).replace(ESCAPED, "$1");
}

function indent(prefix) {
  return prefix.replace(/\t/g, "  ");
}

/** Converts one Markdown line to text. */
function lineText(line) {
  if (RULE.test(line) || FENCE.test(line)) return "";
  const heading = line.match(/^\s{0,3}#{1,6}\s+(.+?)\s*#*\s*$/);
  if (heading) return inlineText(heading[1]);
  const quote = line.match(/^\s*>\s?(.*)$/);
  if (quote) return lineText(quote[1]);
  const bullet = line.match(/^(\s*)[-*+•]\s+(.+)$/);
  if (bullet) return `${indent(bullet[1])}• ${inlineText(bullet[2])}`;
  const numbered = line.match(/^(\s*)(\d+[.)])\s+(.+)$/);
  if (numbered) return `${indent(numbered[1])}${numbered[2]} ${inlineText(numbered[3])}`;
  return inlineText(line.trim());
}

/**
 * Converts Markdown to plain text. Headings, emphasis, and code lose their markers, links read
 * "label (url)", bullets start with "• ", line and paragraph breaks are kept, and runs of blank
 * lines collapse to one.
 *
 * @param {unknown} value - Markdown source; null and undefined become "".
 * @returns {string} The plain text.
 */
export function markdownToText(value) {
  const lines = String(value ?? "").replace(/\r\n?/g, "\n").split("\n").map(lineText);
  return lines.join("\n").replace(/[ \t]+$/gm, "").replace(/\n{3,}/g, "\n\n").trim();
}
