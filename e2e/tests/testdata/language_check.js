// Runs inside the page for TestEveryLanguageRendersOnlyItsOwnText.
// "text" lists every visible string; "layout" lists text that overflows, is clipped, or overlaps another control.
(task) => {
  const visible = (el) => {
    for (let node = el; node && node.nodeType === 1; node = node.parentElement) {
      const style = getComputedStyle(node);
      if (node.hidden || style.display === "none" || style.visibility === "hidden" || Number(style.opacity) < 0.01) return false;
      if (style.clipPath !== "none" || (style.clip && style.clip !== "auto")) return false;
      // Content of a closed <details> keeps a layout box in some engines but is not shown.
      if (node.parentElement?.matches("details:not([open])") && node.tagName !== "SUMMARY") return false;
    }
    const rect = el.getBoundingClientRect();
    return rect.width > 1 && rect.height > 1;
  };

  const pathOf = (el) => {
    const parts = [];
    for (let node = el; node && node.nodeType === 1 && parts.length < 5; node = node.parentElement) {
      let part = node.tagName.toLowerCase();
      if (node.id && !/\d/.test(node.id)) { parts.unshift(`${part}#${node.id}`); break; }
      const classes = [...node.classList].filter((name) => !/^(active|is-|open|selected|waiting|ongoing|accepted|rejected|pending|current|completed)/.test(name)).slice(0, 2);
      if (classes.length) part += `.${classes.join(".")}`;
      parts.unshift(part);
    }
    return parts.join(">");
  };

  function collectText() {
    const out = new Set();
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      const value = node.nodeValue.replace(/\s+/g, " ").trim();
      const parent = node.parentElement;
      if (value && parent && !parent.closest("script, style, template, noscript") && visible(parent)) out.add(`text\t${value}`);
    }
    const attributes = ["placeholder", "aria-label", "title", "alt"];
    for (const el of document.querySelectorAll(attributes.map((name) => `[${name}]`).join(","))) {
      if (!visible(el) && !el.matches("[aria-label]")) continue;
      for (const name of attributes) {
        const value = el.getAttribute(name)?.trim();
        if (value && (visible(el) || name === "aria-label")) out.add(`${name}\t${value}`);
      }
    }
    for (const el of document.querySelectorAll("input:not([type=hidden], [type=checkbox], [type=radio], [type=file]), textarea")) {
      if (visible(el) && el.value.trim()) out.add(`value\t${el.value.trim().replace(/\s+/g, " ")}`);
    }
    for (const el of document.querySelectorAll("select")) {
      if (visible(el) && el.selectedOptions[0]) out.add(`selected\t${el.selectedOptions[0].textContent.trim()}`);
    }
    out.add(`title\t${document.title}`);
    return [...out];
  }

  function textBox(parent) {
    for (let box = parent; box && box !== document.body; box = box.parentElement) {
      const style = getComputedStyle(box);
      const inline = style.display.startsWith("inline") && !/block|flex|grid/.test(style.display);
      if (!inline && (parseFloat(style.borderLeftWidth) > 0 || parseFloat(style.borderRightWidth) > 0
        || !/rgba\(0, 0, 0, 0\)|transparent/.test(style.backgroundColor) || style.overflowX !== "visible")) return box;
    }
    return null;
  }

  function insideScroller(el) {
    for (let node = el; node && node !== document.body; node = node.parentElement) {
      if (/auto|scroll/.test(getComputedStyle(node).overflowX)) return true;
    }
    return false;
  }

  // Line-clamped user content (card titles and highlights) is shortened on purpose.
  const clamped = (el) => {
    for (let node = el; node && node !== document.body; node = node.parentElement) {
      const clamp = getComputedStyle(node).webkitLineClamp;
      if (clamp && clamp !== "none") return true;
    }
    return false;
  };

  function textOverflow(add) {
    const viewport = document.documentElement.clientWidth;
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      const parent = node.parentElement;
      if (!node.nodeValue.trim() || !parent || parent.closest("script, style, template, noscript, option, textarea") || !visible(parent)) continue;
      const range = document.createRange();
      range.selectNodeContents(node);
      const rects = [...range.getClientRects()].filter((rect) => rect.width > 0.5);
      const box = textBox(parent);
      let clipped = false;
      if (box) {
        const outer = box.getBoundingClientRect();
        const style = getComputedStyle(box);
        const left = outer.left + parseFloat(style.borderLeftWidth);
        const right = outer.right - parseFloat(style.borderRightWidth);
        const top = outer.top + parseFloat(style.borderTopWidth);
        const bottom = outer.bottom - parseFloat(style.borderBottomWidth);
        for (const rect of rects) {
          if (!/auto|scroll/.test(style.overflowX) && (rect.right > right + 1 || rect.left < left - 1)) { clipped = true; add("text outside its box", parent, node.nodeValue); break; }
          if (!/auto|scroll/.test(style.overflowY) && (rect.bottom > bottom + 1.5 || rect.top < top - 1.5) && !clamped(parent)) { clipped = true; add("text outside its box", parent, node.nodeValue); break; }
        }
      }
      if (!clipped && !insideScroller(parent) && rects.some((rect) => rect.right > viewport + 1 || rect.left < -1)) add("text off screen", parent, node.nodeValue);
    }
  }

  function truncation(add) {
    for (const el of document.querySelectorAll("body *")) {
      if (visible(el) && getComputedStyle(el).textOverflow === "ellipsis" && el.scrollWidth > el.clientWidth + 1) add("text cut off with an ellipsis", el, el.innerText);
    }
    const canvas = document.createElement("canvas").getContext("2d");
    for (const el of document.querySelectorAll("input, select")) {
      if (!visible(el) || ["checkbox", "radio", "hidden", "file", "color", "range"].includes(el.type)) continue;
      const style = getComputedStyle(el);
      const text = el.tagName === "SELECT" ? el.selectedOptions[0]?.textContent || "" : (!el.value && el.placeholder) || "";
      canvas.font = `${style.fontStyle} ${style.fontWeight} ${style.fontSize} ${style.fontFamily}`;
      if (text && canvas.measureText(text).width > el.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight) + 1) add("field text clipped", el, text);
    }
  }

  function overlaps(add) {
    const layerOf = (el) => {
      for (let node = el; node && node !== document.body; node = node.parentElement) if (/absolute|fixed|sticky/.test(getComputedStyle(node).position)) return node;
      return document.body;
    };
    const controls = [...document.querySelectorAll("button, a[href], input:not([type=hidden]), select, textarea, [role=button], [role=tab], [role^=menuitem], [class*=pill], [class*=badge]")]
      .filter((el) => visible(el) && !el.matches(".search-focus"))
      .map((el) => ({ el, rect: el.getBoundingClientRect(), layer: layerOf(el) }))
      .filter(({ rect }) => rect.bottom > 0 && rect.top < innerHeight && rect.right > 0 && rect.left < innerWidth);
    controls.forEach((a, index) => controls.slice(index + 1).forEach((b) => {
      if (a.layer !== b.layer || a.el.contains(b.el) || b.el.contains(a.el)) return;
      const width = Math.min(a.rect.right, b.rect.right) - Math.max(a.rect.left, b.rect.left);
      const height = Math.min(a.rect.bottom, b.rect.bottom) - Math.max(a.rect.top, b.rect.top);
      if (width <= 2 || height <= 2) return;
      const stack = document.elementsFromPoint(Math.max(a.rect.left, b.rect.left) + width / 2, Math.max(a.rect.top, b.rect.top) + height / 2);
      if (stack.some((node) => a.el.contains(node)) && stack.some((node) => b.el.contains(node))) add("controls overlap", a.el, `${a.el.innerText} / ${b.el.innerText}`);
    }));
  }

  function inspectLayout() {
    const findings = [];
    const add = (kind, el, text) => findings.push({ kind, path: pathOf(el), text: String(text).replace(/\s+/g, " ").trim().slice(0, 80) });
    textOverflow(add);
    truncation(add);
    overlaps(add);
    if (document.documentElement.scrollWidth > document.documentElement.clientWidth + 1) add("page scrolls sideways", document.documentElement, "");
    return findings;
  }

  return task === "text" ? collectText() : inspectLayout();
}
