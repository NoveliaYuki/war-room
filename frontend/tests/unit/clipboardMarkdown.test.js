import { describe, expect, it } from "vitest";
import { htmlToMarkdown } from "../../public/js/utils/clipboardMarkdown.js";

describe("clipboard Markdown", () => {
  it("keeps the posting's structure", () => {
    const html = `<h2>About</h2><p>Be <strong>great </strong>at <em>this</em></p><ul><li>One<ul><li>Nested</li></ul></li></ul><ol><li>First</li></ol><blockquote><p>Quote</p></blockquote><hr>`;
    expect(htmlToMarkdown(html)).toBe("## About\n\nBe **great** at *this*\n\n- One\n- Nested\n\n1. First\n\n> Quote");
  });

  it("drops unsafe content and unsafe links", () => {
    const html = `<script>alert(1)</script><img src="x" onerror="alert(1)"><p><a href="javascript:alert(1)">Bad</a> <a href="https://a.example/x">Site</a> <a href="https://a.example">[x]</a></p>`;
    expect(htmlToMarkdown(html)).toBe("Bad [Site](https://a.example/x) [x]");
  });

  it("handles line breaks, loose text, empty emphasis, and empty input", () => {
    expect(htmlToMarkdown("Loose<br>text<div>Inner</div>")).toBe("Loose text\n\nInner");
    expect(htmlToMarkdown("<b> </b><i>word</i>")).toBe("*word*");
    expect(htmlToMarkdown("<h1> </h1><ul></ul><ul><li> </li></ul>")).toBe("");
    expect(htmlToMarkdown(null)).toBe("");
  });
});
