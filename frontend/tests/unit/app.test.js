import { beforeEach, describe, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({
  api: { getJobCounts: vi.fn(), getMeetings: vi.fn(), getJobs: vi.fn(), getTechnologies: vi.fn(), getSearchPeriods: vi.fn(), createSearchPeriod: vi.fn(), updateSearchPeriod: vi.fn(), deleteSearchPeriod: vi.fn(), createJob: vi.fn(), exportBackup: vi.fn(), importBackup: vi.fn(), getCvVersions: vi.fn() },
  renderCardGrid: vi.fn(), renderScheduleView: vi.fn(), closeWithFlip: vi.fn((_modal, backdrop, done) => {
    backdrop?.classList.remove('active');
    done?.();
  }),
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
      <div id="process-toolbar" class="process-filter-control"><button id="process-filter-trigger" aria-expanded="false"></button><div id="process-filter-panel" hidden><input type="checkbox" name="process-filter-arrangement" value="remote"><input type="checkbox" name="process-filter-arrangement" value="hybrid"><input type="checkbox" name="process-filter-arrangement" value="on_site"><input id="process-filter-any-referral" type="radio" name="process-filter-referral"><input id="process-filter-referral" type="radio" name="process-filter-referral"><input id="process-filter-no-referral" type="radio" name="process-filter-referral"><input id="process-filter-expected-salary"><input id="process-filter-salary-min"><input id="process-filter-salary-max"><input id="process-filter-posted-salary-min"><input id="process-filter-posted-salary-max"><input id="process-filter-technology-search"><div id="process-filter-technology-options"></div><select id="process-filter-currency"><option value="">Any</option><option value="EUR">EUR</option></select></div><div class="search-period-control"><input id="search-period-filter" type="hidden" value="all"><div class="search-period-select-wrap"><button id="search-period-trigger" aria-expanded="false"><span id="search-period-current"></span></button><div id="search-period-menu" role="radiogroup" hidden></div></div><button id="btn-manage-search-periods"></button></div><div id="process-filter-chips"></div><button id="process-filter-clear" hidden></button><span id="process-filter-count" hidden></span><span id="process-filter-summary"></span><div class="process-sort-control"><button id="process-sort-trigger"><span id="process-sort-current"></span></button><div id="process-sort-menu" hidden><button data-sort-mode="added-newest"></button><button data-sort-mode="added-oldest"></button><button data-sort-mode="status-newest"></button><button data-sort-mode="status-oldest"></button><button data-sort-mode="advanced"></button><button data-sort-mode="early"></button><button data-sort-mode="manual"></button></div></div></div>
      <div id="header-controls"><div class="search-wrapper"><button class="search-focus"></button><input id="search-input"></div>
      <div class="more-actions-menu"><button id="toolbar-more-trigger" aria-expanded="false"></button><div id="toolbar-more-options" role="menu" hidden><button id="btn-cv-library" role="menuitem"></button><button id="btn-data-management" role="menuitem"></button></div></div>
      <nav class="view-controls"><div class="filter-tabs">
      <div class="filter-current-group"><button id="filter-current-action"><span id="filter-current-label"></span><span id="filter-current-count"></span></button><button id="filter-menu-trigger" aria-expanded="false"></button></div>
      <div id="filter-menu" hidden><button class="filter-tab" data-filter="waiting" aria-checked="false"><span class="filter-label">Waiting</span><span class="tab-count" id="count-waiting"></span></button>
      <button class="filter-tab" data-filter="ongoing" aria-checked="false"><span class="filter-label">Ongoing</span><span class="tab-count" id="count-ongoing"></span></button>
      <button class="filter-tab" data-filter="accepted" aria-checked="false"><span class="filter-label">Accepted</span><span class="tab-count" id="count-accepted"></span></button>
      <button class="filter-tab" data-filter="rejected" aria-checked="false"><span class="filter-label">Rejected</span><span class="tab-count" id="count-rejected"></span></button>
      <button class="filter-tab" data-filter="all" aria-checked="false"><span class="filter-label">All</span><span class="tab-count" id="count-all"></span></button>
      <button class="filter-tab" data-filter="invalid" aria-checked="false"><span class="filter-label">Invalid</span></button></div></div>
      <button id="tab-schedule" class="schedule-trigger" data-filter="schedule" aria-pressed="false"><span>Daily Schedule</span><span class="tab-count" id="count-meetings"></span></button></nav></div>
      `;
    mocks.api.getJobCounts.mockResolvedValue({ all: 1, waiting: 0, ongoing: 1, accepted: 0, rejected: 0 });
    mocks.api.getMeetings.mockResolvedValue([]);
    mocks.api.getTechnologies.mockResolvedValue([{ id: 'tech-react', name: 'React' }, { id: 'tech-go', name: 'Go' }]);
    mocks.api.getSearchPeriods.mockResolvedValue([]);
    let resolveInitialJobs;
    mocks.api.getJobs.mockImplementation(() => new Promise((resolve) => {
      resolveInitialJobs = resolve;
    }));
    mocks.api.createJob.mockResolvedValue({ id: 'new' });
    mocks.api.getCvVersions.mockResolvedValue([]);
    mocks.renderScheduleView.mockResolvedValue(undefined);
    window.matchMedia = vi.fn(() => ({ matches: false, addEventListener: vi.fn() }));
    const appImport = import('../../public/js/app.js');
    await vi.waitFor(() => expect(mocks.api.getJobs).toHaveBeenCalledTimes(1));
    expect(document.body.dataset.appReady).toBeUndefined();
    resolveInitialJobs([]);
    await appImport;
    const matchingJob = {
      id: 'matching', work_arrangement: 'remote', is_referral: true, expected_salary: '€95k target',
      salary_min: 100000, salary_max: 120000, salary_currency: 'EUR',
      technologies: [{ id: 'tech-react' }, { id: 'tech-go' }], order_index: 1, status_changed_at: 1, created_at: 2,
      current_stage_index: 4, total_stages_count: 5,
    };
    const otherJob = {
      id: 'other', work_arrangement: 'hybrid', is_referral: false, expected_salary: '€80k target',
      salary_min: 80000, salary_max: 90000, salary_currency: 'EUR',
      technologies: [{ id: 'tech-react' }], order_index: 2, status_changed_at: 2, created_at: 1,
      current_stage_index: 1, total_stages_count: 5,
    };
    mocks.api.getJobs.mockResolvedValue([matchingJob, otherJob]);
    localStorageGet.mockRestore();
    await flush();
    const searchInput = document.querySelector('#search-input');
    searchInput.value = 'platform';
    searchInput.dispatchEvent(new Event('input'));
    await new Promise((resolve) => setTimeout(resolve, 225));
    expect(mocks.api.getJobs).toHaveBeenLastCalledWith('ongoing', 'platform');
    expect(document.body.dataset.appReady).toBe('true');
    expect(mocks.renderCardGrid).toHaveBeenCalled();
    const avatar = document.createElement('img');
    avatar.dataset.avatarFallback = 'true';
    const fallback = document.createElement('span');
    avatar.after(fallback);
    document.body.append(avatar, fallback);
    avatar.dispatchEvent(new Event('error', { bubbles: true }));
    expect(avatar.style.display).toBe('none');
    expect(fallback.style.display).toBe('block');
    window.dispatchEvent(new KeyboardEvent('keydown', { key: '/', bubbles: true, cancelable: true }));
    expect(document.activeElement).toBe(document.querySelector('#search-input'));
    document.querySelector('#search-input').blur();
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'n', bubbles: true, cancelable: true }));
    expect(document.querySelector('#new-process-form')).not.toBeNull();
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
    expect(document.querySelector('#new-process-form')).toBeNull();
    document.querySelector('#search-input').focus();
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'n', bubbles: true, cancelable: true }));
    expect(document.querySelector('#new-process-form')).toBeNull();
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', ctrlKey: true, bubbles: true, cancelable: true }));
    expect(document.activeElement).toBe(document.querySelector('#search-input'));
    document.querySelector('#search-input').blur();
    window.dispatchEvent(new KeyboardEvent('keydown', { key: '1', bubbles: true, cancelable: true }));
    await flush();
    expect(document.querySelector('[data-filter="waiting"]').getAttribute('aria-checked')).toBe('true');
    window.dispatchEvent(new KeyboardEvent('keydown', { key: '2', bubbles: true, cancelable: true }));
    await flush();
    window.dispatchEvent(new KeyboardEvent('keydown', { key: '3', bubbles: true, cancelable: true }));
    await flush();
    window.dispatchEvent(new KeyboardEvent('keydown', { key: '4', bubbles: true, cancelable: true }));
    await flush();
    window.dispatchEvent(new KeyboardEvent('keydown', { key: '5', bubbles: true, cancelable: true }));
    await flush();
    window.matchMedia = vi.fn(() => ({ matches: true, addEventListener: vi.fn() }));
    document.querySelector('[data-filter="accepted"]').click();
    expect(document.activeElement).toBe(document.querySelector('#btn-menu-toggle'));
    document.querySelector('#tab-schedule').click();
    await flush();
    expect(mocks.renderScheduleView).toHaveBeenCalled();
    document.querySelector('#filter-current-action').click();
    expect(document.activeElement).toBe(document.querySelector('#btn-menu-toggle'));
    window.matchMedia = vi.fn(() => ({ matches: false, addEventListener: vi.fn() }));
    document.querySelector('[data-filter="ongoing"]').click();
    await flush();
    let currentSearchPeriods = [];
    mocks.api.getSearchPeriods.mockImplementation(async () => currentSearchPeriods);
    mocks.api.createSearchPeriod.mockImplementation(async (payload) => {
      currentSearchPeriods = [{ id: 'period-1', ...payload, job_count: 1 }];
      return currentSearchPeriods[0];
    });
    mocks.api.updateSearchPeriod.mockResolvedValue({ success: true });
    mocks.api.deleteSearchPeriod.mockImplementation(async () => { currentSearchPeriods = []; return { success: true }; });
    document.querySelector('#btn-manage-search-periods').click();
    await flush();
    let periodForm = document.querySelector('#search-period-form');
    const now = new Date();
    const month = String(now.getMonth() + 1).padStart(2, '0');
    const startDate = `${now.getFullYear()}-${month}-01`;
    const endDate = `${now.getFullYear()}-${month}-${String(new Date(now.getFullYear(), now.getMonth() + 1, 0).getDate()).padStart(2, '0')}`;
    const periodName = new Intl.DateTimeFormat(undefined, { month: 'long', year: 'numeric', timeZone: 'UTC' }).format(new Date(`${startDate}T00:00:00Z`));
    periodForm.elements.start_date.value = startDate;
    periodForm.elements.start_date.dispatchEvent(new Event('change'));
    periodForm.elements.end_date.value = endDate;
    periodForm.elements.end_date.dispatchEvent(new Event('change'));
    expect(periodForm.elements.name.value).toBe(periodName);
    periodForm.elements.name.value = '';
    periodForm.elements.start_date.value = startDate;
    periodForm.elements.end_date.value = '';
    periodForm.elements.end_date.dispatchEvent(new Event('change'));
    expect(periodForm.elements.name.value).toBe('');
    periodForm.elements.end_date.value = '2026-04-02';
    periodForm.elements.end_date.dispatchEvent(new Event('change'));
    expect(periodForm.elements.name.value).toContain('–');
    periodForm.elements.name.value = periodName;
    periodForm.elements.end_date.value = endDate;
    periodForm.elements.end_date.dispatchEvent(new Event('change'));
    document.querySelector('#btn-cancel-search-period-edit').click();
    periodForm.elements.name.value = periodName;
    periodForm.elements.start_date.value = startDate;
    periodForm.elements.end_date.value = endDate;
    periodForm.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await vi.waitFor(() => expect(mocks.api.createSearchPeriod).toHaveBeenCalledWith({ name: periodName, start_date: startDate, end_date: endDate }));
    await flush();
    const periodFilter = document.querySelector('#search-period-filter');
    const periodTrigger = document.querySelector('#search-period-trigger');
    const periodMenu = document.querySelector('#search-period-menu');
    periodTrigger.click();
    expect(periodMenu.hidden).toBe(false);
    periodMenu.click();
    document.querySelector('[data-search-period="period-1"]').click();
    await flush();
    expect(periodFilter.value).toBe('period-1');
    expect(document.querySelector('#search-period-menu').hidden).toBe(true);
    expect([...document.querySelectorAll('.process-filter-chip')].some((chip) => chip.textContent.includes(periodName))).toBe(true);
    periodTrigger.click();
    periodTrigger.click();
    expect(periodMenu.hidden).toBe(true);
    periodTrigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
    periodTrigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
    expect(periodMenu.hidden).toBe(false);
    periodMenu.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
    periodMenu.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
    periodMenu.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true }));
    periodMenu.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    expect(periodMenu.hidden).toBe(true);
    document.querySelector('[data-remove-filter="searchPeriod"]').click();
    await flush();
    expect(periodFilter.value).toBe('all');
    periodFilter.value = 'unassigned';
    periodFilter.dispatchEvent(new Event('change'));
    await flush();
    expect([...document.querySelectorAll('.process-filter-chip')].some((chip) => chip.textContent.includes('Unassigned'))).toBe(true);
    document.querySelector('[data-remove-filter="searchPeriod"]').click();
    await flush();
    document.querySelector('#btn-close-search-periods').click();
    document.querySelector('#btn-manage-search-periods').click();
    await flush();
    const emptyPeriodListForm = document.querySelector('#search-period-form');
    emptyPeriodListForm.elements.name.value = 'Manual period';
    emptyPeriodListForm.elements.start_date.value = startDate;
    emptyPeriodListForm.elements.end_date.value = endDate;
    mocks.api.createSearchPeriod.mockRejectedValueOnce(new Error('Period overlaps'));
    emptyPeriodListForm.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await flush();
    expect(mocks.showToast).toHaveBeenCalledWith('Period overlaps', 'error');
    const unmatchedEdit = document.createElement('button');
    unmatchedEdit.dataset.editPeriod = 'missing-period';
    document.querySelector('#search-period-list').append(unmatchedEdit);
    unmatchedEdit.click();
    const unmatchedDelete = document.createElement('button');
    unmatchedDelete.dataset.deletePeriod = 'missing-period';
    document.querySelector('#search-period-list').append(unmatchedDelete);
    unmatchedDelete.click();
    expect(mocks.api.deleteSearchPeriod).not.toHaveBeenCalled();
    document.querySelector('#btn-close-search-periods').click();
    document.querySelector('#btn-new-process').click();
    expect(document.querySelector('#create-search-period').value).toBe('period-1');
    document.querySelector('.btn-cancel').click();
    document.querySelector('#btn-manage-search-periods').click();
    await flush();
    periodForm = document.querySelector('#search-period-form');
    document.querySelector('[data-edit-period="period-1"]').click();
    periodForm.elements.name.value = 'Spring search';
    periodForm.elements.start_date.dispatchEvent(new Event('change'));
    expect(periodForm.elements.name.value).toBe('Spring search');
    periodForm.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await vi.waitFor(() => expect(mocks.api.updateSearchPeriod).toHaveBeenCalledWith('period-1', { name: 'Spring search', start_date: startDate, end_date: endDate }));
    await flush();
    currentSearchPeriods[0].job_count = 2;
    document.querySelector('#btn-close-search-periods').click();
    document.querySelector('#btn-manage-search-periods').click();
    await flush();
    expect(document.querySelector('#search-period-list').textContent).toContain('2 jobs');
    const originalConfirm = window.confirm;
    window.confirm = vi.fn().mockReturnValue(true);
    window.confirm.mockReturnValueOnce(false);
    document.querySelector('[data-delete-period="period-1"]').click();
    expect(mocks.api.deleteSearchPeriod).not.toHaveBeenCalled();
    periodFilter.value = 'period-1';
    periodFilter.dispatchEvent(new Event('change'));
    await flush();
    document.querySelector('#btn-close-search-periods').click();
    document.querySelector('#btn-manage-search-periods').click();
    await flush();
    expect(periodFilter.value).toBe('period-1');
    mocks.api.deleteSearchPeriod.mockRejectedValueOnce(new Error('Delete failed'));
    document.querySelector('[data-delete-period="period-1"]').click();
    await flush();
    expect(mocks.showToast).toHaveBeenCalledWith('Delete failed', 'error');
    document.querySelector('[data-delete-period="period-1"]').click();
    await vi.waitFor(() => expect(mocks.api.deleteSearchPeriod).toHaveBeenCalledWith('period-1'));
    await flush();
    window.confirm = originalConfirm;
    expect(periodFilter.value).toBe('all');
    document.querySelector('#btn-close-search-periods').click();
    periodTrigger.click();
    document.body.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    expect(periodMenu.hidden).toBe(true);
    document.querySelector('#btn-new-process').click();
    expect(document.querySelector('#create-search-period').value).toBe('');
    document.querySelector('.btn-cancel').click();
    const getPeriods = mocks.api.getSearchPeriods;
    getPeriods.mockRejectedValueOnce(new Error('Periods unavailable'));
    document.querySelector('#btn-manage-search-periods').click();
    await flush();
    expect(mocks.showToast).toHaveBeenCalledWith('Periods unavailable', 'error');
    document.querySelector('#btn-close-search-periods').click();
    expect(document.querySelector('[data-filter="ongoing"]').getAttribute('aria-checked')).toBe('true');
    expect(document.querySelector('[data-filter="accepted"]').getAttribute('aria-checked')).toBe('false');
    const arrangementFilter = document.querySelector('[name="process-filter-arrangement"][value="remote"]');
    const hybridFilter = document.querySelector('[name="process-filter-arrangement"][value="hybrid"]');
    arrangementFilter.checked = true;
    hybridFilter.checked = true;
    arrangementFilter.dispatchEvent(new Event('change'));
    await vi.waitFor(() => expect(mocks.renderCardGrid.mock.calls.at(-1)[1]).toEqual([matchingJob, otherJob]));
    expect(document.querySelector('#process-filter-count').textContent).toBe('1');
    expect([...document.querySelectorAll('.process-filter-chip')].map((chip) => chip.textContent.trim())).toEqual(['Remote', 'Hybrid']);
    expect(document.querySelector('#process-filter-technology-options').innerHTML).toContain('React');
    const technologyOptions = document.querySelector('#process-filter-technology-options');
    const reactTechnology = technologyOptions.querySelector('[value="tech-react"]');
    reactTechnology.checked = true;
    reactTechnology.dispatchEvent(new Event('change', { bubbles: true }));
    await flush();
    document.querySelector('#process-filter-referral').checked = true;
    document.querySelector('#process-filter-referral').dispatchEvent(new Event('change'));
    await flush();
    document.querySelector('#process-filter-expected-salary').value = '95k';
    document.querySelector('#process-filter-expected-salary').dispatchEvent(new Event('change'));
    await flush();
    document.querySelector('#process-filter-posted-salary-min').value = '100000';
    document.querySelector('#process-filter-posted-salary-min').dispatchEvent(new Event('change'));
    document.querySelector('#process-filter-posted-salary-max').value = '120000';
    document.querySelector('#process-filter-posted-salary-max').dispatchEvent(new Event('change'));
    document.querySelector('#process-filter-currency').value = 'EUR';
    document.querySelector('#process-filter-currency').dispatchEvent(new Event('change'));
    expect(JSON.parse(window.localStorage.getItem('war-room.process.filters.v1'))).toMatchObject({ referral: 'yes', expectedSalaryQuery: '95k', postedSalaryMin: 100000 });
    await flush();
    expect(JSON.parse(window.localStorage.getItem('war-room.process.filters.v1'))).toMatchObject({
      referral: 'yes', expectedSalaryQuery: '95k', postedSalaryMin: 100000, technologies: ['tech-react'],
    });
    expect(JSON.parse(window.localStorage.getItem('war-room.process.filters.v1')).technologies).toEqual(['tech-react']);
    document.querySelector('#process-filter-no-referral').checked = true;
    document.querySelector('#process-filter-no-referral').dispatchEvent(new Event('change'));
    await flush();
    const noReferralChip = [...document.querySelectorAll('.process-filter-chip')].find((chip) => chip.textContent.includes('No referral'));
    expect(noReferralChip).toBeTruthy();
    noReferralChip.click();
    await flush();
    expect(JSON.parse(window.localStorage.getItem('war-room.process.filters.v1')).referral).toBe('');
    expect(JSON.parse(window.localStorage.getItem('war-room.process.filters.v1')).arrangements).toEqual(['remote', 'hybrid']);
    document.querySelector('[data-remove-filter="expectedSalary"]').click();
    await flush();
    expect(JSON.parse(window.localStorage.getItem('war-room.process.filters.v1')).expectedSalaryQuery).toBe('');
    document.querySelector('[data-remove-filter="postingSalary"]').click();
    await flush();
    expect(JSON.parse(window.localStorage.getItem('war-room.process.filters.v1'))).toMatchObject({ postedSalaryMin: null, postedSalaryMax: null, currency: '' });
    document.querySelector('[data-remove-filter="technology"]').click();
    await flush();
    expect(JSON.parse(window.localStorage.getItem('war-room.process.filters.v1')).technologies).toEqual([]);
    document.querySelector('[data-remove-filter="arrangement"][data-filter-value="remote"]').click();
    await flush();
    expect(JSON.parse(window.localStorage.getItem('war-room.process.filters.v1')).arrangements).toEqual(['hybrid']);
    document.querySelector('#process-filter-chips').dispatchEvent(new MouseEvent('click', { bubbles: true }));
    document.querySelector('#process-filter-chips').innerHTML = '<button data-remove-filter="unknown"></button>';
    document.querySelector('#process-filter-chips button').click();
    expect(document.querySelector('#process-filter-clear').hidden).toBe(false);
    document.querySelector('#process-filter-clear').click();
    await flush();
    expect(document.querySelector('#process-filter-count').hidden).toBe(true);

    document.querySelector('#process-sort-trigger').click();
    document.querySelector('[data-sort-mode="added-oldest"]').click();
    await flush();
    expect(window.localStorage.getItem('war-room.process.sort.ongoing')).toBe('added-oldest');
    document.querySelector('[data-sort-mode="status-newest"]').click();
    await vi.waitFor(() => expect(mocks.renderCardGrid.mock.calls.at(-1)[1]).toEqual([otherJob, matchingJob]));
    expect(window.localStorage.getItem('war-room.process.sort.ongoing')).toBe('status-newest');
    document.querySelector('[data-sort-mode="status-oldest"]').click();
    await vi.waitFor(() => expect(mocks.renderCardGrid.mock.calls.at(-1)[1]).toEqual([matchingJob, otherJob]));
    document.querySelector('[data-sort-mode="advanced"]').click();
    await vi.waitFor(() => expect(mocks.renderCardGrid.mock.calls.at(-1)[1]).toEqual([matchingJob, otherJob]));
    document.querySelector('[data-sort-mode="early"]').click();
    await vi.waitFor(() => expect(mocks.renderCardGrid.mock.calls.at(-1)[1]).toEqual([otherJob, matchingJob]));
    document.querySelector('[data-sort-mode="manual"]').click();
    await vi.waitFor(() => expect(mocks.renderCardGrid.mock.calls.at(-1)[1]).toEqual([matchingJob, otherJob]));
    document.querySelector('[data-sort-mode="added-oldest"]').click();
    await vi.waitFor(() => expect(mocks.renderCardGrid.mock.calls.at(-1)[1]).toEqual([otherJob, matchingJob]));
    const viewMenuTrigger = document.querySelector('#filter-menu-trigger');
    viewMenuTrigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true, cancelable: true }));
    expect(document.activeElement).toBe(document.querySelector('[data-filter="waiting"]'));
    document.querySelector('#filter-menu').dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true, cancelable: true }));
    expect(document.activeElement).toBe(document.querySelector('[data-filter="ongoing"]'));
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
    expect(document.activeElement).toBe(viewMenuTrigger);
    document.querySelector('#process-filter-trigger').click();
    expect(document.querySelector('#process-filter-panel').hidden).toBe(false);
    document.body.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    expect(document.querySelector('#process-filter-panel').hidden).toBe(true);
    document.querySelector('#process-sort-trigger').click();
    expect(document.querySelector('#process-sort-menu').hidden).toBe(false);
    document.querySelector('#process-sort-menu').dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    expect(document.querySelector('#process-sort-menu').hidden).toBe(true);
    const technologySearch = document.querySelector('#process-filter-technology-search');
    technologySearch.value = 'go';
    technologySearch.dispatchEvent(new Event('input'));
    expect(document.querySelector('#process-filter-technology-options').textContent.trim()).toBe('Go');

    const themeToggle = document.querySelector('#btn-theme-toggle');
    const animationFrames = [];
    const requestAnimationFrame = vi.spyOn(window, 'requestAnimationFrame').mockImplementation((callback) => {
      animationFrames.push(callback);
      return animationFrames.length;
    });
    themeToggle.click();
    expect(document.documentElement.dataset.theme).toBe('light');
    expect(document.documentElement.classList.contains('theme-switching')).toBe(true);
    expect(themeToggle.getAttribute('aria-label')).toBe('Switch to dark theme');
    expect(window.localStorage.getItem('war-room.theme')).toBe('light');
    animationFrames.shift()();
    expect(document.documentElement.classList.contains('theme-switching')).toBe(true);
    animationFrames.shift()();
    expect(document.documentElement.classList.contains('theme-switching')).toBe(false);
    delete document.documentElement.dataset.theme;
    window.matchMedia = vi.fn(() => ({ matches: true, addEventListener: vi.fn() }));
    themeToggle.click();
    expect(document.documentElement.dataset.theme).toBe('dark');
    animationFrames.shift()();
    animationFrames.shift()();
    requestAnimationFrame.mockRestore();
    const requestAnimationFrameFallback = window.requestAnimationFrame;
    window.requestAnimationFrame = undefined;
    themeToggle.click();
    await new Promise((resolve) => setTimeout(resolve, 10));
    window.requestAnimationFrame = requestAnimationFrameFallback;
    expect(document.documentElement.classList.contains('theme-switching')).toBe(false);

    const menuToggle = document.querySelector('#btn-menu-toggle');
    const headerControls = document.querySelector('#header-controls');
    const dataButton = document.querySelector('#btn-data-management');
    menuToggle.click();

    expect(menuToggle.getAttribute('aria-expanded')).toBe('true');
    expect(headerControls.classList.contains('is-open')).toBe(true);
    document.querySelector('.search-focus').click();
    expect(document.activeElement).toBe(document.querySelector('#search-input'));
    expect(menuToggle.getAttribute('aria-expanded')).toBe('true');
    expect(headerControls.classList.contains('is-open')).toBe(true);
    document.querySelector('#search-input').blur();
    menuToggle.click();
    expect(menuToggle.getAttribute('aria-expanded')).toBe('false');
    menuToggle.click();

    dataButton.click();
    expect(document.querySelector('#btn-open-cv-library')).toBeNull();
    expect(document.querySelector('.modal-title').textContent).toBe('Data & Backups');
    document.querySelector('#btn-close-data-modal').click();
    window.dispatchEvent(new Event('war-room:open-cv-library'));
    await flush();
    expect(document.querySelector('.modal-title').textContent).toBe('CV Library');
    expect(document.querySelector('#btn-cv-library-back')).toBeNull();
    document.querySelector('#btn-close-cv-library').click();
    expect(document.querySelector('.modal-title')).toBeNull();

    window.dispatchEvent(new Event('war-room:open-technology-library'));
    await flush();
    expect(document.querySelector('.modal-title').textContent).toBe('Technology catalog');
    document.querySelector('#btn-close-technology-library').click();
    await flush();
    expect(mocks.api.getTechnologies).toHaveBeenCalled();

    document.querySelector('[data-filter="invalid"]').click();
    document.querySelector('#btn-new-process').click();
    const maxOnlyForm = document.querySelector('#new-process-form');
    const minInput = maxOnlyForm.querySelector('[name="salary_min"]');
    const maxInput = maxOnlyForm.querySelector('[name="salary_max"]');
    minInput.value = '50000';
    minInput.dispatchEvent(new Event('input'));
    expect(maxOnlyForm.querySelector('[name="salary_type"]').value).toBe('no_max');
    maxInput.value = '100000';
    maxInput.dispatchEvent(new Event('input'));
    expect(maxOnlyForm.querySelector('[name="salary_type"]').value).toBe('limited');
    minInput.value = '';
    minInput.dispatchEvent(new Event('input'));
    expect(maxOnlyForm.querySelector('[name="salary_type"]').value).toBe('no_min');
    maxInput.value = '';
    maxInput.dispatchEvent(new Event('input'));
    expect(maxOnlyForm.querySelector('[name="salary_type"]').value).toBe('no_min');
    maxInput.value = '100000';
    maxOnlyForm.querySelector('[name="position_title"]').value = 'Maximum-only role';
    maxOnlyForm.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await flush();
    expect(mocks.api.createJob).toHaveBeenLastCalledWith(expect.objectContaining({
      salary_type: 'no_min', salary_min: null, salary_max: 100000,
    }));

    document.querySelector('#btn-new-process').click();
    const failedCreateForm = document.querySelector('#new-process-form');
    failedCreateForm.querySelector('[name="position_title"]').value = 'Rejected role';
    mocks.api.createJob.mockRejectedValueOnce(new Error('Create failed'));
    failedCreateForm.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await flush();
    expect(mocks.showToast).toHaveBeenCalledWith('Create failed', 'error');


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

    // Reload the module with valid saved filters to cover preference restoration and validation.
    window.localStorage.setItem('war-room.process.filters.v1', JSON.stringify({
      arrangements: ['remote', 'invalid'], searchPeriod: 'removed-period', expectedSalaryQuery: 'target', postedSalaryMin: 'invalid',
      postedSalaryMax: '120000', currency: 'EUR', referral: 'no', technologies: ['tech-react', 3],
    }));
    window.localStorage.setItem('war-room.process.sort.ongoing', 'newest');
    await import('../../public/js/app.js?restore-filters');
    await flush();
    expect(document.querySelector('[name="process-filter-arrangement"][value="remote"]').checked).toBe(true);
    expect(document.querySelector('[name="process-filter-arrangement"][value="hybrid"]').checked).toBe(false);
    expect(document.querySelector('#process-filter-expected-salary').value).toBe('target');
    expect(document.querySelector('#process-filter-posted-salary-min').value).toBe('');
    expect(document.querySelector('#process-filter-posted-salary-max').value).toBe('120000');
    expect(document.querySelector('#process-filter-currency').value).toBe('EUR');
    expect(document.querySelector('#process-filter-no-referral').checked).toBe(true);
    expect(document.querySelector('#process-sort-current').textContent).toBe('Added · Newest');
    expect(document.querySelector('#search-period-filter').value).toBe('all');
    document.querySelector('#process-filter-chips').dispatchEvent(new MouseEvent('click', { bubbles: true }));
    document.querySelector('#process-filter-clear').click();
    await flush();

    window.localStorage.setItem('war-room.process.filters.v1', JSON.stringify({ searchPeriod: 42 }));
    await import('../../public/js/app.js?invalid-search-period-type');
    await flush();
    expect(document.querySelector('#search-period-filter').value).toBe('all');

    ['process-filter-referral', 'process-filter-no-referral', 'process-filter-any-referral',
      'process-filter-expected-salary', 'process-filter-posted-salary-min',
      'process-filter-posted-salary-max', 'process-filter-currency'].forEach((id) => document.getElementById(id)?.remove());
    document.querySelectorAll('[name="process-filter-arrangement"]').forEach((control) => control.remove());
    await import('../../public/js/app.js?missing-filter-controls');
    await flush();
    const chips = document.querySelector('#process-filter-chips');
    for (const category of ['referral', 'expectedSalary', 'postingSalary']) {
      chips.innerHTML = `<button data-remove-filter="${category}"></button>`;
      chips.querySelector('button').click();
    }
    document.querySelector('#process-filter-clear').click();
    await flush();

  });
});
