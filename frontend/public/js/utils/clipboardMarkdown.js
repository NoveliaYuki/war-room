const SKIPPED_TAGS = new Set(["SCRIPT", "STYLE", "NOSCRIPT", "IFRAME", "SVG", "IMG", "FORM", "BUTTON", "TEMPLATE"]);
const BLOCK_TAGS = new Set(["P", "DIV", "SECTION", "ARTICLE", "MAIN", "HEADER", "FOOTER", "TABLE", "TBODY", "THEAD", "TR", "DL", "DT", "DD", "PRE", "HR", "UL", "OL", "LI", "BLOCKQUOTE", "H1", "H2", "H3", "H4", "H5", "H6"]);
const HEADING_TAG = /^H([1-6])$/;
const SAFE_LINK = /^https?:\/\/[^\s()]+$/i;

/**
 * Converts copied HTML into the Markdown subset the overview renders.
 * DOMParser builds an inert document, so copied scripts and images never run or load.
 *
 * @param {string} html - Untrusted clipboard HTML.
 * @returns {string} Markdown text.
 */
export function htmlToMarkdown(html) {
  const page = new DOMParser().parseFromString(String(html || ""), "text/html");
  const blocks = [];
  collectBlocks(page.body, blocks);
  return blocks.join("\n\n");
}

/** Appends the Markdown blocks of one container, keeping loose text as paragraphs. */
function collectBlocks(parent, blocks) {
  let line = "";
  const flush = () => {
    const text = collapseSpace(line);
    if (text) blocks.push(text);
    line = "";
  };
  for (const node of parent.childNodes) {
    if (isBlock(node)) {
      flush();
      appendBlock(node, blocks);
    } else {
      line += inlineMarkdown(node);
    }
  }
  flush();
}

function isBlock(node) {
  return node.nodeType === 1 && BLOCK_TAGS.has(node.tagName);
}

/** Renders one block element by its kind. */
function appendBlock(element, blocks) {
  const heading = element.tagName.match(HEADING_TAG);
  if (heading) {
    const text = collapseSpace(element.textContent);
    if (text) blocks.push(`${"#".repeat(Number(heading[1]))} ${text}`);
  } else if (element.tagName === "UL" || element.tagName === "OL") {
    const items = listLines(element);
    if (items.length) blocks.push(items.join("\n"));
  } else if (element.tagName === "BLOCKQUOTE") {
    const inner = [];
    collectBlocks(element, inner);
    inner.forEach((block) => blocks.push(`> ${block.replace(/\n/g, " ")}`));
  } else if (element.tagName !== "HR") {
    collectBlocks(element, blocks);
  }
}

/** Flattens a list, since the Markdown renderer has no nested lists. */
function listLines(list) {
  const lines = [];
  [...list.children].filter((item) => item.tagName === "LI").forEach((item, index) => {
    const marker = list.tagName === "OL" ? `${index + 1}. ` : "- ";
    const nested = [...item.children].filter((child) => child.tagName === "UL" || child.tagName === "OL");
    const text = collapseSpace([...item.childNodes].filter((child) => !nested.includes(child)).map(inlineMarkdown).join(" "));
    if (text) lines.push(marker + text);
    nested.forEach((child) => lines.push(...listLines(child)));
  });
  return lines;
}

/** Renders text, emphasis, and safe links inside a block. */
function inlineMarkdown(node) {
  if (node.nodeType === 3) return node.textContent;
  if (node.nodeType !== 1 || SKIPPED_TAGS.has(node.tagName.toUpperCase())) return "";
  if (node.tagName === "BR") return " ";
  const inner = [...node.childNodes].map(inlineMarkdown).join("");
  if (node.tagName === "STRONG" || node.tagName === "B") return wrapInline(inner, "**");
  if (node.tagName === "EM" || node.tagName === "I") return wrapInline(inner, "*");
  if (node.tagName === "A") return linkMarkdown(node, inner);
  return inner;
}

/** Keeps surrounding spaces outside the markers so emphasis still renders. */
function wrapInline(text, marker) {
  const trimmed = text.trim();
  if (!trimmed) return text;
  const start = text.indexOf(trimmed);
  return `${text.slice(0, start)}${marker}${trimmed}${marker}${text.slice(start + trimmed.length)}`;
}

function linkMarkdown(node, text) {
  const href = node.getAttribute("href") || "";
  const label = collapseSpace(text);
  return label && SAFE_LINK.test(href) && !/[[\]]/.test(label) ? `[${label}](${href})` : text;
}

function collapseSpace(value) {
  return String(value).replace(/\s+/g, " ").trim();
}
