
import { describe, it, expect } from 'vitest';
import { escapeHtml, escapeAttr, safeUrl } from '../../public/js/utils/sanitize.js';

describe('sanitize', () => {
  describe('escapeHtml', () => {
    it('escapes special characters', () => {
      expect(escapeHtml('<div class="x">&\'</div>')).toBe('&lt;div class=&quot;x&quot;&gt;&amp;&#39;&lt;/div&gt;');
    });
    it('handles normal strings', () => {
      expect(escapeHtml('hello world')).toBe('hello world');
    });
    it('handles empty strings', () => {
      expect(escapeHtml('')).toBe('');
    });
    it('handles null/undefined', () => {
      expect(escapeHtml(null)).toBe('');
      expect(escapeHtml(undefined)).toBe('');
    });
    it('handles long strings, unicode, emoji', () => {
      expect(escapeHtml('🌍 <b>hello</b>')).toBe('🌍 &lt;b&gt;hello&lt;/b&gt;');
    });
  });

  describe('escapeAttr', () => {
    it('escapes double quotes', () => {
      expect(escapeAttr('hello "world"')).toBe('hello &quot;world&quot;');
    });
    it('handles normal strings', () => {
      expect(escapeAttr('hello world')).toBe('hello world');
    });
    it('handles empty strings', () => {
      expect(escapeAttr('')).toBe('');
    });
    it('handles null/undefined', () => {
      expect(escapeAttr(null)).toBe('');
      expect(escapeAttr(undefined)).toBe('');
    });
  });

  describe('safeUrl', () => {
    it('allows HTTP, HTTPS, and relative links', () => {
      expect(safeUrl('https://example.com/role')).toBe('https://example.com/role');
      expect(safeUrl('http://example.com')).toBe('http://example.com');
      expect(safeUrl('/jobs/123')).toBe('/jobs/123');
    });

    it('rejects executable or malformed schemes', () => {
      expect(safeUrl('javascript:alert(1)')).toBeNull();
      expect(safeUrl('data:text/html,hello')).toBeNull();
      expect(safeUrl('')).toBeNull();
    });
  });
});
