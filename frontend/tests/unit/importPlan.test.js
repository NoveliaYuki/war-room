import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { LANGUAGES, setLanguage } from '../../public/js/i18n.js';
import { buildBoardWorkbook } from '../../public/js/utils/boardWorkbook.js';
import { applyImportPlan, buildImportPlan, planSize, recognizeSheets } from '../../public/js/utils/importPlan.js';
import { parseDelimited, readSpreadsheet, readWorkbook } from '../../public/js/utils/spreadsheetReader.js';
import { dateSerial } from '../../public/js/utils/xlsx.js';
import { fixture, noon, sourceFor } from './importFixture.js';

afterEach(() => setLanguage('en', { persist: false }));

const clone = (value) => structuredClone(value);

/** Exports the fixture in a language and reads the file back, as a user's spreadsheet app would. */
async function exportIn(language, data = fixture()) {
  setLanguage(language, { persist: false });
  const spreadsheet = await readWorkbook(buildBoardWorkbook(data.jobs, data.periods, data.stagesByJob));
  setLanguage('en', { persist: false });
  return spreadsheet;
}

const strip = (cell) => String(cell ?? '').replace(/^\d+\.\d+ /, '');

/** Sets the cell under a header (written without its number) in a sheet row. */
function setCell(sheet, row, header, value) {
  const headerRow = sheet.rows.findIndex((cells) => cells.some((cell) => strip(cell) === header));
  const column = sheet.rows[headerRow].findIndex((cell) => strip(cell) === header);
  expect(column, header).toBeGreaterThanOrEqual(0);
  sheet.rows[row] ||= [];
  sheet.rows[row][column] = value;
}

function targetMock(overrides = {}) {
  return {
    updateJob: vi.fn(async (id) => ({ id })),
    setJobCvVersion: vi.fn(async () => ({})),
    createJob: vi.fn(async (payload) => ({ id: 'job-new', ...payload })),
    updateQuestion: vi.fn(async (id) => ({ id })),
    createQuestion: vi.fn(async () => ({ id: 'q-new' })),
    ...overrides,
  };
}

describe('buildImportPlan with an unedited export', () => {
  const pairs = LANGUAGES.flatMap(({ code: exported }) => LANGUAGES.map(({ code: shown }) => [exported, shown]));

  it.each(pairs)('changes nothing when exported in %s and imported in %s', async (exported, shown) => {
    const spreadsheet = await exportIn(exported);
    setLanguage(shown, { persist: false });
    const plan = await buildImportPlan(spreadsheet, sourceFor(fixture()));
    expect(plan.warnings).toEqual([]);
    expect(planSize(plan)).toBe(0);
  });

  it('loads saved records in the file language', async () => {
    const source = sourceFor(fixture());
    await buildImportPlan(await exportIn('ca'), source);
    expect(source.getJobs).toHaveBeenCalledWith('all', '', { language: 'ca' });
    expect(source.getSearchPeriods).toHaveBeenCalledWith({ language: 'ca' });
    expect(source.getJob).toHaveBeenCalledWith('job-1', { language: 'ca' });
  });
});

// The fixture board exported in Spanish, then saved again by openpyxl and by LibreOffice Calc 7.4 (as .xlsx and as
// semicolon CSV shown as formatted), so imports are checked against files other apps write.
async function readFixture(name) {
  const bytes = new Uint8Array(readFileSync(resolve(import.meta.dirname, 'fixtures', name)));
  return readSpreadsheet(new File([bytes], name));
}

describe('buildImportPlan with files saved by other apps', () => {
  it.each(['board-openpyxl-es.xlsx', 'board-libreoffice-es.xlsx', 'board-libreoffice-es.csv', 'questions-libreoffice-es.csv'])('changes nothing after an unedited save as %s', async (name) => {
    const plan = await buildImportPlan(await readFixture(name), sourceFor(fixture()));
    expect(plan.warnings).toEqual([]);
    expect(planSize(plan)).toBe(0);
  });

  it('reads edits saved by openpyxl', async () => {
    const plan = await buildImportPlan(await readFixture('board-openpyxl-es-edited.xlsx'), sourceFor(fixture()));
    expect(plan.warnings).toEqual([]);
    expect(plan.processUpdates.map(({ job, changes }) => [job.id, changes])).toEqual([['job-1', { status: 'rejected' }], ['job-2', { expected_salary: '80k' }]]);
    expect(plan.questionUpdates.map(({ entry, changes }) => [entry.question.id, changes])).toEqual([['q-1', { answer_notes: 'Hablar de la misión' }]]);
    expect(plan.questionCreates.map(({ target, values }) => [target.stage.id, values])).toEqual([['stage-b', { question: '¿Cómo escalarías la caché?', is_asked: false }]]);
  });
});

describe('buildImportPlan with edited processes', () => {
  it('imports only edited cells, read back as stored values', async () => {
    const spreadsheet = await exportIn('es');
    const [board] = spreadsheet.sheets;
    setCell(board, 2, 'Estado', 'Rechazado');
    setCell(board, 2, 'Descripción', 'Plain new overview');
    setCell(board, 2, 'Salario publicado – mínimo', null);
    setCell(board, 2, 'Stack tecnológico', 'Go; k8s');
    setCell(board, 2, 'Periodo de búsqueda', 'spring 2027');
    setCell(board, 2, 'Versión del CV', 4);
    setCell(board, 2, 'Fecha de envío del CV', dateSerial(2026, 11, 10));
    setCell(board, 2, 'Recomendación', 'No');
    setCell(board, 2, 'Agencia', 'Hays');
    setCell(board, 2, 'Moneda', 'eur');
    const plan = await buildImportPlan(spreadsheet, sourceFor(fixture()));
    expect(plan.warnings).toEqual([{ sheet: 'board', row: 3, problem: 'overviewFormatting', params: {}, column: 'workbook.description' }]);
    expect(plan.processUpdates).toHaveLength(1);
    expect(plan.processUpdates[0]).toMatchObject({
      row: 3,
      changes: {
        company_overview: 'Plain new overview', salary_min: null, technology_ids: ['tech-go', 'tech-k8s'], status: 'rejected',
        search_period_id: 'period-2', cv_version_id: 'cv-4', application_sent_date: '2026-11-10', is_referral: false, recruiter_agency: 'Hays',
      },
      columns: ['workbook.description', 'workbook.salaryMin', 'workbook.technologies', 'workbook.status', 'workbook.searchPeriod',
        'workbook.cvVersion', 'workbook.cvSentDate', 'workbook.referral', 'workbook.recruiterAgency'],
    });

    const target = targetMock();
    expect(await applyImportPlan(plan, target)).toEqual({ total: 1, failures: [] });
    expect(target.updateJob).toHaveBeenCalledWith('job-1', {
      company_overview: 'Plain new overview', technology_ids: ['tech-go', 'tech-k8s'], status: 'rejected', search_period_id: 'period-2',
      application_sent_date: '2026-11-10', is_referral: false, recruiter_agency: 'Hays', salary_type: 'no_min', salary_min: null, salary_max: 75000,
    });
    expect(target.setJobCvVersion).toHaveBeenCalledWith('job-1', 'cv-4');
  });

  it('warns only when an edited Description replaces Markdown formatting', async () => {
    const spreadsheet = await exportIn('en');
    setCell(spreadsheet.sheets[0], 2, 'Description', 'About\nWe build fast services, now in Go.');
    setCell(spreadsheet.sheets[0], 3, 'Description', 'Plain text stays plain');
    const plan = await buildImportPlan(spreadsheet, sourceFor(fixture()));
    expect(plan.processUpdates.map(({ job, changes }) => [job.id, changes])).toEqual([
      ['job-1', { company_overview: 'About\nWe build fast services, now in Go.' }],
      ['job-2', { company_overview: 'Plain text stays plain' }],
    ]);
    expect(plan.warnings).toEqual([{ sheet: 'board', row: 3, problem: 'overviewFormatting', params: {}, column: 'workbook.description' }]);
  });

  it('completes a salary change with the bound that did not change, and clears both bounds', async () => {
    const spreadsheet = await exportIn('en');
    const [board] = spreadsheet.sheets;
    setCell(board, 2, 'Published salary – maximum', '80.000');
    setCell(board, 3, 'Published salary – minimum', '50000');
    const plan = await buildImportPlan(spreadsheet, sourceFor(fixture()));
    const target = targetMock();
    await applyImportPlan(plan, target);
    expect(target.updateJob).toHaveBeenNthCalledWith(1, 'job-1', { salary_type: 'limited', salary_min: 60000, salary_max: 80000 });
    expect(target.updateJob).toHaveBeenNthCalledWith(2, 'job-2', { salary_type: 'no_max', salary_min: 50000, salary_max: null });
    expect(target.setJobCvVersion).not.toHaveBeenCalled();

    setCell(board, 2, 'Published salary – minimum', null);
    setCell(board, 2, 'Published salary – maximum', '');
    const cleared = await buildImportPlan(spreadsheet, sourceFor(fixture()));
    await applyImportPlan(cleared, target);
    expect(target.updateJob).toHaveBeenCalledWith('job-1', { salary_type: 'unknown', salary_min: null, salary_max: null });
  });

  it('ignores a label typed in another language when it means the saved value', async () => {
    const spreadsheet = await exportIn('en');
    setCell(spreadsheet.sheets[0], 2, 'Status', 'En curs');
    setCell(spreadsheet.sheets[0], 3, 'Company', 'Empresa desconocida');
    expect(planSize(await buildImportPlan(spreadsheet, sourceFor(fixture())))).toBe(0);
  });

  it('skips cells it cannot read and edits to read-only columns, with reasons', async () => {
    const spreadsheet = await exportIn('en');
    const [board] = spreadsheet.sheets;
    setCell(board, 3, 'Role', ' ');
    setCell(board, 3, 'Status', 'Pending');
    setCell(board, 3, 'Tech stack', 'Go; Rust');
    setCell(board, 3, 'Search period', 'Nope');
    setCell(board, 3, 'CV version', 9);
    setCell(board, 3, 'CV sent date', '13/11/2026');
    setCell(board, 3, 'Published salary – minimum', 'lots');
    setCell(board, 3, 'Current stage', 'Final');
    setCell(board, 3, 'CV file', 'other.pdf');
    setCell(board, 3, 'Your expected salary', '90k');
    const plan = await buildImportPlan(spreadsheet, sourceFor(fixture()));
    expect(plan.processUpdates).toEqual([expect.objectContaining({ row: 4, changes: { expected_salary: '90k' } })]);
    expect(plan.warnings).toEqual([
      { sheet: 'board', row: 4, problem: 'requiredValue', params: {}, column: 'workbook.role' },
      { sheet: 'board', row: 4, problem: 'invalidNumber', params: { value: 'lots' }, column: 'workbook.salaryMin' },
      { sheet: 'board', row: 4, problem: 'unknownTechnologies', params: { names: 'Rust' }, column: 'workbook.technologies' },
      { sheet: 'board', row: 4, problem: 'invalidValue', params: { value: 'Pending' }, column: 'workbook.status' },
      { sheet: 'board', row: 4, problem: 'unknownPeriod', params: { value: 'Nope' }, column: 'workbook.searchPeriod' },
      { sheet: 'board', row: 4, problem: 'unknownCvVersion', params: { value: '9' }, column: 'workbook.cvVersion' },
      { sheet: 'board', row: 4, problem: 'invalidDate', params: { value: '13/11/2026' }, column: 'workbook.cvSentDate' },
      { sheet: 'board', row: 4, problem: 'readOnly', params: {}, columns: ['workbook.currentStage', 'workbook.cvFile'] },
    ]);
  });

  it('warns that a process changed in War Room after the export instead of listing read-only edits', async () => {
    const spreadsheet = await exportIn('en');
    const data = fixture();
    data.jobs[0].updated_at = noon(2026, 11, 20);
    data.jobs[0].status_changed_at = noon(2026, 11, 20);
    let plan = await buildImportPlan(spreadsheet, sourceFor(data));
    expect(plan.warnings).toEqual([]);
    setCell(spreadsheet.sheets[0], 2, 'Role', 'Staff Engineer');
    plan = await buildImportPlan(spreadsheet, sourceFor(data));
    expect(plan.processUpdates[0].changes).toEqual({ position_title: 'Staff Engineer' });
    expect(plan.warnings).toEqual([{ sheet: 'board', row: 3, problem: 'changedSinceExport', params: {} }]);
  });

  it('skips rows with an unknown or repeated Process ID', async () => {
    const spreadsheet = await exportIn('en');
    const [board] = spreadsheet.sheets;
    board.rows.push([...board.rows[2]], [...board.rows[3]]);
    setCell(board, 4, 'Role', 'Copied role');
    setCell(board, 5, 'Process ID', 'ghost');
    const plan = await buildImportPlan(spreadsheet, sourceFor(fixture()));
    expect(planSize(plan)).toBe(0);
    expect(plan.warnings).toEqual([
      { sheet: 'board', row: 5, problem: 'duplicateProcess', params: { row: 3 } },
      { sheet: 'board', row: 6, problem: 'unknownProcess', params: {} },
    ]);
  });
});

describe('buildImportPlan with new processes', () => {
  it('adds a row without a Process ID like the New process form, then sets what creation did not', async () => {
    const spreadsheet = await exportIn('en');
    const [board] = spreadsheet.sheets;
    board.rows.push([], ['', ' '], []);
    setCell(board, 4, 'Company', 'Globex');
    setCell(board, 4, 'Role', 'Data Engineer');
    setCell(board, 4, 'Published salary – maximum', '90.000');
    setCell(board, 4, 'Tech stack', 'Postgres');
    setCell(board, 4, 'Status', 'Ongoing');
    setCell(board, 4, 'CV version', 3);
    setCell(board, 4, 'Date added', dateSerial(2026, 11, 1));
    setCell(board, 6, 'Company', 'No role');
    const plan = await buildImportPlan(spreadsheet, sourceFor(fixture()));
    expect(plan.processCreates).toEqual([{
      row: 5,
      values: { company_name: 'Globex', position_title: 'Data Engineer', salary_max: 90000, technology_ids: ['tech-pg'], status: 'ongoing', cv_version_id: 'cv-3' },
      columns: ['workbook.company', 'workbook.role', 'workbook.salaryMax', 'workbook.technologies', 'workbook.status', 'workbook.cvVersion'],
    }]);
    expect(plan.warnings).toEqual([
      { sheet: 'board', row: 5, problem: 'readOnly', params: {}, columns: ['workbook.createdDate'] },
      { sheet: 'board', row: 7, problem: 'roleRequired', params: {} },
    ]);

    // Like the browser demo, creation keeps the status Waiting and ignores the tech stack.
    const target = targetMock({
      createJob: vi.fn(async (payload) => ({
        id: 'job-new', company_name: payload.company_name, position_title: payload.position_title, status: 'waiting', technologies: [],
        salary_min: null, salary_max: 90000, selected_cv_version: { id: 'cv-4' },
      })),
    });
    await applyImportPlan(plan, target);
    expect(target.createJob).toHaveBeenCalledWith({
      company_name: 'Globex', position_title: 'Data Engineer', technology_ids: ['tech-pg'], status: 'ongoing',
      salary_type: 'no_min', salary_min: null, salary_max: 90000, create_default_stages: false,
    });
    expect(target.updateJob).toHaveBeenCalledWith('job-new', { technology_ids: ['tech-pg'], status: 'ongoing' });
    expect(target.setJobCvVersion).toHaveBeenCalledWith('job-new', 'cv-3');
  });

  it('sends no follow-up when creation saved every value', async () => {
    const plan = { processUpdates: [], processCreates: [{ row: 3, values: { position_title: 'Engineer' }, columns: [] }], questionUpdates: [], questionCreates: [], warnings: [] };
    const target = targetMock();
    await applyImportPlan(plan, target);
    expect(target.createJob).toHaveBeenCalledWith({ position_title: 'Engineer', company_name: '', create_default_stages: false });
    expect(target.updateJob).not.toHaveBeenCalled();
    expect(target.setJobCvVersion).not.toHaveBeenCalled();
  });
});

describe('buildImportPlan with a sheet that has no Process ID column', () => {
  it('matches rows by company and role, in any language', async () => {
    const data = fixture();
    data.jobs.push({ ...clone(data.jobs[1]), id: 'job-3' });
    const csv = 'Empresa;Puesto;Estado;Tipo de contrato\n'
      + '"Acme, Inc.";"Senior ""Platform"" Engineer";Aceptado;Indefinido\n'
      + 'acme, inc.;senior "platform" engineer;Rechazado;\n'
      + 'Empresa desconocida;Analyst;En espera;\n'
      + 'Globex;Engineer;En espera;\n';
    setLanguage('ca', { persist: false });
    const plan = await buildImportPlan({ dateOffset: 0, sheets: [{ name: 'board.csv', rows: parseDelimited(csv) }] }, sourceFor(data));
    expect(plan.processUpdates).toEqual([expect.objectContaining({ row: 2, changes: { status: 'accepted', employment_type: 'permanent' } })]);
    expect(plan.processCreates).toEqual([]);
    expect(plan.warnings).toEqual([
      { sheet: 'board', row: 3, problem: 'duplicateProcess', params: { row: 2 } },
      { sheet: 'board', row: 4, problem: 'noMatchingProcess', params: {} },
      { sheet: 'board', row: 5, problem: 'noMatchingProcess', params: {} },
    ]);
  });
});

describe('buildImportPlan with interview questions', () => {
  it('updates edited questions and adds rows without a Question ID to their stage', async () => {
    const spreadsheet = await exportIn('es');
    const [board, questions] = spreadsheet.sheets;
    setCell(board, 2, 'Empresa', 'Acme Corp');
    setCell(questions, 1, 'Pregunta', 'Why Acme?');
    setCell(questions, 1, 'Formulada', 'Sí');
    setCell(questions, 2, 'Empresa', 'Acme Corp');
    questions.rows.push([...questions.rows[3]], [...questions.rows[3]], [...questions.rows[3]], [...questions.rows[3]], [...questions.rows[3]]);
    setCell(questions, 4, 'ID de la pregunta', null);
    setCell(questions, 4, 'Pregunta', 'Explain sharding');
    setCell(questions, 4, 'Formulada', 'Yes');
    setCell(questions, 5, 'Notas de respuesta', 'Copied');
    setCell(questions, 6, 'ID de la pregunta', 'q-ghost');
    setCell(questions, 7, 'ID de la pregunta', '');
    setCell(questions, 7, 'ID de la fase', 'stage-ghost');
    setCell(questions, 8, 'ID de la pregunta', '');
    setCell(questions, 8, 'Pregunta', '');
    setCell(questions, 3, 'Fase', 'Final round');
    const plan = await buildImportPlan(spreadsheet, sourceFor(fixture()));
    expect(plan.processUpdates[0].changes).toEqual({ company_name: 'Acme Corp' });
    expect(plan.questionUpdates).toEqual([
      expect.objectContaining({ row: 2, changes: { question: 'Why Acme?', is_asked: true }, columns: ['workbook.question', 'workbook.asked'] }),
    ]);
    expect(plan.questionCreates).toEqual([expect.objectContaining({ row: 5, values: { question: 'Explain sharding', is_asked: true } })]);
    expect(plan.questionCreates[0].target.stage.id).toBe('stage-b');
    expect(plan.warnings).toEqual([
      { sheet: 'questions', row: 4, problem: 'readOnly', params: {}, columns: ['workbook.stage'] },
      { sheet: 'questions', row: 6, problem: 'duplicateQuestion', params: { row: 4 } },
      { sheet: 'questions', row: 7, problem: 'unknownQuestion', params: {} },
      { sheet: 'questions', row: 8, problem: 'unknownStage', params: {} },
      { sheet: 'questions', row: 9, problem: 'questionRequired', params: {} },
    ]);

    const target = targetMock();
    expect((await applyImportPlan(plan, target)).total).toBe(3);
    expect(target.updateQuestion).toHaveBeenCalledWith('q-1', { question: 'Why Acme?', is_asked: true });
    expect(target.createQuestion).toHaveBeenCalledWith({ stage_id: 'stage-b', question: 'Explain sharding', answer_notes: '' });
    expect(target.updateQuestion).toHaveBeenCalledWith('q-new', { is_asked: true });
  });

  it('adds a question that was not asked without marking it', async () => {
    const plan = { processUpdates: [], processCreates: [], questionUpdates: [], warnings: [],
      questionCreates: [{ row: 2, target: { stage: { id: 'stage-a' } }, values: { question: 'New?', answer_notes: 'Notes' } }] };
    const target = targetMock();
    await applyImportPlan(plan, target);
    expect(target.createQuestion).toHaveBeenCalledWith({ stage_id: 'stage-a', question: 'New?', answer_notes: 'Notes' });
    expect(target.updateQuestion).not.toHaveBeenCalled();
  });

  it('reads a questions sheet on its own, and warns when it has no ID columns', async () => {
    const spreadsheet = await exportIn('en');
    const questions = spreadsheet.sheets[1];
    setCell(questions, 3, 'Answer notes', 'Use LRU');
    const source = sourceFor(fixture());
    const plan = await buildImportPlan({ dateOffset: 0, sheets: [questions] }, source);
    expect(plan.questionUpdates.map((entry) => entry.changes)).toEqual([{ answer_notes: 'Use LRU' }]);

    const withoutIds = { name: 'q', rows: [['Company', 'Role', 'Stage', 'Question', 'Answer notes', 'Asked'], ['Acme', 'Engineer', 'HR', 'Why?', '', 'No']] };
    const skipped = await buildImportPlan({ dateOffset: 0, sheets: [withoutIds] }, source);
    expect(planSize(skipped)).toBe(0);
    expect(skipped.warnings).toEqual([{ sheet: 'questions', row: null, problem: 'questionsWithoutIds', params: {} }]);
  });
});

describe('recognizeSheets', () => {
  it('finds sheets by their headers below other rows, whatever they are named', () => {
    const found = recognizeSheets([
      { name: 'Notes', rows: [['Company', 'Role']] },
      { name: 'Mine', rows: [['My export'], [], ['Process ID', 'Role', 'Status', 'Status']] },
      { name: 'Another board', rows: [['Company', 'Role', 'Status']] },
    ]);
    expect(Object.keys(found)).toEqual(['board']);
    expect(found.board).toMatchObject({ headerRow: 2, columns: new Map([['workbook.processId', 0], ['workbook.role', 1], ['workbook.status', 2]]), language: 'en' });
  });

  it('prefers the interface language when headers read the same in several languages', () => {
    const rows = [['Empresa', 'Fase', 'Pregunta', 'ID de la fase', 'ID de la pregunta']];
    expect(recognizeSheets([{ name: 'q', rows }]).questions.language).toBe('es');
    setLanguage('ca', { persist: false });
    expect(recognizeSheets([{ name: 'q', rows }]).questions.language).toBe('ca');
  });

  it('rejects a file with no board or questions sheet', async () => {
    await expect(buildImportPlan({ dateOffset: 0, sheets: [{ name: 'x', rows: [['Name', 'Notes']] }] }, sourceFor(fixture()))).rejects.toMatchObject({ code: 'noSheets' });
  });
});

describe('applyImportPlan', () => {
  it('reports each record that fails and saves the rest, with progress', async () => {
    const plan = {
      processUpdates: [{ row: 3, job: { id: 'job-1' }, changes: { status: 'rejected' } }, { row: 4, job: { id: 'job-2' }, changes: { cv_version_id: null } }],
      processCreates: [], questionCreates: [], warnings: [],
      questionUpdates: [{ row: 2, entry: { question: { id: 'q-1' } }, changes: { question: 'x' } }],
    };
    const progress = vi.fn();
    const target = targetMock({
      updateJob: vi.fn(async () => { throw new Error('Status is invalid'); }),
      updateQuestion: vi.fn(async () => { throw {}; }),
    });
    const result = await applyImportPlan(plan, target, progress);
    expect(result).toEqual({ total: 3, failures: [{ sheet: 'board', row: 3, message: 'Status is invalid' }, { sheet: 'questions', row: 2, message: '' }] });
    expect(target.setJobCvVersion).toHaveBeenCalledWith('job-2', null);
    expect(progress.mock.calls).toEqual([[1, 3], [2, 3], [3, 3]]);
    expect(await applyImportPlan({ processUpdates: [], processCreates: [], questionUpdates: [], questionCreates: [] }, target)).toEqual({ total: 0, failures: [] });
  });
});
