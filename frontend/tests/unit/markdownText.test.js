import { describe, expect, it } from 'vitest';
import { markdownToText } from '../../public/js/utils/markdownText.js';

describe('markdownToText', () => {
  it('drops heading, emphasis, and code markers', () => {
    expect(markdownToText('## About **Acme**\nWe build _fast_ and *safe* `Go` services with __care__.'))
      .toBe('About Acme\nWe build fast and safe Go services with care.');
    expect(markdownToText('### Closed heading ###')).toBe('Closed heading');
    expect(markdownToText('**Bold _and italic_ text**')).toBe('Bold and italic text');
  });

  it('writes links as label and address', () => {
    expect(markdownToText('Apply [here](https://example.com/jobs?id=1) or <https://example.com/a_b_c>.'))
      .toBe('Apply here (https://example.com/jobs?id=1) or https://example.com/a_b_c.');
    expect(markdownToText('[https://example.com](https://example.com)')).toBe('https://example.com');
    expect(markdownToText('[**Docs**](https://example.com/docs)')).toBe('Docs (https://example.com/docs)');
  });

  it('keeps snake_case names, bare addresses with underscores, and lone asterisks', () => {
    expect(markdownToText('Use snake_case_name at https://example.com/my_page_here')).toBe('Use snake_case_name at https://example.com/my_page_here');
    expect(markdownToText('5 * 3 * 2 = 30')).toBe('5 * 3 * 2 = 30');
    expect(markdownToText('Escaped \\*stars\\* and \\_underscores\\_')).toBe('Escaped *stars* and _underscores_');
  });

  it('turns list markers into bullets, keeps numbers and indentation', () => {
    expect(markdownToText('- Go\n* Kubernetes\n+ PostgreSQL\n  - nested\n\t- tabbed\n1. First\n2) Second'))
      .toBe('• Go\n• Kubernetes\n• PostgreSQL\n  • nested\n  • tabbed\n1. First\n2) Second');
  });

  it('keeps line and paragraph breaks, and collapses blank runs', () => {
    expect(markdownToText('\r\n\r\nFirst line\r\nsecond line   \n\n\n\n> Quoted **text**\n\n---\n\n```js\ncode()\n```\n\nEnd\n\n'))
      .toBe('First line\nsecond line\n\nQuoted text\n\ncode()\n\nEnd');
  });

  it('returns an empty string for empty input', () => {
    expect(markdownToText(null)).toBe('');
    expect(markdownToText(undefined)).toBe('');
    expect(markdownToText('')).toBe('');
  });
});
