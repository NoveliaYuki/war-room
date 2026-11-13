import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import en from '../../public/js/locales/en.js';
import es from '../../public/js/locales/es.js';
import ca from '../../public/js/locales/ca.js';

const frontendRoot = resolve(import.meta.dirname, '../..');
// Stylesheets and scripts are dropped so parsing never tries to fetch them.
const indexHtml = readFileSync(resolve(frontendRoot, 'index.html'), 'utf8').replace(/<link\b[^>]*>|<script\b[^>]*><\/script>/g, '');
const placeholderSlots = (entry) => [...new Set((typeof entry === 'string' ? [entry] : Object.values(entry)).join(' ').match(/\{\w+\}/g) || [])].sort();

/** Imports a fresh i18n module after setting saved and browser language preferences. */
async function loadI18n({ saved = null, languages = ['en-US'], language = languages[0] } = {}) {
  vi.resetModules();
  window.localStorage.clear();
  if (saved !== null) window.localStorage.setItem('war-room.language', saved);
  vi.spyOn(window.navigator, 'languages', 'get').mockReturnValue(languages);
  vi.spyOn(window.navigator, 'language', 'get').mockReturnValue(language);
  return import('../../public/js/i18n.js');
}

beforeEach(() => {
  document.head.innerHTML = '';
  document.body.innerHTML = '';
  document.documentElement.lang = '';
  delete document.documentElement.dataset.i18nPending;
});

afterEach(() => {
  vi.restoreAllMocks();
  window.localStorage.clear();
});

describe('language resolution', () => {
  it.each([
    ['ca-ES', 'ca'],
    ['es_MX', 'es'],
    ['EN', 'en'],
    ['fr-FR', null],
    ['', null],
    [undefined, null],
  ])('matches %s to %s', async (tag, expected) => {
    const { matchLanguage } = await loadI18n();
    expect(matchLanguage(tag)).toBe(expected);
  });

  it('prefers an explicit choice over the browser language', async () => {
    const { resolveLanguage } = await loadI18n();
    expect(resolveLanguage('ca', ['es-ES'])).toBe('ca');
  });

  it('uses the first supported browser language when nothing is saved', async () => {
    const { resolveLanguage } = await loadI18n();
    expect(resolveLanguage(null, ['fr-FR', 'ca-ES', 'es-ES'])).toBe('ca');
  });

  it('falls back to English when no browser language is supported', async () => {
    const { resolveLanguage } = await loadI18n();
    expect(resolveLanguage('xx', ['fr-FR', 'de'])).toBe('en');
    expect(resolveLanguage(null)).toBe('en');
  });

  it('starts in the browser language without saving it', async () => {
    const { getLanguage } = await loadI18n({ languages: ['es-ES', 'en'] });
    expect(getLanguage()).toBe('es');
    expect(window.localStorage.getItem('war-room.language')).toBeNull();
  });

  it('starts in a saved language even when the browser prefers another', async () => {
    const { getLanguage } = await loadI18n({ saved: 'ca', languages: ['es-ES'] });
    expect(getLanguage()).toBe('ca');
  });

  it('reads navigator.language when the language list is empty', async () => {
    const { getLanguage } = await loadI18n({ languages: [], language: 'ca' });
    expect(getLanguage()).toBe('ca');
  });

  it('still detects the browser language when storage is blocked', async () => {
    vi.resetModules();
    vi.spyOn(window.Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('blocked'); });
    vi.spyOn(window.navigator, 'languages', 'get').mockReturnValue(['es']);
    const { getLanguage } = await import('../../public/js/i18n.js');
    expect(getLanguage()).toBe('es');
  });
});

describe('message lookup', () => {
  it('fills named placeholders and leaves unknown ones visible', async () => {
    const { t } = await loadI18n();
    expect(t('card.currentStep', { index: 2, total: 4, title: 'Tech' })).toBe('Step 2/4: Tech');
    expect(t('card.currentStep', { index: 2 })).toBe('Step 2/{total}: {title}');
  });

  it('selects plural forms from the count', async () => {
    const { t, setLanguage } = await loadI18n();
    expect(t('schedule.meetingCount', { count: 1 })).toBe('1 meeting');
    expect(t('schedule.meetingCount', { count: 3 })).toBe('3 meetings');
    setLanguage('es', { persist: false });
    expect(t('schedule.meetingCount', { count: 1 })).toBe('1 reunión');
    expect(t('schedule.meetingCount', { count: 0 })).toBe('0 reuniones');
  });

  it('uses the other form when a language reports a category the message lacks', async () => {
    const { t } = await loadI18n({ languages: ['es-ES'] });
    expect(t('schedule.meetingCount', { count: 1000000 })).toBe('1000000 reuniones');
  });

  it('returns the key for an unknown message', async () => {
    const { t } = await loadI18n();
    expect(t('missing.key')).toBe('missing.key');
  });

  it('returns translated text for every supported language', async () => {
    const { t, setLanguage } = await loadI18n();
    setLanguage('ca', { persist: false });
    expect(t('status.rejected')).toBe('Rebutjat');
    setLanguage('es', { persist: false });
    expect(t('status.rejected')).toBe('Rechazado');
  });

  it('keeps a browser regional variant for Intl formatting', async () => {
    const { getLocale, setLanguage } = await loadI18n({ languages: ['en-GB', 'es-MX'] });
    expect(getLocale()).toBe('en-GB');
    setLanguage('es', { persist: false });
    expect(getLocale()).toBe('es-MX');
    setLanguage('ca', { persist: false });
    expect(getLocale()).toBe('ca');
  });
});

describe('document translation', () => {
  it('translates keyed text, attributes, and parameters', async () => {
    const { applyTranslations, setLanguage } = await loadI18n();
    document.body.innerHTML = `
      <span id="text" data-i18n="filters.title">Filters</span>
      <input id="input" data-i18n-placeholder="search.placeholder" data-i18n-aria-label="search.label" />
      <button id="button" data-i18n-title="card.openDetails"></button>
      <span id="params" data-i18n="filters.statusChip" data-i18n-params='{"statuses":"X"}'></span>
      <span id="broken" data-i18n="filters.statusChip" data-i18n-params="{"></span>`;
    setLanguage('ca', { persist: false });
    applyTranslations(document);
    expect(document.querySelector('#text').textContent).toBe('Filtres');
    expect(document.querySelector('#input').placeholder).toBe('Cerca ofertes…');
    expect(document.querySelector('#input').getAttribute('aria-label')).toBe('Cerca processos de selecció');
    expect(document.querySelector('#button').title).toBe('Obre els detalls');
    expect(document.querySelector('#params').textContent).toBe('Estat: X');
    expect(document.querySelector('#broken').textContent).toBe('Estat: {statuses}');
  });

  it('keeps text set from a key current across language changes', async () => {
    const { setLanguage, setTranslatedAttribute, setTranslatedText } = await loadI18n();
    document.body.innerHTML = '<span id="label"></span><button id="trigger"></button>';
    const label = document.querySelector('#label');
    const trigger = document.querySelector('#trigger');
    setTranslatedText(label, 'searchPeriods.trigger', { period: '2026' });
    setTranslatedAttribute(trigger, 'aria-label', 'filters.triggerActive', { count: 2 });
    expect(label.textContent).toBe('Search period: 2026');
    setLanguage('es', { persist: false });
    expect(label.textContent).toBe('Periodo de búsqueda: 2026');
    expect(trigger.getAttribute('aria-label')).toBe('Filtros; 2 filtros activos');
    setTranslatedText(label, 'filters.title');
    expect(label.dataset.i18nParams).toBeUndefined();
  });

  it('switches language, saves the choice, and notifies listeners', async () => {
    const { getLanguage, setLanguage, LANGUAGE_CHANGE_EVENT } = await loadI18n();
    document.body.innerHTML = '<p data-i18n="account.signOut">Sign out</p>';
    const listener = vi.fn();
    window.addEventListener(LANGUAGE_CHANGE_EVENT, listener);
    expect(setLanguage('es')).toBe(true);
    window.removeEventListener(LANGUAGE_CHANGE_EVENT, listener);
    expect(getLanguage()).toBe('es');
    expect(document.documentElement.lang).toBe('es');
    expect(document.querySelector('p').textContent).toBe('Cerrar sesión');
    expect(window.localStorage.getItem('war-room.language')).toBe('es');
    expect(listener).toHaveBeenCalledTimes(1);
    expect(listener.mock.calls[0][0].detail).toEqual({ language: 'es' });
  });

  it('saves a re-selected current language without re-rendering', async () => {
    const { setLanguage, LANGUAGE_CHANGE_EVENT } = await loadI18n({ languages: ['ca'] });
    const listener = vi.fn();
    window.addEventListener(LANGUAGE_CHANGE_EVENT, listener);
    expect(setLanguage('ca')).toBe(false);
    window.removeEventListener(LANGUAGE_CHANGE_EVENT, listener);
    expect(window.localStorage.getItem('war-room.language')).toBe('ca');
    expect(listener).not.toHaveBeenCalled();
  });

  it('ignores unsupported languages', async () => {
    const { getLanguage, setLanguage } = await loadI18n();
    expect(setLanguage('fr')).toBe(false);
    expect(getLanguage()).toBe('en');
    expect(window.localStorage.getItem('war-room.language')).toBeNull();
  });

  it('applies a choice for the page when storage is blocked', async () => {
    const { getLanguage, setLanguage } = await loadI18n();
    vi.spyOn(window.Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked'); });
    expect(setLanguage('ca')).toBe(true);
    expect(getLanguage()).toBe('ca');
  });

  it('translates the page and reveals it on initialization', async () => {
    const { initializeDocumentLanguage } = await loadI18n({ languages: ['es'] });
    document.documentElement.dataset.i18nPending = '';
    document.body.innerHTML = '<a data-i18n="app.skipToContent">Skip to main content</a>';
    initializeDocumentLanguage();
    expect(document.documentElement.lang).toBe('es');
    expect(document.documentElement.dataset.i18nPending).toBeUndefined();
    expect(document.querySelector('a').textContent).toBe('Saltar al contenido principal');
  });
});

describe('dictionaries', () => {
  it.each([['es', es], ['ca', ca]])('%s defines exactly the English keys', (_, dictionary) => {
    expect(Object.keys(dictionary).sort()).toEqual(Object.keys(en).sort());
  });

  it.each([['es', es], ['ca', ca]])('%s keeps every placeholder and plural shape', (_, dictionary) => {
    for (const [key, entry] of Object.entries(en)) {
      expect(typeof dictionary[key], key).toBe(typeof entry);
      expect(placeholderSlots(dictionary[key]), key).toEqual(placeholderSlots(entry));
      if (typeof entry === 'object') expect(Object.keys(dictionary[key]).sort(), key).toEqual(Object.keys(entry).sort());
    }
  });

  // Reviewed entries that read the same as English: shared words, brand names, and format-only strings.
  // An entry copied from English without translating fails here; add one only after checking it in that language.
  const SAME_AS_ENGLISH = {
    es: ['csv.no', 'cv.version', 'detail.fileSizeKb', 'employment.b2b', 'jobImport.urlPlaceholder', 'markdown.source', 'referral.toggleLabel', 'sort.short.manual', 'stageType.cultural'],
    ca: ['csv.no', 'cv.version', 'detail.fileSizeKb', 'employment.b2b', 'interviewer.notes', 'jobImport.urlPlaceholder', 'markdown.source', 'referral.toggleLabel', 'sort.short.manual', 'stageType.cultural'],
  };

  it.each([['es', es], ['ca', ca]])('%s translates every message it does not share with English', (language, dictionary) => {
    const same = Object.keys(en).filter((key) => JSON.stringify(dictionary[key]) === JSON.stringify(en[key]));
    expect(same.sort()).toEqual([...SAME_AS_ENGLISH[language]].sort());
  });

  it('keeps HTML out of messages', () => {
    for (const dictionary of [en, es, ca]) {
      for (const entry of Object.values(dictionary)) {
        const text = typeof entry === 'string' ? entry : Object.values(entry).join(' ');
        expect(text).not.toMatch(/<[a-z/]/i);
      }
    }
  });

  it('defines every key the static page uses on elements without child elements', () => {
    const page = new DOMParser().parseFromString(indexHtml, 'text/html');
    const keyed = [...page.querySelectorAll('[data-i18n], [data-i18n-aria-label], [data-i18n-title], [data-i18n-placeholder]')];
    expect(keyed.length).toBeGreaterThan(50);
    for (const element of keyed) {
      for (const attribute of ['data-i18n', 'data-i18n-aria-label', 'data-i18n-title', 'data-i18n-placeholder']) {
        const key = element.getAttribute(attribute);
        if (key) expect(en, key).toHaveProperty([key]);
      }
      if (element.hasAttribute('data-i18n')) expect(element.children.length, element.outerHTML).toBe(0);
    }
  });
});

describe('pre-paint language script', () => {
  it.each([
    [null, ['es-ES', 'en'], 'es', true],
    ['ca', ['es-ES'], 'ca', true],
    [null, ['fr-FR', 'de-DE'], 'en', false],
    ['en', ['ca-ES'], 'en', false],
    ['fr', ['ca'], 'ca', true],
  ])('saved %s with browser %j resolves to %s', async (saved, languages, expected, pending) => {
    const { resolveLanguage } = await loadI18n({ saved, languages });
    await import('../../public/js/language.js');
    expect(document.documentElement.lang).toBe(expected);
    expect(resolveLanguage(saved, languages)).toBe(expected);
    expect('i18nPending' in document.documentElement.dataset).toBe(pending);
  });

  it('detects the browser language when storage is blocked', async () => {
    vi.resetModules();
    vi.spyOn(window.Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('blocked'); });
    vi.spyOn(window.navigator, 'languages', 'get').mockReturnValue(undefined);
    vi.spyOn(window.navigator, 'language', 'get').mockReturnValue('ca-ES');
    await import('../../public/js/language.js');
    expect(document.documentElement.lang).toBe('ca');
  });
});
