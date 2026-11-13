import { afterEach, describe, expect, it, vi } from 'vitest';
import { setLanguage } from '../../public/js/i18n.js';
import { boardCsvFilename, buildBoardCsv, csvCell } from '../../public/js/utils/boardCsv.js';
import { saveBlob } from '../../public/js/utils/download.js';

afterEach(() => {
  setLanguage('en', { persist: false });
  vi.restoreAllMocks();
  vi.useRealTimers();
});

/** Returns Unix seconds for a local calendar date at noon, so the test does not depend on the time zone. */
function localNoon(year, month, day) {
  return Math.floor(new Date(year, month - 1, day, 12).getTime() / 1000);
}

const fullJob = {
  company_name: 'Acme, Inc.',
  position_title: 'Senior "Platform" Engineer',
  status: 'ongoing',
  current_stage_title: 'Technical',
  current_stage_title_is_type: true,
  work_arrangement: 'on_site',
  employment_type: 'permanent_b2b',
  salary_min: 60000,
  salary_max: 75000,
  salary_currency: 'EUR',
  is_referral: true,
  recruiter_name: 'Núria Puig',
  recruiter_agency: null,
  recruiter_contact: 'nuria@example.com',
  technologies: [{ name: 'Go' }, { name: 'PostgreSQL' }],
  selected_cv_version: { version_number: 3, original_name: 'cv-2026.pdf' },
  application_sent_date: '2026-11-02',
  recruiter_first_contact_date: null,
  search_period_id: 'period-1',
  job_post_url: 'https://example.com/jobs/1',
  application_url: null,
  created_at: localNoon(2026, 11, 1),
  status_changed_at: localNoon(2026, 11, 5),
  updated_at: localNoon(2026, 11, 6),
};

/** Splits a CSV document without quoted line breaks into rows of raw cells. */
function rows(csv) {
  return csv.replace(/^﻿/, '').split('\r\n').filter(Boolean);
}

describe('csvCell', () => {
  it('leaves plain values unquoted and empties null values', () => {
    expect(csvCell('Acme')).toBe('Acme');
    expect(csvCell(75000)).toBe('75000');
    expect(csvCell(null)).toBe('');
    expect(csvCell(undefined)).toBe('');
  });

  it('quotes commas, quotes, and line breaks per RFC 4180', () => {
    expect(csvCell('Acme, Inc.')).toBe('"Acme, Inc."');
    expect(csvCell('Say "hi"')).toBe('"Say ""hi"""');
    expect(csvCell('line one\nline two')).toBe('"line one\nline two"');
    expect(csvCell('line one\r\nline two')).toBe('"line one\r\nline two"');
  });

  it('neutralizes cells a spreadsheet would run as formulas', () => {
    expect(csvCell('=HYPERLINK("https://evil.example","x")')).toBe('"\'=HYPERLINK(""https://evil.example"",""x"")"');
    expect(csvCell('+1+1')).toBe("'+1+1");
    expect(csvCell('-2')).toBe("'-2");
    expect(csvCell('@SUM(A1)')).toBe("'@SUM(A1)");
    expect(csvCell('\tcmd')).toBe("'\tcmd");
    expect(csvCell('\rcmd')).toBe('"\'\rcmd"');
    expect(csvCell('a=b')).toBe('a=b');
  });
});

describe('buildBoardCsv', () => {
  it('starts with a UTF-8 byte order mark and ends every row with CRLF', () => {
    const csv = buildBoardCsv([fullJob]);
    expect(csv.startsWith('﻿')).toBe(true);
    expect(csv.endsWith('\r\n')).toBe(true);
    expect(csv.replace(/\r\n/g, '')).not.toMatch(/[\r\n]/);
    expect(rows(csv)).toHaveLength(2);
  });

  it('writes English headers and translated values with ISO dates', () => {
    const [header, row] = rows(buildBoardCsv([fullJob], [{ id: 'period-1', name: 'Autumn 2026' }]));
    expect(header).toBe('Company,Role,Status,Current stage,Work arrangement,Employment type,Minimum salary,Maximum salary,Currency,Referral,Recruiter,Recruiting agency,Recruiter contact details,Tech stack,CV version,CV file,CV sent date,First recruiter contact,Search period,Job posting link,Application link,Date added,Status changed,Last updated');
    expect(row).toBe('"Acme, Inc.","Senior ""Platform"" Engineer",Ongoing,Technical,On-site,Permanent / B2B,60000,75000,EUR,Yes,Núria Puig,,nuria@example.com,Go; PostgreSQL,3,cv-2026.pdf,2026-11-02,,Autumn 2026,https://example.com/jobs/1,,2026-11-01,2026-11-05,2026-11-06');
  });

  it('translates headers and stored values but keeps user text as typed', () => {
    setLanguage('es', { persist: false });
    const [header, row] = rows(buildBoardCsv([fullJob]));
    expect(header.split(',').slice(0, 4)).toEqual(['Empresa', 'Puesto', 'Estado', 'Fase actual']);
    expect(row).toContain(',En curso,Técnica,Presencial,Indefinido / B2B,');
    expect(row).toContain('"Senior ""Platform"" Engineer"');
    expect(row).toContain(',Sí,Núria Puig,');

    setLanguage('ca', { persist: false });
    const [catalanHeader, catalanRow] = rows(buildBoardCsv([{ ...fullJob, is_referral: false, status: 'accepted' }]));
    expect(catalanHeader).toContain('Data d\'alta');
    expect(catalanRow).toContain(',Acceptat,Tècnica,Presencial,');
    expect(catalanRow).toContain(',No,Núria Puig,');
  });

  it('keeps custom stage titles, translates default ones, and leaves missing values empty', () => {
    setLanguage('es', { persist: false });
    const custom = rows(buildBoardCsv([{ ...fullJob, current_stage_title: 'Technical', current_stage_title_is_type: false }]))[1];
    expect(custom).toContain(',En curso,Technical,');
    const defaultTitle = rows(buildBoardCsv([{ ...fullJob, current_stage_title: 'Recruiter Screen', current_stage_title_is_type: false }]))[1];
    expect(defaultTitle).not.toContain('Recruiter Screen');

    const sparse = rows(buildBoardCsv([{ company_name: 'unknown', position_title: 'Engineer', status: 'waiting', created_at: 0 }]))[1];
    expect(sparse).toBe('Empresa desconocida,Engineer,En espera,,,,,,,No,,,,,,,,,,,,,,');
  });

  it('neutralizes formulas from imported postings', () => {
    const row = rows(buildBoardCsv([{ ...fullJob, company_name: '=cmd|"/c calc"!A1', position_title: '@Engineer' }]))[1];
    expect(row.startsWith('"\'=cmd|""/c calc""!A1",\'@Engineer,')).toBe(true);
  });

  it('writes a header only for an empty board', () => {
    expect(rows(buildBoardCsv([]))).toHaveLength(1);
  });
});

describe('boardCsvFilename', () => {
  it('names the file after the local date', () => {
    expect(boardCsvFilename(new Date(2026, 0, 5, 23, 30))).toBe('war-room-board-2026-01-05.csv');
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 10, 13, 9));
    expect(boardCsvFilename()).toBe('war-room-board-2026-11-13.csv');
  });
});

describe('saveBlob', () => {
  it('downloads through a temporary link and releases the object URL', () => {
    vi.useFakeTimers();
    const createObjectURL = vi.fn(() => 'blob:csv');
    const revokeObjectURL = vi.fn();
    vi.stubGlobal('URL', Object.assign(URL, { createObjectURL, revokeObjectURL }));
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function record() {
      expect(this.download).toBe('board.csv');
      expect(this.getAttribute('href')).toBe('blob:csv');
      expect(this.isConnected).toBe(true);
    });
    const blob = new Blob(['x']);
    saveBlob(blob, 'board.csv');
    expect(createObjectURL).toHaveBeenCalledWith(blob);
    expect(click).toHaveBeenCalledOnce();
    expect(document.querySelector('a[download]')).toBeNull();
    vi.advanceTimersByTime(1000);
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:csv');
  });
});
