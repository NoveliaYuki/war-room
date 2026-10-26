import { beforeEach, describe, expect, it, vi } from 'vitest';

describe('early theme selection', () => {
  beforeEach(() => {
    vi.resetModules();
    delete document.documentElement.dataset.theme;
  });

  it.each(['dark', 'light'])('applies a saved %s theme before paint', async (theme) => {
    vi.spyOn(window.localStorage, 'getItem').mockReturnValue(theme);
    await import('../../public/js/theme.js');
    expect(document.documentElement.dataset.theme).toBe(theme);
  });

  it('leaves system selection in control for an invalid saved value', async () => {
    vi.spyOn(window.localStorage, 'getItem').mockReturnValue('system');
    await import('../../public/js/theme.js');
    expect(document.documentElement.dataset.theme).toBeUndefined();
  });

  it('leaves system selection in control when storage is unavailable', async () => {
    vi.spyOn(window.localStorage, 'getItem').mockImplementation(() => {
      throw new Error('storage unavailable');
    });
    await import('../../public/js/theme.js');
    expect(document.documentElement.dataset.theme).toBeUndefined();
  });
});
