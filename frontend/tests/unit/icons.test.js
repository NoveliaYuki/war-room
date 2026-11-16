
import { describe, it, expect } from 'vitest';
import { icon } from '../../public/js/icons.js';

describe('icons', () => {
  it('returns SVG string for known icon', () => {
    expect(icon('plus')).toContain('<svg');
  });
  it('returns a safe fallback for unknown icon', () => {
    expect(icon('unknown_non_existent')).toContain('<svg');
    expect(icon('unknown_non_existent')).toContain('<circle');
  });
  it('supports custom sizes', () => {
    expect(icon('plus', 32)).toContain('width="32"');
    expect(icon('plus', 32)).toContain('height="32"');
  });

  it('renders every registered icon entry', () => {
    const registered = [
      'target', 'calendar', 'zap', 'sparkles', 'clock', 'phone', 'building', 'video', 'user', 'users',
      'externalLink', 'arrowUpRight', 'chat', 'euro', 'dollar', 'award', 'clipboard', 'fileText', 'download',
      'pin', 'trash', 'edit', 'check', 'chevronUp', 'chevronDown', 'plus', 'close', 'briefcase', 'gripLines',
      'compass', 'helpCircle',
    ];
    for (const name of registered) {
      const markup = icon(name, 20, 'custom-icon');
      expect(markup).toContain('<svg');
      expect(markup).toContain('width="20"');
      expect(markup).toContain('class="svg-icon custom-icon"');
    }
  });
});
