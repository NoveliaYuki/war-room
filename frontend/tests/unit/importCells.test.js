import { afterEach, describe, expect, it } from 'vitest';
import { setLanguage } from '../../public/js/i18n.js';
import {
  amountOf, buildLabelIndex, CELL_READERS, foldText, isBlank, isoDateOf, labelReader, normalizeText, sameAsExported, textOf,
} from '../../public/js/utils/importCells.js';
import { dateSerial } from '../../public/js/utils/xlsx.js';

afterEach(() => setLanguage('en', { persist: false }));

const context = {
  labels: buildLabelIndex(),
  dateOffset: 0,
  technologies: new Map([['go', { id: 'tech-go' }], ['golang', { id: 'tech-go' }], ['postgresql', { id: 'tech-pg' }]]),
  periods: [{ id: 'p-1', name: 'Autumn 2026' }, { id: 'p-2', name: 'Twice' }, { id: 'p-3', name: 'twice' }],
  cvVersions: [{ id: 'cv-3', version_number: 3 }],
};

describe('cell text', () => {
  it('reads every cell type as text, normalized for comparison', () => {
    expect(textOf(null)).toBe('');
    expect(textOf(60000)).toBe('60000');
    expect(textOf(false)).toBe('false');
    expect(normalizeText('  line one  \r\nline\u0001 two\t\n')).toBe('line one\nline two');
    expect(isBlank(' \n ')).toBe(true);
    expect(isBlank(0)).toBe(false);
    expect(foldText('  On-Site\n Office ')).toBe('on-site office');
  });
});

describe('amountOf', () => {
  it.each([
    [60000, 60000], ['60000', 60000], ['60,000', 60000], ['60.000', 60000], ['1.234.567', 1234567], ['60 000 €', 60000],
    ['72,5', 72.5], ['85,000.40', 85000.4], ['72.5k', 72500], ['$90K', 90000], ['', null], [null, null],
  ])('reads %j as %j', (value, expected) => {
    expect(amountOf(value)).toBe(expected);
  });

  it.each(['about 60k', '.', 'k', '-5'])('does not read %j', (value) => {
    expect(amountOf(value)).toBeNaN();
  });
});

describe('isoDateOf', () => {
  it('reads date serial numbers and YYYY-MM-DD text', () => {
    expect(isoDateOf(dateSerial(2026, 11, 2))).toBe('2026-11-02');
    expect(isoDateOf(dateSerial(2026, 11, 2) + 0.75)).toBe('2026-11-02');
    expect(isoDateOf(dateSerial(2026, 11, 2) - 1462, 1462)).toBe('2026-11-02');
    expect(isoDateOf(' 2026-11-02 ')).toBe('2026-11-02');
    expect(isoDateOf('2026-11-02T00:00:00Z')).toBe('2026-11-02');
    expect(isoDateOf('')).toBe('');
  });

  it.each(['2026-02-30', '02/11/2026', 'soon', 0, 3000000])('does not read %j', (value) => {
    expect(isoDateOf(value)).toBeNull();
  });
});

describe('sameAsExported', () => {
  it('compares text, numbers, and dates the way each was written', () => {
    expect(sameAsExported('text', 'Acme ', 'Acme')).toBe(true);
    expect(sameAsExported('wrap', 'a\r\nb', 'a\nb')).toBe(true);
    expect(sameAsExported('text', 'Acme', 'Acme Corp')).toBe(false);
    expect(sameAsExported('number', '60,000', 60000)).toBe(true);
    expect(sameAsExported('number', null, undefined)).toBe(true);
    expect(sameAsExported('number', 'n/a', null)).toBe(false);
    expect(sameAsExported('date', '2026-11-02', dateSerial(2026, 11, 2))).toBe(true);
    expect(sameAsExported('date', dateSerial(2026, 11, 2) - 1462, dateSerial(2026, 11, 2), 1462)).toBe(true);
    expect(sameAsExported('date', 'someday', 'someday')).toBe(true);
    expect(sameAsExported('date', '2026-11-03', dateSerial(2026, 11, 2))).toBe(false);
  });
});

describe('labels', () => {
  it('reads labels in every supported language and stored values', () => {
    setLanguage('ca', { persist: false });
    const status = labelReader('status');
    expect(status('Approved', context)).toEqual({ value: 'accepted' });
    expect(status('aprobado', context)).toEqual({ value: 'accepted' });
    expect(status('Acceptat', context)).toEqual({ value: 'accepted' });
    expect(status('En curso', context)).toEqual({ value: 'ongoing' });
    expect(status('rebutjat', context)).toEqual({ value: 'rejected' });
    expect(status('WAITING', context)).toEqual({ value: 'waiting' });
    expect(status('', context)).toEqual({ problem: 'requiredValue', params: {} });
    expect(status('Pending', context)).toEqual({ problem: 'invalidValue', params: { value: 'Pending' } });
    expect(labelReader('workArrangement', 'unknown')('', context)).toEqual({ value: 'unknown' });
    expect(labelReader('workArrangement', 'unknown')('Presencial', context)).toEqual({ value: 'on_site' });
    expect(labelReader('employmentType', 'unknown')('Indefinido / B2B', context).value).toBe('permanent_b2b');
  });
});

describe('CELL_READERS', () => {
  it('reads text, required text, and currencies', () => {
    expect(CELL_READERS.text(' a\r\nb ', context)).toEqual({ value: 'a\nb' });
    expect(CELL_READERS.required(' ', context)).toEqual({ problem: 'requiredValue', params: {} });
    expect(CELL_READERS.required(' Engineer ', context)).toEqual({ value: 'Engineer' });
    expect(CELL_READERS.currency(' usd ', context)).toEqual({ value: 'USD' });
  });

  it('reads the undisclosed company in any language', () => {
    for (const name of ['', 'Unknown', 'Unknown Company', 'Empresa desconocida', 'empresa desconeguda']) {
      expect(CELL_READERS.company(name, context)).toEqual({ value: 'Unknown' });
    }
    expect(CELL_READERS.company(' Acme ', context)).toEqual({ value: 'Acme' });
  });

  it('reads amounts, dates, and Yes or No', () => {
    expect(CELL_READERS.amount('', context)).toEqual({ value: null });
    expect(CELL_READERS.amount('72,5k', context)).toEqual({ value: 72500 });
    expect(CELL_READERS.amount('-1', context).problem).toBe('invalidNumber');
    expect(CELL_READERS.amount(-1, context).problem).toBe('invalidNumber');
    expect(CELL_READERS.date('', context)).toEqual({ value: '' });
    expect(CELL_READERS.date('13/11/2026', context)).toEqual({ problem: 'invalidDate', params: { value: '13/11/2026' } });
    expect(CELL_READERS.date(dateSerial(2026, 1, 5), context)).toEqual({ value: '2026-01-05' });
    for (const [cell, value] of [[true, true], [false, false], ['', false], [1, true], [0, false], ['Sí', true], ['yes', true], ['NO', false], ['TRUE', true]]) {
      expect(CELL_READERS.yesNo(cell, context)).toEqual({ value });
    }
    expect(CELL_READERS.yesNo('maybe', context).problem).toBe('invalidValue');
  });

  it('reads a tech stack only when every name is in the catalog', () => {
    expect(CELL_READERS.technologies('Go; PostgreSQL;golang\n', context)).toEqual({ value: ['tech-go', 'tech-pg'] });
    expect(CELL_READERS.technologies('', context)).toEqual({ value: [] });
    expect(CELL_READERS.technologies('Go; Kubernetez; Rust', context)).toEqual({ problem: 'unknownTechnologies', params: { names: 'Kubernetez, Rust' } });
  });

  it('reads a search period by its unique name and a CV by its version number', () => {
    expect(CELL_READERS.period('', context)).toEqual({ value: '' });
    expect(CELL_READERS.period(' autumn 2026 ', context)).toEqual({ value: 'p-1' });
    expect(CELL_READERS.period('Twice', context).problem).toBe('unknownPeriod');
    expect(CELL_READERS.period('Spring', context).problem).toBe('unknownPeriod');
    expect(CELL_READERS.cvVersion('', context)).toEqual({ value: null });
    expect(CELL_READERS.cvVersion(3, context)).toEqual({ value: 'cv-3' });
    expect(CELL_READERS.cvVersion('4', context)).toEqual({ problem: 'unknownCvVersion', params: { value: '4' } });
  });
});
