import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const { api, toast, confirmAction } = vi.hoisted(() => ({
  api: {
    getJobs: vi.fn(), getJob: vi.fn(), getSearchPeriods: vi.fn(), getTechnologies: vi.fn(), getCvVersions: vi.fn(),
    updateJob: vi.fn(), setJobCvVersion: vi.fn(), createJob: vi.fn(), updateQuestion: vi.fn(), createQuestion: vi.fn(),
    importBackup: vi.fn(),
  },
  toast: vi.fn(),
  confirmAction: vi.fn(),
}));
vi.mock('../../public/js/api.js', () => ({ api }));
vi.mock('../../public/js/utils/toast.js', () => ({ showToast: toast }));
vi.mock('../../public/js/utils/confirmDialog.js', () => ({ confirmAction }));

import { bindDataImport, isBackupFile, renderDataImport, warningText } from '../../public/js/components/dataImport.js';
import { setLanguage } from '../../public/js/i18n.js';

const job = (id, company, role, status = 'waiting') => ({ id, company_name: company, position_title: role, status, technologies: [] });
const csvFile = (lines, name = 'board.csv') => new File([lines.join('\n')], name, { type: 'text/csv' });

const backupFile = (name = 'war-room-backup.wrbackup', bytes = 'WRBACKUP\x02salt-and-ciphertext') => new File([bytes], name, { type: 'application/octet-stream' });

function mount(onImported = vi.fn(), { demo = false } = {}) {
  document.body.innerHTML = `<div id="root">${renderDataImport()}</div>`;
  const root = document.querySelector('#root');
  bindDataImport(root, { demo, onImported });
  return {
    root, onImported,
    input: root.querySelector('#data-import-file'),
    button: root.querySelector('#btn-import-spreadsheet'),
    preview: root.querySelector('#data-import-preview'),
    backupPanel: root.querySelector('#data-import-backup'),
    confirm: root.querySelector('#backup-import-confirm'),
    backupButton: root.querySelector('#btn-import-backup'),
    status: root.querySelector('#data-import-status'),
  };
}

function choose(input, files) {
  Object.defineProperty(input, 'files', { configurable: true, value: files });
  input.dispatchEvent(new Event('change'));
}

beforeEach(() => {
  for (const mock of Object.values(api)) mock.mockReset();
  toast.mockReset();
  confirmAction.mockReset();
  api.getJobs.mockResolvedValue([job('job-1', 'Acme', 'Engineer'), job('job-2', 'Unknown', 'Analyst', 'ongoing')]);
  api.getSearchPeriods.mockResolvedValue([]);
  api.getTechnologies.mockResolvedValue([]);
  api.getCvVersions.mockResolvedValue([]);
  api.updateJob.mockResolvedValue({});
});

afterEach(() => setLanguage('en', { persist: false }));

describe('import section', () => {
  it('opens one file picker for spreadsheets and backups, with no action until a file is chosen', () => {
    const { root, input, button, backupPanel } = mount();
    expect(root.querySelector('h3').textContent).toBe('Import');
    expect(input.getAttribute('accept')).toContain('.xlsx');
    expect(input.getAttribute('accept')).toContain('.csv');
    expect(input.getAttribute('accept')).toContain('.wrbackup');
    expect(button.hidden).toBe(true);
    expect(backupPanel.hidden).toBe(true);
    const click = vi.spyOn(input, 'click').mockImplementation(() => {});
    root.querySelector('#data-import-choose').click();
    expect(click).toHaveBeenCalled();
  });

  it('previews the changes and warnings, then saves them and refreshes', async () => {
    const { root, input, button, preview, onImported } = mount();
    const jobs = await api.getJobs();
    let release;
    api.getJobs.mockImplementationOnce(() => new Promise((resolve) => { release = () => resolve(jobs); }));
    choose(input, [csvFile(['Process ID,Company,Role,Status', 'job-1,Acme,Engineer,Ongoing', 'job-2,Unknown Company,Analyst,Ongoing', 'ghost,X,Y,Waiting', ',Globex,Designer,'])]);
    expect(root.querySelector('#data-import-file-name').textContent).toBe('board.csv');
    await vi.waitFor(() => expect(release).toBeTypeOf('function'));
    expect(preview.textContent).toBe('Reading the spreadsheet…');
    expect(button.disabled).toBe(true);
    release();
    await vi.waitFor(() => expect(button.disabled).toBe(false));
    expect(button.hidden).toBe(false);
    expect([...preview.querySelectorAll('.spreadsheet-import-counts li')].map((item) => item.textContent)).toEqual(['1 process will be updated', '1 new process will be added']);
    expect([...preview.querySelectorAll('.spreadsheet-import-changes li')].map((item) => item.textContent)).toEqual(['Update Engineer at Acme: Status', 'Add Designer at Globex']);
    expect(preview.querySelector('.spreadsheet-import-notice strong').textContent).toBe('1 warning');
    expect(preview.querySelector('.spreadsheet-import-notice li').textContent).toMatch(/^Board, row 4: No process has this Process ID/);

    api.createJob.mockResolvedValue({ id: 'job-3', company_name: 'Globex', position_title: 'Designer' });
    button.click();
    await vi.waitFor(() => expect(onImported).toHaveBeenCalledWith({ close: true }));
    expect(api.updateJob).toHaveBeenCalledWith('job-1', { status: 'ongoing' });
    expect(api.createJob).toHaveBeenCalledWith({ company_name: 'Globex', position_title: 'Designer', create_default_stages: false });
    expect(toast).toHaveBeenCalledWith('2 changes imported', 'success');
    expect(preview.innerHTML).toBe('');
    expect(input.value).toBe('');
    expect(input.disabled).toBe(false);
    expect(button.disabled).toBe(true);
    expect(button.hidden).toBe(true);
    button.click();
    expect(api.updateJob).toHaveBeenCalledTimes(1);
  });

  it('lists the changes that could not be saved and keeps the dialog open', async () => {
    const { root, input, button, preview, onImported } = mount();
    api.updateJob.mockRejectedValueOnce(new Error('Status is invalid')).mockRejectedValueOnce(new Error(''));
    choose(input, [csvFile(['Process ID,Company,Role,Status', 'job-1,Acme,Engineer,Ongoing', 'job-2,,Analyst,Rejected'])]);
    await vi.waitFor(() => expect(button.disabled).toBe(false));
    button.click();
    expect(root.querySelector('#data-import-choose').disabled).toBe(true);
    await vi.waitFor(() => expect(onImported).toHaveBeenCalledWith({ close: false }));
    expect(preview.querySelector('strong').textContent).toBe('2 changes were not saved');
    expect([...preview.querySelectorAll('li')].map((item) => item.textContent)).toEqual(['Board, row 2: Status is invalid', 'Board, row 3: Could not save this change.']);
    expect(toast).toHaveBeenCalledWith('2 of 2 changes could not be imported', 'error');
  });

  it('does not close a dialog that was replaced while saving', async () => {
    const { root, input, button, onImported } = mount();
    choose(input, [csvFile(['Process ID,Company,Role,Status', 'job-1,Acme,Engineer,Ongoing'])]);
    await vi.waitFor(() => expect(button.disabled).toBe(false));
    button.click();
    root.remove();
    await vi.waitFor(() => expect(onImported).toHaveBeenCalledWith({ close: false }));
  });

  it('says when a file changes nothing', async () => {
    const { input, button, preview } = mount();
    choose(input, [csvFile(['Process ID,Company,Role,Status', 'job-1,Acme,Engineer,Waiting'])]);
    await vi.waitFor(() => expect(preview.textContent).toBe('No changes found: this file matches what War Room has saved.'));
    expect(button.hidden).toBe(false);
    expect(button.disabled).toBe(true);
  });

  it('explains files it cannot read, in the selected language', async () => {
    setLanguage('es', { persist: false });
    const { input, preview, button } = mount();
    choose(input, [new File([new Uint8Array([0xd0, 0xcf, 0x11, 0xe0, 0, 0])], 'old.xls')]);
    await vi.waitFor(() => expect(preview.querySelector('[role="alert"]').textContent).toBe('War Room no puede leer este archivo. Elige un libro de Excel (.xlsx) o un archivo CSV exportado desde War Room, o una copia de seguridad cifrada (.wrbackup).'));
    expect(button.hidden).toBe(true);
    choose(input, [csvFile(['Name,Notes', 'a,b'])]);
    await vi.waitFor(() => expect(preview.textContent).toContain('Este archivo no tiene una hoja Tablero'));
    api.getJobs.mockRejectedValueOnce(new Error('offline'));
    choose(input, [csvFile(['Process ID,Company,Role', 'job-1,Acme,Engineer'])]);
    await vi.waitFor(() => expect(preview.textContent).toBe('No se ha podido comparar la hoja de cálculo con tus datos. Inténtalo de nuevo.'));
  });

  it('shows only the newest file when choices overlap, and clears when no file is chosen', async () => {
    const { root, input, button, preview } = mount();
    let release;
    api.getJobs.mockImplementationOnce(() => new Promise((resolve) => { release = () => resolve([]); }));
    choose(input, [csvFile(['Process ID,Company,Role,Status', 'job-1,Acme,Engineer,Ongoing'], 'first.csv')]);
    await vi.waitFor(() => expect(release).toBeTypeOf('function'));
    choose(input, [csvFile(['Process ID,Company,Role,Status', 'job-1,Acme,Engineer,Rejected'], 'second.csv')]);
    await vi.waitFor(() => expect(button.disabled).toBe(false));
    release();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(preview.textContent).toContain('Status');
    expect(root.querySelector('#data-import-file-name').textContent).toBe('second.csv');

    api.getJobs.mockImplementationOnce(() => Promise.reject(new Error('late')));
    choose(input, [csvFile(['Process ID,Company,Role,Status', 'job-1,Acme,Engineer,Ongoing'], 'third.csv')]);
    choose(input, []);
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(preview.innerHTML).toBe('');
    expect(root.querySelector('#data-import-file-name').textContent).toBe('No file selected');
    expect(button.disabled).toBe(true);
  });

  it('previews interview question changes with their process and stage', async () => {
    const stages = [{ id: 'stage-1', stage_type: 'HR', custom_title: 'Recruiter Screen', questions: [{ id: 'q-1', question: 'Why us?', answer_notes: '', is_asked: false }] }];
    api.getJob.mockImplementation(async (id) => ({ id, stages: id === 'job-1' ? stages : [] }));
    const { input, preview, button } = mount();
    choose(input, [csvFile([
      'Company,Role,Stage,Question,Answer notes,Asked,Process ID,Stage ID,Question ID',
      'Acme,Engineer,Recruiter Screen,Why us?,Mission,Yes,job-1,stage-1,q-1',
      'Acme,Engineer,Recruiter Screen,Biggest failure?,,No,job-1,stage-1,',
    ])]);
    await vi.waitFor(() => expect(button.disabled).toBe(false));
    expect([...preview.querySelectorAll('.spreadsheet-import-changes li')].map((item) => item.textContent)).toEqual([
      'Update a question in Engineer at Acme, Recruiter Screen: Answer notes, Asked',
      'Add a question to Engineer at Acme, Recruiter Screen: Biggest failure?',
    ]);
    expect([...preview.querySelectorAll('.spreadsheet-import-counts li')].map((item) => item.textContent)).toEqual(['1 interview question will be updated', '1 new interview question will be added']);
  });

  it('shortens long lists', async () => {
    api.getJobs.mockResolvedValue(Array.from({ length: 52 }, (_, index) => job(`job-${index}`, 'Acme', `Role ${index}`)));
    const { input, preview, button } = mount();
    const rows = Array.from({ length: 52 }, (_, index) => `job-${index},Acme,Role ${index},Ongoing`);
    choose(input, [csvFile(['Process ID,Company,Role,Status', ...rows])]);
    await vi.waitFor(() => expect(button.disabled).toBe(false));
    const items = preview.querySelectorAll('.spreadsheet-import-changes li');
    expect(items).toHaveLength(51);
    expect(items[50].textContent).toBe('and 2 more');
  });

  it('recognizes an encrypted backup and replaces the data only after confirmation', async () => {
    const { input, button, preview, backupPanel, confirm, backupButton, status, onImported } = mount();
    const file = backupFile();
    choose(input, [file]);
    await vi.waitFor(() => expect(backupPanel.hidden).toBe(false));
    expect(backupPanel.querySelector('.data-transfer-warning').textContent).toMatch(/^This is an encrypted War Room backup\. Importing it replaces all processes/);
    expect(button.hidden).toBe(true);
    expect(preview.innerHTML).toBe('');
    expect(api.getJobs).not.toHaveBeenCalled();
    expect(backupButton.disabled).toBe(true);
    confirm.click();
    expect(backupButton.disabled).toBe(false);
    confirm.click();
    expect(backupButton.disabled).toBe(true);
    confirm.click();

    let finish;
    api.importBackup.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
    backupButton.click();
    expect(status.textContent).toBe('Validating and importing backup…');
    expect(backupButton.disabled).toBe(true);
    expect(confirm.disabled).toBe(true);
    expect(input.disabled).toBe(true);
    finish({ success: true });
    await vi.waitFor(() => expect(onImported).toHaveBeenCalledWith({ close: true }));
    expect(api.importBackup).toHaveBeenCalledWith(file, false);
    expect(toast).toHaveBeenCalledWith('Backup imported successfully', 'success');
  });

  it('shows why a backup was rejected and lets the user try again', async () => {
    const { input, confirm, backupButton, status, onImported } = mount();
    choose(input, [backupFile()]);
    await vi.waitFor(() => expect(confirm.closest('[hidden]')).toBeNull());
    confirm.click();
    api.importBackup.mockRejectedValueOnce(new Error('The backup key does not match')).mockRejectedValueOnce(new Error(''));
    backupButton.click();
    await vi.waitFor(() => expect(status.textContent).toBe('The backup key does not match'));
    expect(backupButton.disabled).toBe(false);
    expect(input.disabled).toBe(false);
    backupButton.click();
    await vi.waitFor(() => expect(status.textContent).toBe('Failed to import backup'));
    expect(onImported).not.toHaveBeenCalled();
  });

  it('asks before restoring a backup with no processes', async () => {
    const { input, confirm, backupButton, onImported } = mount();
    const file = backupFile();
    choose(input, [file]);
    await vi.waitFor(() => expect(confirm.closest('[hidden]')).toBeNull());
    confirm.click();
    const empty = Object.assign(new Error('empty'), { requiresEmptyConfirmation: true });
    confirmAction.mockResolvedValueOnce(false).mockResolvedValueOnce(true);
    api.importBackup.mockRejectedValueOnce(empty);
    backupButton.click();
    await vi.waitFor(() => expect(confirmAction).toHaveBeenCalledWith(expect.objectContaining({ title: 'Replace this workspace with an empty backup?', danger: true })));
    await vi.waitFor(() => expect(backupButton.disabled).toBe(false));
    expect(api.importBackup).toHaveBeenCalledTimes(1);

    api.importBackup.mockRejectedValueOnce(empty).mockResolvedValueOnce({ success: true });
    backupButton.click();
    await vi.waitFor(() => expect(onImported).toHaveBeenCalledWith({ close: true }));
    expect(api.importBackup).toHaveBeenLastCalledWith(file, true);
  });

  it('does not close a dialog that was replaced while restoring a backup', async () => {
    const { root, input, confirm, backupButton, onImported } = mount();
    choose(input, [backupFile()]);
    await vi.waitFor(() => expect(confirm.closest('[hidden]')).toBeNull());
    confirm.click();
    api.importBackup.mockResolvedValueOnce({ success: true });
    backupButton.click();
    root.remove();
    await vi.waitFor(() => expect(onImported).toHaveBeenCalledWith({ close: false }));
  });

  it('explains in the browser demo that backups cannot be restored there', async () => {
    setLanguage('ca', { persist: false });
    const { input, preview, backupPanel, button } = mount(vi.fn(), { demo: true });
    choose(input, [backupFile('copia.bin')]);
    await vi.waitFor(() => expect(preview.textContent).toBe('És una còpia de seguretat xifrada de War Room. La restauració de còpies de seguretat no està disponible a la demo del navegador.'));
    expect(backupPanel.hidden).toBe(true);
    expect(button.hidden).toBe(true);
    choose(input, [csvFile(['Process ID,Company,Role,Status', 'job-1,Acme,Engineer,Ongoing'])]);
    await vi.waitFor(() => expect(button.disabled).toBe(false));
  });

  it('resets the other kind of import when a different file is chosen', async () => {
    const { input, button, preview, backupPanel, confirm, backupButton } = mount();
    choose(input, [backupFile()]);
    await vi.waitFor(() => expect(backupPanel.hidden).toBe(false));
    confirm.click();
    choose(input, [csvFile(['Process ID,Company,Role,Status', 'job-1,Acme,Engineer,Ongoing'])]);
    expect(backupPanel.hidden).toBe(true);
    expect(confirm.checked).toBe(false);
    expect(backupButton.disabled).toBe(true);
    await vi.waitFor(() => expect(button.disabled).toBe(false));

    choose(input, [backupFile()]);
    expect(button.hidden).toBe(true);
    expect(preview.innerHTML).toBe('');
    await vi.waitFor(() => expect(backupPanel.hidden).toBe(false));
    button.click();
    expect(api.updateJob).not.toHaveBeenCalled();
  });

  it('ignores a backup choice replaced before it was recognized', async () => {
    const { input, button, backupPanel } = mount();
    choose(input, [backupFile()]);
    choose(input, [csvFile(['Process ID,Company,Role,Status', 'job-1,Acme,Engineer,Ongoing'])]);
    await vi.waitFor(() => expect(button.disabled).toBe(false));
    expect(backupPanel.hidden).toBe(true);
  });
});

describe('isBackupFile', () => {
  it('recognizes a backup by its extension or by the marker both backup formats start with', async () => {
    expect(await isBackupFile(new File(['anything'], 'Copy.WRBACKUP'))).toBe(true);
    expect(await isBackupFile(new File(['WRBACKUP\x02rest'], 'download'))).toBe(true);
    expect(await isBackupFile(new File(['WRBACKUP\x01rest'], 'old-format.bin'))).toBe(true);
    expect(await isBackupFile(new File(['PK\x03\x04'], 'board.xlsx'))).toBe(false);
    expect(await isBackupFile(new File(['WRBACK'], 'short.csv'))).toBe(false);
    expect(await isBackupFile(new File([''], ''))).toBe(false);
  });
});

describe('warningText', () => {
  it('names the sheet, row, and columns in the selected language', () => {
    setLanguage('ca', { persist: false });
    expect(warningText({ sheet: 'questions', row: null, problem: 'questionsWithoutIds', params: {} })).toBe("El full Preguntes d'entrevista no té columnes d'ID, així que no se n'han importat les files.");
    expect(warningText({ sheet: 'board', row: 7, problem: 'readOnly', params: {}, columns: ['workbook.currentStage', 'workbook.cvFile'] }))
      .toBe('Tauler, fila 7: Fase actual, Fitxer del CV no es pot canviar en importar, així que s\'han omès aquests canvis.');
    expect(warningText({ sheet: 'board', row: 3, problem: 'invalidDate', params: { value: 'ahir' }, column: 'workbook.cvSentDate' }))
      .toBe("Tauler, fila 3: Data d'enviament del CV «ahir» no és una data, així que s'ha omès. Fes servir AAAA-MM-DD.");
  });
});
