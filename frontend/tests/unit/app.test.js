import { beforeEach, describe, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({
  api: { getJobCounts: vi.fn(), getMeetings: vi.fn(), getJobs: vi.fn(), createJob: vi.fn(), exportBackup: vi.fn(), importBackup: vi.fn() },
  renderCardGrid: vi.fn(), renderScheduleView: vi.fn(), closeWithFlip: vi.fn((_modal, _backdrop, done) => done?.()),
  showToast: vi.fn(),
}));
vi.mock('../../public/js/api.js', () => ({ api: mocks.api }));
vi.mock('../../public/js/components/cardGrid.js', () => ({ renderCardGrid: mocks.renderCardGrid }));
vi.mock('../../public/js/components/scheduleView.js', () => ({ renderScheduleView: mocks.renderScheduleView }));
vi.mock('../../public/js/flip.js', () => ({ closeWithFlip: mocks.closeWithFlip, cancelPendingFlipClose: vi.fn() }));
vi.mock('../../public/js/utils/toast.js', () => ({ showToast: mocks.showToast }));

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

describe('application entry point', () => {
  it('boots, filters and searches, opens the creation form, and submits valid data', async () => {
    const localStorageGet = vi.spyOn(window.localStorage, 'getItem').mockImplementation(() => {
      throw new Error('storage unavailable');
    });
    delete document.documentElement.dataset.theme;
    document.body.innerHTML = `
      <div id="cards-grid"></div><div id="modal-backdrop"><div id="detail-modal"></div></div>
      <button id="btn-menu-toggle" aria-expanded="false"></button>
      <button id="btn-new-process" class="new-process-trigger"></button>
      <button id="btn-theme-toggle"></button>
      <div id="header-controls"><div class="search-wrapper"><input id="search-input"></div>
      <button id="btn-data-management"></button><div class="filter-tabs">
      <button class="filter-tab" data-filter="ongoing"></button><button class="filter-tab" data-filter="accepted"></button>
      <button class="filter-tab" data-filter="rejected"></button><button class="filter-tab" data-filter="all"></button>
      <button class="filter-tab" data-filter="schedule"></button><button class="filter-tab" data-filter="invalid"></button></div></div>
      <span id="count-all"></span><span id="count-ongoing"></span><span id="count-accepted"></span><span id="count-rejected"></span><span id="count-meetings"></span>`;
    mocks.api.getJobCounts.mockResolvedValue({ all: 1, ongoing: 1, accepted: 0, rejected: 0 });
    mocks.api.getMeetings.mockResolvedValue([]);
    mocks.api.getJobs.mockResolvedValue([]);
    mocks.api.createJob.mockResolvedValue({ id: 'new' });
    mocks.renderScheduleView.mockResolvedValue(undefined);
    window.matchMedia = vi.fn(() => ({ matches: false, addEventListener: vi.fn() }));
    await import('../../public/js/app.js');
    localStorageGet.mockRestore();
    await flush();
    expect(mocks.renderCardGrid).toHaveBeenCalled();

    const themeToggle = document.querySelector('#btn-theme-toggle');
    themeToggle.click();
    expect(document.documentElement.dataset.theme).toBe('light');
    expect(themeToggle.getAttribute('aria-label')).toBe('Switch to dark theme');
    expect(window.localStorage.getItem('war-room.theme')).toBe('light');
    delete document.documentElement.dataset.theme;
    window.matchMedia = vi.fn(() => ({ matches: true, addEventListener: vi.fn() }));
    themeToggle.click();
    expect(document.documentElement.dataset.theme).toBe('dark');

    const menuToggle = document.querySelector('#btn-menu-toggle');
    const headerControls = document.querySelector('#header-controls');
    const dataButton = document.querySelector('#btn-data-management');
    menuToggle.click();
    expect(menuToggle.getAttribute('aria-expanded')).toBe('true');
    expect(headerControls.classList.contains('is-open')).toBe(true);
    menuToggle.click();
    expect(menuToggle.getAttribute('aria-expanded')).toBe('false');
    menuToggle.click();

    document.querySelector('[data-filter="accepted"]').click();
    expect(menuToggle.getAttribute('aria-expanded')).toBe('false');
    expect(headerControls.classList.contains('is-open')).toBe(false);
    window.dispatchEvent(new KeyboardEvent('keydown', { key: '3' }));
    expect(document.querySelector('[data-filter="rejected"]').classList.contains('active')).toBe(true);
    document.querySelector('#search-input').value = 'engineer';
    vi.useFakeTimers();
    document.querySelector('#search-input').dispatchEvent(new Event('input'));
    vi.advanceTimersByTime(220);
    vi.useRealTimers();
    await flush();
    expect(mocks.api.getJobs).toHaveBeenLastCalledWith('rejected', 'engineer');
    window.dispatchEvent(new KeyboardEvent('keydown', { key: '5' }));
    await flush();
    expect(mocks.renderScheduleView).toHaveBeenCalled();
    expect(window.localStorage.getItem('war-room.active-filter')).toBe('schedule');

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'n' }));
    const form = document.querySelector('#new-process-form');
    expect(form).not.toBeNull();
    const keyword = form.querySelector('[name="keyword_note"]');
    keyword.value = 'remote EU'; keyword.dispatchEvent(new Event('input'));
    expect(document.querySelector('#keyword-char-count').textContent).toBe('9');
    form.querySelector('[name="salary_min"]').value = '80000';
    form.querySelector('[name="salary_min"]').dispatchEvent(new Event('input'));
    expect(form.querySelector('[name="salary_type"]').value).toBe('no_max');
    form.querySelector('[name="position_title"]').value = 'Engineer';
    form.querySelector('[name="experience_notes"]').value = 'Platform migrations';
    form.querySelector('[name="expected_salary"]').value = 'Flexible by scope';
    form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await flush();
    expect(mocks.api.createJob).toHaveBeenCalledWith(expect.objectContaining({
      company_name: 'Unknown', position_title: 'Engineer', salary_type: 'no_max', salary_min: 80000,
      salary_max: null, keyword_note: 'remote EU', experience_notes: 'Platform migrations', expected_salary: 'Flexible by scope',
    }));
    expect(mocks.showToast).toHaveBeenCalledWith('Selection process created successfully!', 'success');

    // Cover keyboard shortcuts while typing, search focus, modal closing, and both salary bounds.
    document.querySelector('#search-input').focus();
    document.querySelector('#search-input').blur();
    menuToggle.click();
    document.querySelector('#search-input').focus();
    expect(menuToggle.getAttribute('aria-expanded')).toBe('false');
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'n' }));
    expect(document.querySelector('#new-process-form')).toBeNull();
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'n', ctrlKey: true }));
    expect(document.querySelector('#new-process-form')).toBeNull();
    document.querySelector('#search-input').blur();
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'n' }));
    const secondForm = document.querySelector('#new-process-form');
    secondForm.querySelector('[name="salary_min"]').value = '80000';
    secondForm.querySelector('[name="salary_max"]').value = '100000';
    secondForm.querySelector('[name="salary_max"]').dispatchEvent(new Event('input'));
    expect(secondForm.querySelector('[name="salary_type"]').value).toBe('limited');
    secondForm.querySelector('[name="salary_min"]').value = '';
    secondForm.querySelector('[name="salary_min"]').dispatchEvent(new Event('input'));
    expect(secondForm.querySelector('[name="salary_type"]').value).toBe('no_min');
    secondForm.querySelector('[name="salary_max"]').value = '';
    secondForm.querySelector('[name="salary_max"]').dispatchEvent(new Event('input'));
    expect(secondForm.querySelector('[name="salary_type"]').value).toBe('no_min');
    secondForm.querySelector('.btn-cancel').click();
    expect(mocks.closeWithFlip).toHaveBeenCalled();

    window.dispatchEvent(new KeyboardEvent('keydown', { key: '/', bubbles: true }));
    expect(document.activeElement).toBe(document.querySelector('#search-input'));
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', metaKey: true }));
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'n', metaKey: true }));
    expect(document.querySelector('#new-process-form')).toBeNull();
    document.querySelector('#search-input').blur();
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'n' }));
    const noSalaryForm = document.querySelector('#new-process-form');
    expect(noSalaryForm).not.toBeNull();
    noSalaryForm.querySelector('[name="position_title"]').value = 'Engineer';
    noSalaryForm.querySelector('[name="salary_type"]').value = 'unknown';
    noSalaryForm.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await flush();
    expect(mocks.api.createJob).toHaveBeenLastCalledWith(expect.objectContaining({
      salary_type: 'unknown', salary_min: null, salary_max: null,
    }));
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'n' }));
    expect(document.querySelector('#new-process-form')).not.toBeNull();
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    expect(document.querySelector('#detail-modal').innerHTML).toBe('');
    document.querySelector('#modal-backdrop').classList.remove('active');
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    document.querySelector('#detail-modal').click(); // A modal click must not be treated as a backdrop click.
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'n', altKey: true }));
    expect(document.querySelector('#new-process-form')).toBeNull();
    document.querySelector('#btn-new-process').click();
    document.querySelector('#modal-backdrop').click();
    expect(document.querySelector('#detail-modal').innerHTML).toBe('');

    // Logo error handling ignores unrelated elements and reveals the optional fallback.
    document.dispatchEvent(new Event('error'));
    const ignoredImage = document.createElement('img');
    document.body.append(ignoredImage);
    ignoredImage.dispatchEvent(new Event('error', { bubbles: true }));
    expect(ignoredImage.style.display).toBe('');
    const noFallbackImage = document.createElement('img');
    noFallbackImage.dataset.avatarFallback = 'true';
    document.body.append(noFallbackImage);
    noFallbackImage.dispatchEvent(new Event('error', { bubbles: true }));
    expect(noFallbackImage.style.display).toBe('none');
    const avatar = document.createElement('img');
    const fallback = document.createElement('div');
    avatar.dataset.avatarFallback = 'true';
    const avatarWrapper = document.createElement('div');
    avatarWrapper.append(avatar, fallback);
    document.body.append(avatarWrapper);
    avatar.dispatchEvent(new Event('error', { bubbles: true }));
    expect(avatar.style.display).toBe('none');
    expect(fallback.style.display).toBe('block');

    document.querySelector('#count-meetings').remove();
    document.querySelector('[data-filter="all"]').click();
    await flush();
    expect(mocks.api.getJobs).toHaveBeenLastCalledWith('all', 'engineer');

    // A failed initial API call is logged and does not escape the event handler.
    const logError = vi.spyOn(console, 'error').mockImplementation(() => {});
    mocks.api.getJobCounts.mockRejectedValueOnce(new Error('offline'));
    document.querySelector('[data-filter="all"]').click();
    await flush();
    expect(logError).toHaveBeenCalledWith('Failed to load jobs data:', expect.any(Error));
    logError.mockRestore();

    const localStorageSet = vi.spyOn(window.localStorage, 'setItem').mockImplementation(() => {
      throw new Error('storage unavailable');
    });
    document.querySelector('[data-filter="schedule"]').click();
    await flush();
    localStorageSet.mockRestore();

    mocks.api.createJob.mockRejectedValueOnce(new Error('offline'));
    document.querySelector('#btn-new-process').click();
    const failedForm = document.querySelector('#new-process-form');
    failedForm.querySelector('[name="position_title"]').value = 'Unavailable role';
    failedForm.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await flush();
    expect(mocks.showToast).toHaveBeenLastCalledWith('offline', 'error');

    document.querySelector('[data-filter="invalid"]').click();
    expect(document.querySelector('[data-filter="schedule"]').classList.contains('active')).toBe(true);
    document.querySelector('#btn-new-process').click();
    const maxOnlyForm = document.querySelector('#new-process-form');
    maxOnlyForm.querySelector('[name="salary_max"]').value = '100000';
    maxOnlyForm.querySelector('[name="position_title"]').value = 'Maximum-only role';
    maxOnlyForm.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await flush();
    expect(mocks.api.createJob).toHaveBeenLastCalledWith(expect.objectContaining({
      salary_type: 'no_min', salary_min: null, salary_max: 100000,
    }));


    // Export and import use the same ZIP dialog and confirmation regardless of data adapter.
    dataButton.click();
    expect(document.querySelector('#btn-export-backup').textContent).toContain('ZIP backup');
    expect(document.querySelector('#btn-import-backup').disabled).toBe(true);
    const fileInput = document.querySelector('#backup-import-file');
    const backupFile = new File(['zip'], 'backup.zip', { type: 'application/zip' });
    Object.defineProperty(fileInput, 'files', { configurable: true, value: [backupFile] });
    fileInput.dispatchEvent(new Event('change'));
    expect(document.querySelector('#btn-import-backup').disabled).toBe(true);
    document.querySelector('#backup-import-confirm').click();
    expect(document.querySelector('#btn-import-backup').disabled).toBe(false);
    mocks.api.importBackup.mockResolvedValueOnce({ success: true });
    document.querySelector('#btn-import-backup').click();
    await flush();
    expect(mocks.api.importBackup).toHaveBeenCalledWith(backupFile, false);
    expect(document.querySelector('#detail-modal').innerHTML).toBe('');

    dataButton.click();
    const anchorClick = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
    vi.stubGlobal('URL', Object.assign(URL, { createObjectURL: vi.fn(() => 'blob:backup'), revokeObjectURL: vi.fn() }));
    mocks.api.exportBackup.mockResolvedValueOnce(new Blob(['zip']));
    document.querySelector('#btn-export-backup').click();
    await flush();
    expect(anchorClick).toHaveBeenCalled();
    expect(mocks.showToast).toHaveBeenCalledWith('Backup downloaded', 'success');
    anchorClick.mockRestore();
    mocks.api.exportBackup.mockRejectedValueOnce(new Error('export failed'));
    document.querySelector('#btn-export-backup').click();
    await flush();
    expect(mocks.showToast).toHaveBeenCalledWith('export failed', 'error');
    mocks.api.exportBackup.mockRejectedValueOnce(new Error(''));
    document.querySelector('#btn-export-backup').click();
    await flush();
    expect(mocks.showToast).toHaveBeenCalledWith('Failed to export backup', 'error');

    const importFile = document.querySelector('#backup-import-file');
    Object.defineProperty(importFile, 'files', { configurable: true, value: [backupFile] });
    importFile.dispatchEvent(new Event('change'));
    document.querySelector('#backup-import-confirm').click();
    const importButton = document.querySelector('#btn-import-backup');
    mocks.api.importBackup.mockRejectedValueOnce(new Error('broken ZIP'));
    importButton.click();
    await flush();
    expect(document.querySelector('#backup-import-status').textContent).toBe('broken ZIP');
    expect(importButton.disabled).toBe(false);
    mocks.api.importBackup.mockRejectedValueOnce(new Error(''));
    importButton.click();
    await flush();
    expect(document.querySelector('#backup-import-status').textContent).toBe('Failed to import backup');
    const emptyBackup = Object.assign(new Error('empty'), { requiresEmptyConfirmation: true });
    const confirmEmpty = vi.fn().mockReturnValueOnce(false).mockReturnValueOnce(true);
    vi.stubGlobal('confirm', confirmEmpty);
    mocks.api.importBackup.mockRejectedValueOnce(emptyBackup);
    importButton.click();
    await flush();
    expect(importButton.disabled).toBe(false);
    mocks.api.importBackup.mockRejectedValueOnce(emptyBackup).mockResolvedValueOnce({ success: true });
    importButton.click();
    await flush();
    expect(confirmEmpty).toHaveBeenCalledTimes(2);
    expect(mocks.api.importBackup).toHaveBeenLastCalledWith(backupFile, true);
    dataButton.click();
    document.querySelector('#btn-close-data-modal').click();
    vi.unstubAllGlobals();
  });
});
