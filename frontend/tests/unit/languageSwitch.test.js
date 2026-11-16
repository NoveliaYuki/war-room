import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

beforeEach(() => {
  vi.resetModules();
  window.localStorage.clear();
  vi.spyOn(window.navigator, 'languages', 'get').mockReturnValue(['en-US']);
  document.body.innerHTML = `
    <div id="menu" data-language-switch="menu"></div>
    <div id="buttons" data-language-switch="buttons"></div>
    <span data-i18n="language.label">Language</span>`;
});

afterEach(() => {
  vi.restoreAllMocks();
  window.localStorage.clear();
});

describe('language selectors', () => {
  it('lists each language in its own language with the active one marked', async () => {
    const { bindLanguageSwitches } = await import('../../public/js/components/languageSwitch.js');
    bindLanguageSwitches();
    const menuOptions = [...document.querySelectorAll('#menu [data-language-option]')];
    expect(menuOptions.map((option) => [option.textContent, option.lang, option.getAttribute('role')])).toEqual([
      ['English', 'en', 'menuitemradio'],
      ['Español', 'es', 'menuitemradio'],
      ['Català', 'ca', 'menuitemradio'],
    ]);
    expect(menuOptions.map((option) => option.getAttribute('aria-checked'))).toEqual(['true', 'false', 'false']);
    const buttons = [...document.querySelectorAll('#buttons [data-language-option]')];
    expect(buttons.map((option) => option.getAttribute('aria-pressed'))).toEqual(['true', 'false', 'false']);
    expect(buttons.every((option) => !option.hasAttribute('role'))).toBe(true);
  });

  it('switches the page language and keeps every selector in sync', async () => {
    const { bindLanguageSwitches } = await import('../../public/js/components/languageSwitch.js');
    bindLanguageSwitches();
    document.querySelector('#buttons [data-language-option="ca"]').click();
    expect(document.querySelector('[data-i18n]').textContent).toBe('Idioma');
    expect(document.documentElement.lang).toBe('ca');
    expect(window.localStorage.getItem('war-room.language')).toBe('ca');
    expect(document.querySelector('#menu [data-language-option="ca"]').getAttribute('aria-checked')).toBe('true');
    expect(document.querySelector('#menu [data-language-option="en"]').getAttribute('aria-checked')).toBe('false');
    expect(document.querySelector('#buttons [data-language-option="ca"]').getAttribute('aria-pressed')).toBe('true');
  });

  it('ignores clicks between options', async () => {
    const { bindLanguageSwitches } = await import('../../public/js/components/languageSwitch.js');
    bindLanguageSwitches();
    document.querySelector('#menu').click();
    expect(window.localStorage.getItem('war-room.language')).toBeNull();
  });
});
