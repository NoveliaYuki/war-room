import { describe, expect, it } from "vitest";
import { renderMarkdown } from "../../public/js/utils/markdown.js";

describe("renderMarkdown", () => {
  it("renders common overview formatting", () => {
    expect(renderMarkdown("# Role\n\n**Security** with `Go`\n- Kubernetes\n- Vault\n1. Design\n2. Build"))
      .toBe('<h1>Role</h1><p><strong>Security</strong> with <code>Go</code></p><ul><li>Kubernetes</li><li>Vault</li></ul><ol><li>Design</li><li>Build</li></ol>');
  });

  it("renders blockquotes and both supported emphasis styles", () => {
    expect(renderMarkdown("> Callout\n\n*italic* _underlined_ __bold__"))
      .toBe("<blockquote><p>Callout</p></blockquote><p><em>italic</em> <em>underlined</em> <strong>bold</strong></p>");
  });

  it("recognizes bullet characters copied from job postings", () => {
    expect(renderMarkdown("• First requirement\n• Second requirement"))
      .toBe("<ul><li>First requirement</li><li>Second requirement</li></ul>");
  });

  it("normalizes Windows line endings and accepts empty input", () => {
    expect(renderMarkdown("# Role\r\n\r\nSummary")).toBe("<h1>Role</h1><p>Summary</p>");
    expect(renderMarkdown(null)).toBe("");
  });

  it("escapes HTML and excludes unsafe links", () => {
    const rendered = renderMarkdown('<img src=x onerror="alert(1)"> [bad](javascript:alert(1)) [good](https://example.com)');
    expect(rendered).toContain('&lt;img src=x onerror=&quot;alert(1)&quot;&gt;');
    expect(rendered).toContain('<a href="https://example.com" target="_blank" rel="noopener noreferrer">good</a>');
    expect(renderMarkdown("[bad](javascript:alert(1))")).not.toContain("href=");
  });
});
