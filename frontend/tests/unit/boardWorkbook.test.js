import { afterEach, describe, expect, it, vi } from 'vitest';
import { LANGUAGES, setLanguage } from '../../public/js/i18n.js';
import { boardSheet, boardWorkbookFilename, buildBoardWorkbook, loadJobStages, questionsSheet, XLSX_MIME } from '../../public/js/utils/boardWorkbook.js';
import { dateSerial } from '../../public/js/utils/xlsx.js';
import { readZip } from './zipReader.js';

afterEach(() => {
  setLanguage('en', { persist: false });
  vi.useRealTimers();
});

/** Returns Unix seconds for a local calendar date at noon, so the test does not depend on the time zone. */
function localNoon(year, month, day) {
  return Math.floor(new Date(year, month - 1, day, 12).getTime() / 1000);
}

const fullJob = {
  id: 'job-1',
  company_name: 'Acme, Inc.',
  position_title: 'Senior "Platform" Engineer',
  company_overview: '## About\nWe build **fast** services.\n\n- Go\n- [Docs](https://example.com/docs)',
  description: 'Original recruiter message',
  status: 'ongoing',
  current_stage_title: 'Technical',
  current_stage_title_is_type: true,
  work_arrangement: 'on_site',
  employment_type: 'permanent_b2b',
  salary_min: 60000,
  salary_max: 75000,
  salary_currency: 'EUR',
  expected_salary: '70k + equity',
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

const values = (row) => row.map((cell) => cell?.value);

/** Returns a board data row as a map from column header (without its number) to value. */
function boardRow(sheet, index = 2) {
  const headers = sheet.rows[1].map((cell) => cell.value.replace(/^\d+\.\d+ /, ''));
  return Object.fromEntries(headers.map((header, column) => [header, sheet.rows[index][column].value]));
}

describe('boardSheet', () => {
  it('writes numbered group headers merged over their columns, numbered column headers, and a hidden process ID', () => {
    const sheet = boardSheet([fullJob]);
    expect(sheet.name).toBe('Board');
    expect(sheet.columns).toHaveLength(27);
    expect(sheet.columns.map((column) => Boolean(column.hidden)).lastIndexOf(false)).toBe(25);
    expect(sheet.columns[26].hidden).toBe(true);
    expect(values(sheet.rows[0]).filter(Boolean)).toEqual(['1 Job specification', '2 Process', '3 Application', '4 Recruiter']);
    expect(sheet.rows[0].every((cell) => cell.kind === 'group')).toBe(true);
    expect(sheet.merges).toEqual(['A1:K1', 'L1:Q1', 'R1:V1', 'W1:Z1']);
    expect(values(sheet.rows[1])).toEqual([
      '1.1 Company', '1.2 Role', '1.3 Description', '1.4 Employment type', '1.5 Work arrangement', '1.6 Published salary – minimum',
      '1.7 Published salary – maximum', '1.8 Currency', '1.9 Your expected salary', '1.10 Tech stack', '1.11 Job posting link',
      '2.1 Status', '2.2 Current stage', '2.3 Search period', '2.4 Date added', '2.5 Status changed', '2.6 Last updated',
      '3.1 CV version', '3.2 CV file', '3.3 CV sent date', '3.4 Application link', '3.5 Referral',
      '4.1 Name', '4.2 Agency', '4.3 Contact details', '4.4 First contact', 'Process ID',
    ]);
    expect(sheet.rows[1].every((cell) => cell.kind === 'header')).toBe(true);
    expect(sheet.frozenRows).toBe(2);
    expect(sheet.filterRow).toBe(2);
  });

  it('writes the role overview as plain text, salaries as numbers, and dates as serial numbers', () => {
    const sheet = boardSheet([fullJob], [{ id: 'period-1', name: 'Autumn 2026' }]);
    expect(boardRow(sheet)).toEqual({
      Company: 'Acme, Inc.', Role: 'Senior "Platform" Engineer',
      Description: 'About\nWe build fast services.\n\n• Go\n• Docs (https://example.com/docs)',
      'Employment type': 'Permanent / B2B', 'Work arrangement': 'On-site',
      'Published salary – minimum': 60000, 'Published salary – maximum': 75000, Currency: 'EUR',
      'Your expected salary': '70k + equity', 'Tech stack': 'Go; PostgreSQL', 'Job posting link': 'https://example.com/jobs/1',
      Status: 'Ongoing', 'Current stage': 'Technical', 'Search period': 'Autumn 2026',
      'Date added': dateSerial(2026, 11, 1), 'Status changed': dateSerial(2026, 11, 5), 'Last updated': dateSerial(2026, 11, 6),
      'CV version': 3, 'CV file': 'cv-2026.pdf', 'CV sent date': dateSerial(2026, 11, 2), 'Application link': null, Referral: 'Yes',
      Name: 'Núria Puig', Agency: null, 'Contact details': 'nuria@example.com', 'First contact': null, 'Process ID': 'job-1',
    });
    const kinds = Object.fromEntries(sheet.rows[1].map((cell, column) => [cell.value, sheet.rows[2][column].kind]));
    expect(kinds['1.3 Description']).toBe('wrap');
    expect(kinds['1.6 Published salary – minimum']).toBe('number');
    expect(kinds['2.4 Date added']).toBe('date');
    expect(kinds['3.3 CV sent date']).toBe('date');
  });

  it('keeps custom stage titles, translates default ones, and leaves missing values empty', () => {
    setLanguage('es', { persist: false });
    expect(boardRow(boardSheet([{ ...fullJob, current_stage_title: 'Technical', current_stage_title_is_type: false }]))['Fase actual']).toBe('Technical');
    expect(boardRow(boardSheet([{ ...fullJob, current_stage_title: 'Recruiter Screen', current_stage_title_is_type: false }]))['Fase actual']).not.toBe('Recruiter Screen');
    const sparse = boardRow(boardSheet([{ company_name: 'unknown', position_title: 'Engineer', status: 'waiting', created_at: 0, application_sent_date: 'someday' }]));
    expect(sparse.Empresa).toBe('Empresa desconocida');
    expect(sparse.Descripción).toBe('');
    expect(sparse['Fase actual']).toBe('');
    expect(sparse['Fecha de alta']).toBeNull();
    expect(sparse['Fecha de envío del CV']).toBe('someday');
    expect(sparse['Stack tecnológico']).toBe('');
    expect(sparse.Recomendación).toBe('No');
  });

  it('translates headers, sheet names, and stored values but keeps user text as typed', () => {
    setLanguage('es', { persist: false });
    let sheet = boardSheet([fullJob]);
    expect(sheet.name).toBe('Tablero');
    expect(values(sheet.rows[0]).filter(Boolean)).toEqual(['1 Especificación del puesto', '2 Proceso', '3 Candidatura', '4 Reclutador']);
    expect(values(sheet.rows[1]).slice(0, 3)).toEqual(['1.1 Empresa', '1.2 Puesto', '1.3 Descripción']);
    expect(boardRow(sheet)).toMatchObject({ Estado: 'En curso', 'Fase actual': 'Técnica', 'Modalidad de trabajo': 'Presencial', Recomendación: 'Sí', Puesto: 'Senior "Platform" Engineer' });

    setLanguage('ca', { persist: false });
    sheet = boardSheet([{ ...fullJob, is_referral: false, status: 'accepted' }]);
    expect(sheet.name).toBe('Tauler');
    expect(values(sheet.rows[1])).toContain('2.4 Data d\'alta');
    expect(values(sheet.rows[1])).toContain('1.6 Salari publicat – mínim');
    expect(boardRow(sheet)).toMatchObject({ Estat: 'Acceptat', 'Fase actual': 'Tècnica', Recomanació: 'No', Nom: 'Núria Puig' });
  });

  it('keeps a formula-like value as text', () => {
    const company = '=HYPERLINK("https://evil.example","x")';
    expect(boardRow(boardSheet([{ ...fullJob, company_name: company }])).Company).toBe(company);
  });

  it('writes only the headers for an empty board', () => {
    expect(boardSheet([]).rows).toHaveLength(2);
  });
});

const stagesByJob = new Map([
  ['job-1', [
    { id: 'stage-b', order_index: 1, stage_type: 'Technical', custom_title: null, questions: [{ id: 'q-3', order_index: 0, question: 'Design a cache', answer_notes: '', is_asked: false }] },
    { id: 'stage-a', order_index: 0, stage_type: 'HR', custom_title: 'Recruiter Screen', questions: [
      { id: 'q-2', order_index: 1, question: 'Salary?', answer_notes: 'Say 70k', is_asked: true },
      { id: 'q-1', order_index: 0, question: 'Why us?', answer_notes: 'Mission\nand team', is_asked: false },
    ] },
    { id: 'stage-c', order_index: 2, stage_type: 'Cultural', custom_title: 'Meet the CTO' },
  ]],
  ['job-2', [{ id: 'stage-d', stage_type: 'HR', custom_title: null, questions: [{ id: 'q-4', question: '=1+1', answer_notes: '', is_asked: true }] }]],
]);
const jobs = [fullJob, { id: 'job-2', company_name: 'unknown', position_title: 'Analyst' }, { id: 'job-3', company_name: 'Empty', position_title: 'None' }];

describe('questionsSheet', () => {
  it('lists every question in job, stage, then question order, with hidden IDs', () => {
    const sheet = questionsSheet(jobs, stagesByJob);
    expect(sheet.name).toBe('Interview questions');
    expect(values(sheet.rows[0])).toEqual(['Company', 'Role', 'Stage', 'Question', 'Answer notes', 'Asked', 'Process ID', 'Stage ID', 'Question ID']);
    expect(sheet.columns.map((column) => Boolean(column.hidden))).toEqual([false, false, false, false, false, false, true, true, true]);
    expect(sheet.rows.slice(1).map(values)).toEqual([
      ['Acme, Inc.', 'Senior "Platform" Engineer', 'Recruiter Screen', 'Why us?', 'Mission\nand team', 'No', 'job-1', 'stage-a', 'q-1'],
      ['Acme, Inc.', 'Senior "Platform" Engineer', 'Recruiter Screen', 'Salary?', 'Say 70k', 'Yes', 'job-1', 'stage-a', 'q-2'],
      ['Acme, Inc.', 'Senior "Platform" Engineer', 'Technical', 'Design a cache', '', 'No', 'job-1', 'stage-b', 'q-3'],
      ['Unknown Company', 'Analyst', 'HR', '=1+1', '', 'Yes', 'job-2', 'stage-d', 'q-4'],
    ]);
    expect(sheet.rows[1].map((cell) => cell.kind).slice(3, 5)).toEqual(['wrap', 'wrap']);
    expect(sheet.frozenRows).toBe(1);
    expect(sheet.filterRow).toBe(1);
  });

  it('translates headers, default stage titles, stage types, and Yes/No', () => {
    setLanguage('es', { persist: false });
    let sheet = questionsSheet(jobs, stagesByJob);
    expect(sheet.name).toBe('Preguntas de entrevista');
    expect(values(sheet.rows[0]).slice(0, 6)).toEqual(['Empresa', 'Puesto', 'Fase', 'Pregunta', 'Notas de respuesta', 'Formulada']);
    expect(values(sheet.rows[1]).slice(2, 6)).toEqual(['Primer contacto', 'Why us?', 'Mission\nand team', 'No']);
    expect(values(sheet.rows[3])[2]).toBe('Técnica');
    expect(values(sheet.rows[2])[5]).toBe('Sí');

    setLanguage('ca', { persist: false });
    sheet = questionsSheet(jobs, stagesByJob);
    expect(sheet.name).toBe("Preguntes d'entrevista");
    expect(values(sheet.rows[0]).slice(6)).toEqual(['ID del procés', 'ID de la fase', 'ID de la pregunta']);
    expect(values(sheet.rows[4])[0]).toBe('Empresa desconeguda');
  });

  it('writes only the header when there are no questions', () => {
    expect(questionsSheet(jobs, new Map()).rows).toHaveLength(1);
  });
});

describe('sheet names', () => {
  it.each(LANGUAGES.map(({ code }) => code))('are valid and distinct in %s', (language) => {
    setLanguage(language, { persist: false });
    const names = [boardSheet([]).name, questionsSheet([], new Map()).name];
    for (const name of names) {
      expect(name.length).toBeLessThanOrEqual(31);
      expect(name).not.toMatch(/[[\]:*?/\\]|^'|'$/);
    }
    expect(new Set(names).size).toBe(2);
  });
});

describe('loadJobStages', () => {
  it('revokes browser attachment links it does not use', async () => {
    const revoke = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {});
    const loadJob = async (id) => ({ id, stages: [], attachments: [{ download_url: 'blob:demo-1' }, { download_url: '/api/attachments/a/download' }, {}] });
    await loadJobStages([{ id: 'job-1' }], loadJob);
    expect(revoke).toHaveBeenCalledTimes(1);
    expect(revoke).toHaveBeenCalledWith('blob:demo-1');
    revoke.mockRestore();
  });

  it('loads every job with at most the given number of requests at once', async () => {
    let active = 0;
    let peak = 0;
    const loadJob = vi.fn(async (id) => {
      active += 1;
      peak = Math.max(peak, active);
      await new Promise((resolve) => setTimeout(resolve, id === 'job-0' ? 5 : 1));
      active -= 1;
      return id === 'job-9' ? { id } : { id, stages: [{ id: `${id}-stage` }] };
    });
    const many = Array.from({ length: 10 }, (_, index) => ({ id: `job-${index}` }));
    const stages = await loadJobStages(many, loadJob, 3);
    expect(peak).toBe(3);
    expect(loadJob).toHaveBeenCalledTimes(10);
    expect(stages.get('job-0')).toEqual([{ id: 'job-0-stage' }]);
    expect(stages.get('job-9')).toEqual([]);
    expect(await loadJobStages([], loadJob)).toEqual(new Map());
  });

  it('rejects when a job fails to load', async () => {
    await expect(loadJobStages([{ id: 'x' }], () => Promise.reject(new Error('gone')))).rejects.toThrow('gone');
  });
});

describe('buildBoardWorkbook', () => {
  it('builds an .xlsx package with the board and question sheets', () => {
    const { files } = readZip(buildBoardWorkbook([{ ...fullJob, company_name: '=HYPERLINK("https://evil.example","x")' }], [], stagesByJob, new Date(2026, 10, 16)));
    const workbook = new DOMParser().parseFromString(files.get('xl/workbook.xml'), 'application/xml');
    expect([...workbook.querySelectorAll('sheet')].map((sheet) => sheet.getAttribute('name'))).toEqual(['Board', 'Interview questions']);
    const board = files.get('xl/worksheets/sheet1.xml');
    expect(board).toContain('<mergeCell ref="A1:K1"/>');
    expect(board).toContain('<autoFilter ref="A2:AA3"/>');
    expect(board).toContain('<col min="27" max="27" width="38" customWidth="1" hidden="1"/>');
    expect(board).toContain('<c r="AA3" s="1" t="inlineStr"><is><t xml:space="preserve">job-1</t></is></c>');
    expect(board).toContain('<c r="A3" s="1" t="inlineStr"><is><t xml:space="preserve">=HYPERLINK(&quot;https://evil.example&quot;,&quot;x&quot;)</t></is></c>');
    expect(board).not.toContain('<f>');
    expect(board).toContain(`<c r="F3" s="3"><v>60000</v></c>`);
    expect(board).toContain(`<c r="T3" s="4"><v>${dateSerial(2026, 11, 2)}</v></c>`);
    const questions = files.get('xl/worksheets/sheet2.xml');
    expect(questions).toContain('<col min="7" max="7" width="38" customWidth="1" hidden="1"/>');
    expect(questions).toContain('<autoFilter ref="A1:I4"/>');
    expect(XLSX_MIME).toBe('application/vnd.openxmlformats-officedocument.spreadsheetml.sheet');
  });
});

describe('boardWorkbookFilename', () => {
  it('names the file after the local date', () => {
    expect(boardWorkbookFilename(new Date(2026, 0, 5, 23, 30))).toBe('war-room-board-2026-01-05.xlsx');
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 10, 13, 9));
    expect(boardWorkbookFilename()).toBe('war-room-board-2026-11-13.xlsx');
  });
});
