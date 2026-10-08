import { escapeHtml, safeUrl } from "./sanitize.js";

/** Renders a safe Markdown subset for job overviews. */
export function renderMarkdown(value) {
  const lines = String(value || "").replace(/\r\n?/g, "\n").split("\n");
  const output = [];
  let paragraph = [];
  let listType = "";

  const closeList = () => {
    if (listType) output.push(`</${listType}>`);
    listType = "";
  };
  const flushParagraph = () => {
    if (paragraph.length) output.push(`<p>${renderInlineMarkdown(paragraph.join(" "))}</p>`);
    paragraph = [];
  };

  for (const line of lines) {
    const heading = line.match(/^\s{0,3}(#{1,6})\s+(.+?)\s*#*\s*$/);
    const listItem = parseListItem(line);
    const quote = line.match(/^\s*>\s?(.*)$/);
    if (!line.trim()) {
      flushParagraph();
      closeList();
    } else if (heading) {
      flushParagraph();
      closeList();
      const level = heading[1].length;
      output.push(`<h${level}>${renderInlineMarkdown(heading[2])}</h${level}>`);
    } else if (listItem) {
      flushParagraph();
      const nextType = listItem.type;
      if (listType !== nextType) {
        closeList();
        output.push(`<${nextType}>`);
        listType = nextType;
      }
      output.push(`<li>${renderInlineMarkdown(listItem.text)}</li>`);
    } else if (quote) {
      flushParagraph();
      closeList();
      output.push(`<blockquote><p>${renderInlineMarkdown(quote[1])}</p></blockquote>`);
    } else {
      closeList();
      paragraph.push(line.trim());
    }
  }
  flushParagraph();
  closeList();
  return output.join("");
}

/** Identifies an ordered or unordered list item. */
function parseListItem(line) {
  const unordered = line.match(/^\s*[-*+•]\s+(.+)$/);
  if (unordered) return { type: "ul", text: unordered[1] };
  const ordered = line.match(/^\s*\d+[.)]\s+(.+)$/);
  return ordered ? { type: "ol", text: ordered[1] } : null;
}

/** Escapes plain text while rendering inline emphasis, code, and safe links. */
function renderInlineMarkdown(value) {
  const tokens = /\[([^\]]+)\]\(([^\s)]+)\)|`([^`]+)`|\*\*(.+?)\*\*|__(.+?)__|\*(?!\s)(.+?)\*|_(?!\s)(.+?)_/g;
  let result = "";
  let previousEnd = 0;
  for (const match of value.matchAll(tokens)) {
    result += escapeHtml(value.slice(previousEnd, match.index));
    if (match[1] !== undefined) {
      const url = safeUrl(match[2]);
      result += url
        ? `<a href="${escapeHtml(url)}" target="_blank" rel="noopener noreferrer">${escapeHtml(match[1])}</a>`
        : escapeHtml(match[1]);
    } else if (match[3] !== undefined) result += `<code>${escapeHtml(match[3])}</code>`;
    else if (match[4] !== undefined || match[5] !== undefined) result += `<strong>${escapeHtml(match[4] ?? match[5])}</strong>`;
    else result += `<em>${escapeHtml(match[6] ?? match[7])}</em>`;
    previousEnd = match.index + match[0].length;
  }
  return result + escapeHtml(value.slice(previousEnd));
}
