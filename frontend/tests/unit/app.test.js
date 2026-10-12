import { beforeEach, describe, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({
  api: { getJobCounts: vi.fn(), getMeetings: vi.fn(), getJobs: vi.fn(), getTechnologies: vi.fn(), getSearchPeriods: vi.fn(), createSearchPeriod: vi.fn(), updateSearchPeriod: vi.fn(), deleteSearchPeriod: vi.fn(), createJob: vi.fn(), exportBackup: vi.fn(), importBackup: vi.fn(), getCvVersions: vi.fn() },
  renderCardGrid: vi.fn(), renderScheduleView: vi.fn(), closeWithFlip: vi.fn((_modal, backdrop, done) => {
    backdrop?.classList.remove('active');
    done?.();
  }),
  showToast: vi.fn(), confirmAction: vi.fn(),
}));
vi.mock('../../public/js/api.js', () => ({ api: mocks.api }));
vi.mock('../../public/js/components/cardGrid.js', () => ({ renderCardGrid: mocks.renderCardGrid }));
vi.mock('../../public/js/components/scheduleView.js', () => ({ renderScheduleView: mocks.renderScheduleView }));
vi.mock('../../public/js/flip.js', () => ({ closeWithFlip: mocks.closeWithFlip, cancelPendingFlipClose: vi.fn() }));
vi.mock('../../public/js/utils/toast.js', () => ({ showToast: mocks.showToast }));
vi.mock('../../public/js/utils/confirmDialog.js', () => ({ confirmAction: mocks.confirmAction }));

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

describe('application entry point', () => {
  it('boots, filters and searches, opens the creation form, and submits valid data', async () => {
    const localStorageGet = vi.spyOn(window.localStorage, 'getItem').mockImplementation(() => {
      throw new Error('storage unavailable');
    });
    delete document.documentElement.dataset.theme;
    document.body.innerHTML = `
      <div id="app-load-error" hidden><button id="app-load-retry" type="button">Try again</button></div>
      <div id="cards-grid" aria-busy="true"><div class="board-loading-state" role="status">Loading selection processes</div></div><div id="modal-backdrop"><div id="detail-modal"></div></div>
      <button id="btn-menu-toggle" aria-expanded="false"></button>
      <button id="btn-new-process" class="new-process-trigger"></button>
      <button id="btn-theme-toggle"></button>
      <div id="process-toolbar">
        <div class="toolbar-search-filter"><div class="process-filter-control"><button id="process-filter-trigger" aria-expanded="false"><span>Filters</span><span id="process-filter-count" hidden></span></button>
          <div id="process-filter-panel" hidden><div class="process-filter-panel-header"><strong>Filters</strong><button class="process-filter-close" type="button"></button></div><section class="process-filter-active" hidden><div class="process-filter-chips" data-mobile-filter-chips></div></section><fieldset class="process-filter-status"><legend>Status</legend><input type="radio" name="process-status-filter" value="waiting"><input type="radio" name="process-status-filter" value="ongoing"><input type="radio" name="process-status-filter" value="accepted"><input type="radio" name="process-status-filter" value="rejected"><input type="radio" name="process-status-filter" value="all"><input type="radio" name="process-status-filter" value="invalid"></fieldset><fieldset class="process-filter-period"><legend>Search period</legend><div id="search-period-control" class="search-period-control search-period-select-wrap"><input id="search-period-filter" type="hidden" value="all"><button id="search-period-trigger" aria-expanded="false"><span id="search-period-current"></span></button><div id="search-period-menu" role="menu" hidden></div></div></fieldset><input type="checkbox" name="process-filter-arrangement" value="remote"><input type="checkbox" name="process-filter-arrangement" value="hybrid"><input type="checkbox" name="process-filter-arrangement" value="on_site"><input id="process-filter-any-referral" type="radio" name="process-filter-referral"><input id="process-filter-referral" type="radio" name="process-filter-referral"><input id="process-filter-no-referral" type="radio" name="process-filter-referral"><input id="process-filter-expected-salary"><input id="process-filter-salary-min"><input id="process-filter-salary-max"><input id="process-filter-posted-salary-min"><input id="process-filter-posted-salary-max"><input id="process-filter-technology-search"><div id="process-filter-technology-options"></div><select id="process-filter-currency"><option value="">Any</option><option value="EUR">EUR</option></select><button class="process-filter-done" type="button">Done</button></div></div>
        </div>
        <div id="process-filter-chips"></div><div id="process-filter-panel-footer" hidden><button id="process-filter-clear"></button></div><span id="process-filter-summary"></span><div class="toolbar-view-controls"><div class="process-sort-control"><button id="process-sort-trigger"><span id="process-sort-current"></span></button><div id="process-sort-menu" hidden><button data-sort-mode="added-newest"></button><button data-sort-mode="added-oldest"></button><button data-sort-mode="status-newest"></button><button data-sort-mode="status-oldest"></button><button data-sort-mode="advanced"></button><button data-sort-mode="early"></button><button data-sort-mode="manual"></button></div></div></div>
      </div>
      <div id="header-controls"><div class="search-wrapper"><button class="search-focus"></button><input id="search-input"></div>
      <div class="more-actions-menu"><button id="toolbar-more-trigger" aria-expanded="false"></button><div id="toolbar-more-options" role="menu" hidden><button id="btn-cv-library" role="menuitem"></button><button id="btn-data-management" role="menuitem"></button></div></div>
      <nav class="board-view-switch"><button id="tab-board" class="board-view-option" aria-pressed="true">Board</button>
      <button id="tab-schedule" class="board-view-option" data-filter="schedule" aria-pressed="false"><span>Schedule</span><span class="tab-count" id="count-meetings"></span></button></nav>
      <span id="count-all"></span><span id="count-waiting"></span><span id="count-ongoing"></span><span id="count-accepted"></span><span id="count-rejected"></span></div>
      `;
    mocks.api.getJobCounts.mockResolvedValue({ all: 1, waiting: 0, ongoing: 1, accepted: 0, rejected: 0 });
    const today = new Date();
    const dateAtOffset = (offset) => {
      const date = new Date(today);
      date.setDate(date.getDate() + offset);
      return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`;
    };
    mocks.api.getMeetings.mockResolvedValue([
      { meeting_date: dateAtOffset(-1) },
      { meeting_date: dateAtOffset(0) },
      { meeting_date: dateAtOffset(1) },
      { meeting_date: null },
    ]);
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
    const getSearchPeriods = mocks.api.getSearchPeriods;
    delete mocks.api.getSearchPeriods;
    const appImport = import('../../public/js/app.js');
    await vi.waitFor(() => expect(mocks.api.getJobs).toHaveBeenCalledTimes(1));
    mocks.api.getSearchPeriods = getSearchPeriods;
    expect(document.body.dataset.appReady).toBeUndefined();
    expect(document.querySelector('#cards-grid').getAttribute('aria-busy')).toBe('true');
    expect(document.querySelector('.board-loading-state')).not.toBeNull();
    resolveInitialJobs([]);
    await appImport;
    expect(document.querySelector('#app-load-error').hidden).toBe(true);
    expect(document.querySelector('#count-meetings').textContent).toBe('3');
    expect(document.querySelector('#cards-grid').getAttribute('aria-busy')).toBe('false');
    expect(document.querySelector('.board-loading-state')).toBeNull();
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
    mocks.api.getJobCounts.mockResolvedValueOnce({ all: 2, ongoing: 1, accepted: 0, rejected: 0 });
    document.querySelector('#process-filter-trigger').click();
    document.querySelector('input[name="process-status-filter"][value="waiting"]').click();
    await flush();
    expect(document.querySelector('#process-filter-panel').hidden).toBe(false);
    expect(mocks.renderCardGrid.mock.calls.at(-1)[6].statusCount).toBe(2);
    document.querySelector('input[name="process-status-filter"][value="ongoing"]').click();
    await flush();
    expect(document.querySelector('#process-filter-panel').hidden).toBe(false);
    expect(document.querySelector('input[name="process-status-filter"][value="ongoing"]').checked).toBe(true);
    document.querySelector('input[name="process-status-filter"][value="invalid"]').click();
    expect(document.querySelector('input[name="process-status-filter"][value="ongoing"]').checked).toBe(true);
    localStorageGet.mockRestore();
    const originalGetComputedStyle = window.getComputedStyle.bind(window);
    await flush();
    const searchInput = document.querySelector('#search-input');
    searchInput.value = 'platform';
    searchInput.dispatchEvent(new Event('input'));
    await new Promise((resolve) => setTimeout(resolve, 225));
    expect(mocks.api.getJobs).toHaveBeenLastCalledWith('ongoing', 'platform');
    expect(document.body.dataset.appReady).toBe('true');
    expect(mocks.renderCardGrid).toHaveBeenCalled();
    expect(document.querySelector('#process-toolbar').style.getPropertyValue('--toolbar-search-filter-width')).toBe('');
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
    expect(document.activeElement).toBe(document.querySelector('#create-position-title'));
    expect(document.querySelector('#detail-modal .modal-close-btn').getAttribute('aria-label')).toBe('Close dialog');
    const unassociatedLabels = [...document.querySelectorAll('#new-process-form label.meta-label')].filter((label) => !label.htmlFor || !document.getElementById(label.htmlFor));
    expect(unassociatedLabels).toHaveLength(0);
    expect(document.querySelector('.process-stage-creation-note').textContent).toContain('stages start empty');
    const overview = document.querySelector('#create-company-overview');
    const sourceTab = document.querySelector('#create-overview-tab-source');
    const previewTab = document.querySelector('#create-overview-tab-preview');
    overview.value = '## Role overview\n\n**Security-first** engineering.';
    sourceTab.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true, cancelable: true }));
    expect(document.activeElement).toBe(previewTab);
    expect(document.querySelector('#create-overview-preview').innerHTML).toContain('<strong>Security-first</strong>');
    expect(document.querySelector('#create-overview-source-panel').hidden).toBe(true);
    previewTab.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true, cancelable: true }));
    expect(document.activeElement).toBe(sourceTab);
    expect(document.querySelector('#create-overview-preview').hidden).toBe(true);
    document.querySelector('#create-keyword-note').value = 'AI infrastructure';
    document.querySelector('#create-keyword-note').dispatchEvent(new Event('input'));
    expect(document.querySelector('#keyword-char-count').textContent).toBe('17');
    overview.dispatchEvent(new Event('input', { bubbles: true }));
    expect(overview.style.height).not.toBe('');
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
    expect(document.querySelector('#new-process-form')).toBeNull();
    window.matchMedia = vi.fn(() => ({ matches: true, addEventListener: vi.fn() }));
    document.querySelector('#btn-new-process').click();
    expect(document.activeElement).toBe(document.querySelector('#detail-modal .modal-close-btn'));
    window.matchMedia = vi.fn(() => ({ matches: false, addEventListener: vi.fn() }));
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
    document.querySelector('#search-input').focus();
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'n', bubbles: true, cancelable: true }));
    expect(document.querySelector('#new-process-form')).toBeNull();
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', ctrlKey: true, bubbles: true, cancelable: true }));
    expect(document.activeElement).toBe(document.querySelector('#search-input'));
    document.querySelector('#search-input').blur();
    window.dispatchEvent(new KeyboardEvent('keydown', { key: '1', bubbles: true, cancelable: true }));
    await flush();
    expect(document.querySelector('input[name="process-status-filter"][value="waiting"]').checked).toBe(true);
    window.dispatchEvent(new KeyboardEvent('keydown', { key: '2', bubbles: true, cancelable: true }));
    await flush();
    window.dispatchEvent(new KeyboardEvent('keydown', { key: '3', bubbles: true, cancelable: true }));
    await flush();
    window.dispatchEvent(new KeyboardEvent('keydown', { key: '4', bubbles: true, cancelable: true }));
    await flush();
    window.dispatchEvent(new KeyboardEvent('keydown', { key: '5', bubbles: true, cancelable: true }));
    await flush();
    window.matchMedia = vi.fn(() => ({ matches: true, addEventListener: vi.fn() }));
    document.querySelector('input[name="process-status-filter"][value="accepted"]').click();
    expect(document.querySelector('input[name="process-status-filter"][value="accepted"]').checked).toBe(true);
    document.querySelector('#tab-schedule').click();
    await flush();
    expect(mocks.renderScheduleView).toHaveBeenCalled();
    expect(document.querySelector('#tab-schedule').getAttribute('aria-pressed')).toBe('true');
    document.querySelector('#tab-board').click();
    await flush();
    expect(document.querySelector('#tab-board').getAttribute('aria-pressed')).toBe('true');
    expect(document.querySelector('input[name="process-status-filter"][value="accepted"]').checked).toBe(true);
    window.matchMedia = vi.fn(() => ({ matches: false, addEventListener: vi.fn() }));
    document.querySelector('input[name="process-status-filter"][value="ongoing"]').click();
    await flush();
    let currentSearchPeriods = [];
    mocks.api.getSearchPeriods.mockImplementation(async () => currentSearchPeriods);
    mocks.api.createSearchPeriod.mockImplementation(async (payload) => {
      currentSearchPeriods = [{ id: 'period-1', ...payload, job_count: 1 }];
      return currentSearchPeriods[0];
    });
    mocks.api.updateSearchPeriod.mockResolvedValue({ success: true });
    mocks.api.deleteSearchPeriod.mockImplementation(async () => { currentSearchPeriods = []; return { success: true }; });
    window.matchMedia = vi.fn(() => ({ matches: true, addEventListener: vi.fn() }));
    document.querySelector('[data-manage-search-periods]').click();
    await flush();
    expect(document.activeElement).toBe(document.querySelector('#btn-close-search-periods'));
    window.matchMedia = vi.fn(() => ({ matches: false, addEventListener: vi.fn() }));
    let periodForm = document.querySelector('#search-period-form');
    const now = new Date();
    const month = String(now.getMonth() + 1).padStart(2, '0');
    const startDate = `${now.getFullYear()}-${month}-01`;
    const endDate = `${now.getFullYear()}-${month}-${String(new Date(now.getFullYear(), now.getMonth() + 1, 0).getDate()).padStart(2, '0')}`;
    const periodName = new Intl.DateTimeFormat(undefined, { month: 'long', year: 'numeric', timeZone: 'UTC' }).format(new Date(`${startDate}T00:00:00Z`));
    expect(periodForm.elements.name.placeholder).toBe(periodName);
    periodForm.elements.start_date.value = startDate;
    periodForm.elements.start_date.dispatchEvent(new Event('change'));
    periodForm.elements.end_date.value = endDate;
    periodForm.elements.end_date.dispatchEvent(new Event('change'));
    expect(periodForm.elements.name.value).toBe(periodName);
    periodForm.elements.name.value = '';
    periodForm.elements.start_date.value = startDate;
    periodForm.elements.end_date.value = '';
    periodForm.elements.end_date.dispatchEvent(new Event('change'));
    expect(periodForm.elements.name.value).toBe(periodName);
    periodForm.elements.end_date.value = '2026-04-02';
    periodForm.elements.end_date.dispatchEvent(new Event('change'));
    expect(periodForm.elements.name.value).toContain('–');
    periodForm.elements.name.value = periodName;
    periodForm.elements.end_date.value = endDate;
    periodForm.elements.end_date.dispatchEvent(new Event('change'));
    expect(document.querySelector('#btn-cancel-search-period-edit').hidden).toBe(true);
    periodForm.elements.name.value = periodName;
    periodForm.elements.start_date.value = startDate;
    periodForm.elements.end_date.value = '';
    periodForm.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await vi.waitFor(() => expect(mocks.api.createSearchPeriod).toHaveBeenCalledWith({ name: periodName, start_date: startDate, end_date: '' }));
    expect(document.querySelector('#btn-cancel-search-period-edit').hidden).toBe(true);
    await flush();
    const periodFilter = document.querySelector('#search-period-filter');
    const periodTrigger = document.querySelector('#search-period-trigger');
    const periodMenu = document.querySelector('#search-period-menu');
    expect(document.querySelector('#process-filter-panel').contains(periodTrigger)).toBe(true);
    periodTrigger.click();
    expect(periodMenu.hidden).toBe(false);
    expect(document.querySelector('[data-search-period="period-1"]').textContent).toContain('Present');
    periodMenu.click();
    document.querySelector('[data-search-period="period-1"]').click();
    await flush();
    expect(periodFilter.value).toBe('period-1');
    expect(document.querySelector('#search-period-menu').hidden).toBe(true);
    expect(document.querySelectorAll('#process-filter-chips .process-filter-chip')).toHaveLength(0);
    expect(document.querySelector('#process-filter-count').textContent).toBe('2');
    expect(document.querySelector('#process-filter-panel-footer').hidden).toBe(false);
    expect(document.querySelector('#search-period-current').textContent).toBe(periodName);
    expect(document.querySelector('#process-filter-trigger').getAttribute('aria-label')).toContain('2 active filters');
    expect(document.querySelector('#search-period-trigger').getAttribute('aria-label')).toContain(periodName);
    document.querySelector('#process-filter-clear').click();
    await flush();
    expect(periodFilter.value).toBe('all');
    expect(document.querySelector('#process-filter-count').hidden).toBe(true);
    expect(document.querySelector('#process-filter-panel-footer').hidden).toBe(true);
    expect(document.querySelector('#process-filter-trigger').getAttribute('aria-label')).toContain('no active filters');
    const remoteFilter = document.querySelector('[name="process-filter-arrangement"][value="remote"]');
    remoteFilter.checked = true;
    remoteFilter.dispatchEvent(new Event('change'));
    await flush();
    expect(document.querySelector('#process-filter-count').textContent).toBe('1');
    const remoteChip = document.querySelector('#process-filter-chips [data-remove-filter="arrangement"]');
    remoteChip.click();
    await flush();
    expect(remoteFilter.checked).toBe(false);
    expect(document.activeElement).toBe(document.querySelector('#process-filter-trigger'));
    remoteFilter.checked = true;
    remoteFilter.dispatchEvent(new Event('change'));
    await flush();
    document.querySelector('#process-filter-clear').click();
    await flush();
    expect(periodFilter.value).toBe('all');
    expect(document.querySelector('#process-filter-count').hidden).toBe(true);
    periodTrigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true, cancelable: true }));
    expect(periodMenu.hidden).toBe(true);
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
    periodTrigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true, cancelable: true }));
    const periodOptions = [...periodMenu.querySelectorAll('button[role="menuitemradio"], button[role="menuitem"]')];
    expect(document.activeElement).toBe(periodOptions.find((option) => option.getAttribute('aria-checked') === 'true'));
    periodOptions[0].dispatchEvent(new KeyboardEvent('keydown', { key: 'End', bubbles: true, cancelable: true }));
    expect(document.activeElement).toBe(periodOptions.at(-1));
    periodOptions.at(-1).dispatchEvent(new KeyboardEvent('keydown', { key: 'Home', bubbles: true, cancelable: true }));
    expect(document.activeElement).toBe(periodOptions[0]);
    periodOptions[0].dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true, cancelable: true }));
    expect(document.activeElement).toBe(periodOptions.at(-1));
    periodOptions.at(-1).dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true, cancelable: true }));
    expect(document.activeElement).toBe(periodOptions[0]);
    periodMenu.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
    periodTrigger.click();
    document.querySelector('#search-period-control').dispatchEvent(new FocusEvent('focusout', { bubbles: true, relatedTarget: periodMenu }));
    expect(periodMenu.hidden).toBe(false);
    document.querySelector('#search-period-control').dispatchEvent(new FocusEvent('focusout', { bubbles: true, relatedTarget: document.body }));
    document.body.focus();
    await flush();
    expect(periodMenu.hidden).toBe(true);
    periodTrigger.click();
    document.querySelector('#search-period-control').dispatchEvent(new FocusEvent('focusout', { bubbles: true, relatedTarget: null }));
    document.querySelector('[data-search-period="unassigned"]').click();
    await flush();
    expect(periodFilter.value).toBe('unassigned');
    expect(periodMenu.hidden).toBe(true);
    document.querySelector('[data-search-period="all"]').click();
    await flush();
    expect(periodFilter.value).toBe('all');
    periodFilter.value = 'unassigned';
    periodFilter.dispatchEvent(new Event('change'));
    await flush();
    expect(document.querySelectorAll('.process-filter-chip')).toHaveLength(0);
    expect(document.querySelector('#process-filter-count').textContent).toBe('1');
    expect(document.querySelector('#process-filter-panel-footer').hidden).toBe(false);
    expect(document.querySelector('#search-period-current').textContent).toBe('No period');
    document.querySelector('[data-search-period="all"]').click();
    await flush();
    document.querySelector('#btn-close-search-periods').click();
    expect(document.querySelector('#detail-modal').classList.contains('search-period-manager-modal')).toBe(false);
    expect(document.querySelector('#modal-backdrop').classList.contains('search-period-manager-backdrop')).toBe(false);
    document.querySelector('[data-manage-search-periods]').click();
    await flush();
    expect(document.querySelector('#detail-modal').classList.contains('search-period-manager-modal')).toBe(true);
    expect(document.querySelector('#modal-backdrop').classList.contains('search-period-manager-backdrop')).toBe(true);
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
    expect(document.querySelector('#create-search-period').selectedOptions[0].textContent).toContain('Present');
    document.querySelector('.btn-cancel').click();
    document.querySelector('[data-manage-search-periods]').click();
    await flush();
    periodForm = document.querySelector('#search-period-form');
    document.querySelector('[data-edit-period="period-1"]').click();
    expect(document.querySelector('#btn-cancel-search-period-edit').hidden).toBe(false);
    expect(periodForm.querySelector('[type="submit"]').textContent).toBe('Save changes');
    expect(periodForm.elements.end_date.value).toBe('');
    periodForm.elements.name.value = 'Spring search';
    periodForm.elements.start_date.dispatchEvent(new Event('change'));
    expect(periodForm.elements.name.value).toBe('Spring search');
    periodForm.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await vi.waitFor(() => expect(mocks.api.updateSearchPeriod).toHaveBeenCalledWith('period-1', { name: 'Spring search', start_date: startDate, end_date: '' }));
    await flush();
    currentSearchPeriods[0].job_count = 2;
    document.querySelector('#btn-close-search-periods').click();
    document.querySelector('[data-manage-search-periods]').click();
    await flush();
    expect(document.querySelector('#search-period-list').textContent).toContain('2 jobs');
    mocks.confirmAction.mockReset().mockResolvedValue(true);
    mocks.confirmAction.mockResolvedValueOnce(false);
    document.querySelector('[data-delete-period="period-1"]').click();
    expect(mocks.api.deleteSearchPeriod).not.toHaveBeenCalled();
    periodFilter.value = 'period-1';
    periodFilter.dispatchEvent(new Event('change'));
    await flush();
    document.querySelector('#btn-close-search-periods').click();
    document.querySelector('[data-manage-search-periods]').click();
    await flush();
    expect(periodFilter.value).toBe('period-1');
    mocks.api.deleteSearchPeriod.mockRejectedValueOnce(new Error('Delete failed'));
    document.querySelector('[data-delete-period="period-1"]').click();
    await vi.waitFor(() => expect(mocks.api.deleteSearchPeriod).toHaveBeenCalledWith('period-1'));
    await vi.waitFor(() => expect(mocks.showToast).toHaveBeenCalledWith('Delete failed', 'error'));
    document.querySelector('#process-filter-chips').innerHTML = '<button data-remove-filter="searchPeriod"></button>';
    document.querySelector('[data-remove-filter="searchPeriod"]').click();
    await flush();
    expect(periodFilter.value).toBe('all');
    document.querySelector('[data-delete-period="period-1"]').click();
    await vi.waitFor(() => expect(mocks.api.deleteSearchPeriod).toHaveBeenCalledWith('period-1'));
    await flush();
    expect(periodFilter.value).toBe('all');
    document.querySelector('#btn-close-search-periods').click();
    periodTrigger.click();
    document.body.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    expect(periodMenu.hidden).toBe(true);
    document.querySelector('#btn-new-process').click();
    expect(document.querySelector('#create-search-period')).toBeNull();
    document.querySelector('.btn-cancel').click();
    const getPeriods = mocks.api.getSearchPeriods;
    getPeriods.mockRejectedValueOnce(new Error('Periods unavailable'));
    document.querySelector('[data-manage-search-periods]').click();
    await flush();
    expect(mocks.showToast).toHaveBeenCalledWith('Periods unavailable', 'error');
    document.querySelector('#btn-close-search-periods').click();
    expect(document.querySelector('input[name="process-status-filter"][value="all"]').checked).toBe(true);
    expect(document.querySelector('input[name="process-status-filter"][value="accepted"]').checked).toBe(false);
    const arrangementFilter = document.querySelector('[name="process-filter-arrangement"][value="remote"]');
    const hybridFilter = document.querySelector('[name="process-filter-arrangement"][value="hybrid"]');
    arrangementFilter.checked = true;
    hybridFilter.checked = true;
    arrangementFilter.dispatchEvent(new Event('change'));
    await vi.waitFor(() => expect(mocks.renderCardGrid.mock.calls.at(-1)[1]).toEqual([matchingJob, otherJob]));
    expect(document.querySelector('#process-filter-count').textContent).toBe('1');
    expect([...document.querySelectorAll('#process-filter-chips .process-filter-chip')].map((chip) => chip.textContent.trim())).toEqual(['Remote', 'Hybrid']);
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
    const noReferralChip = [...document.querySelectorAll('#process-filter-chips .process-filter-chip')].find((chip) => chip.textContent.includes('No referral'));
    expect(noReferralChip).toBeTruthy();
    noReferralChip.click();
    await flush();
    expect(JSON.parse(window.localStorage.getItem('war-room.process.filters.v1')).referral).toBe('');
    expect(JSON.parse(window.localStorage.getItem('war-room.process.filters.v1')).arrangements).toEqual(['remote', 'hybrid']);
    document.querySelector('#process-filter-chips [data-remove-filter="expectedSalary"]').click();
    await flush();
    expect(JSON.parse(window.localStorage.getItem('war-room.process.filters.v1')).expectedSalaryQuery).toBe('');
    document.querySelector('#process-filter-chips [data-remove-filter="postingSalary"]').click();
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
    expect(document.querySelector('#process-filter-panel-footer').hidden).toBe(false);
    document.querySelector('#process-filter-clear').click();
    await flush();
    expect(document.querySelector('#process-filter-count').hidden).toBe(true);
    expect(document.querySelector('#process-filter-panel-footer').hidden).toBe(true);

    document.querySelector('input[name="process-status-filter"][value="ongoing"]').click();
    await flush();
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
    document.querySelector('#tab-schedule').click();
    await flush();
    expect(document.querySelector('#tab-schedule').getAttribute('aria-pressed')).toBe('true');
    expect(document.querySelector('#process-toolbar').hidden).toBe(true);
    document.querySelector('#tab-board').click();
    await flush();
    expect(document.querySelector('#tab-board').getAttribute('aria-pressed')).toBe('true');
    expect(document.querySelector('#process-toolbar').hidden).toBe(false);
    expect(document.querySelector('input[name="process-status-filter"][value="ongoing"]').checked).toBe(true);
    document.querySelector('#process-filter-trigger').click();
    expect(document.querySelector('#process-filter-panel').hidden).toBe(false);
    document.querySelector('#process-filter-panel').dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
    expect(document.querySelector('#process-filter-panel').hidden).toBe(true);
    expect(document.activeElement).toBe(document.querySelector('#process-filter-trigger'));
    document.querySelector('#process-filter-trigger').click();
    document.querySelector('#process-filter-trigger').dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
    expect(document.querySelector('#process-filter-panel').hidden).toBe(true);
    document.querySelector('#process-filter-trigger').click();
    document.body.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    expect(document.querySelector('#process-filter-panel').hidden).toBe(true);
    window.matchMedia = vi.fn((query) => ({ matches: query === '(max-width: 640px)', addEventListener: vi.fn() }));
    const mobileRemoteFilter = document.querySelector('[name="process-filter-arrangement"][value="remote"]');
    mobileRemoteFilter.checked = true;
    mobileRemoteFilter.dispatchEvent(new Event('change'));
    await flush();
    document.querySelector('#process-filter-trigger').click();
    const filterPanel = document.querySelector('#process-filter-panel');
    expect(filterPanel.getAttribute('role')).toBe('dialog');
    expect(filterPanel.getAttribute('aria-modal')).toBe('true');
    expect(document.body.classList.contains('process-filter-open')).toBe(true);
    expect(document.querySelector('[data-mobile-filter-chips] [data-remove-filter="arrangement"]')).not.toBeNull();
    document.querySelector('[data-mobile-filter-chips] [data-remove-filter="arrangement"]').click();
    await flush();
    expect(mobileRemoteFilter.checked).toBe(false);
    expect(filterPanel.hidden).toBe(false);
    document.querySelector('.process-filter-close').click();
    expect(filterPanel.hidden).toBe(true);
    expect(document.body.classList.contains('process-filter-open')).toBe(false);
    expect(document.activeElement).toBe(document.querySelector('#process-filter-trigger'));
    document.querySelector('#process-filter-trigger').click();
    document.querySelector('.process-filter-done').click();
    expect(filterPanel.hidden).toBe(true);
    document.querySelector('#process-filter-trigger').click();
    document.querySelector('#process-filter-trigger').click();
    expect(filterPanel.hidden).toBe(true);
    document.querySelector('#search-period-trigger').click();
    expect(document.querySelector('#search-period-menu').hidden).toBe(false);
    document.dispatchEvent(new Event('click'));
    expect(document.querySelector('#search-period-menu').hidden).toBe(false);
    document.body.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    expect(document.querySelector('#search-period-menu').hidden).toBe(true);
    document.querySelector('#process-sort-trigger').click();
    expect(document.querySelector('#process-sort-menu').hidden).toBe(false);
    document.querySelector('#process-sort-menu').dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    document.querySelector('#process-sort-trigger').dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true, cancelable: true }));
    expect(document.querySelector('#process-sort-menu').hidden).toBe(true);
    document.querySelector('#process-sort-trigger').dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true, cancelable: true }));
    const sortOptions = [...document.querySelectorAll('[data-sort-mode]')];
    const sortMenu = document.querySelector('#process-sort-menu');
    sortOptions[0].dispatchEvent(new KeyboardEvent('keydown', { key: 'End', bubbles: true, cancelable: true }));
    expect(document.activeElement).toBe(sortOptions.at(-1));
    sortOptions.at(-1).dispatchEvent(new KeyboardEvent('keydown', { key: 'Home', bubbles: true, cancelable: true }));
    expect(document.activeElement).toBe(sortOptions[0]);
    sortOptions[0].dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true, cancelable: true }));
    expect(document.activeElement).toBe(sortOptions.at(-1));
    sortOptions.at(-1).dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true, cancelable: true }));
    expect(document.activeElement).toBe(sortOptions[0]);
    sortMenu.dispatchEvent(new FocusEvent('focusout', { bubbles: true, relatedTarget: document.body }));
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
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
    expect(menuToggle.getAttribute('aria-expanded')).toBe('false');

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

    const activeStatus = document.querySelector('input[name="process-status-filter"]:checked').value;
    document.querySelector('input[name="process-status-filter"][value="invalid"]').click();
    await flush();
    expect(document.querySelector('input[name="process-status-filter"]:checked').value).toBe(activeStatus);
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
      create_default_stages: false,
    }));

    document.querySelector('#btn-new-process').click();
    const failedCreateForm = document.querySelector('#new-process-form');
    failedCreateForm.querySelector('[name="position_title"]').value = 'Rejected role';
    mocks.api.createJob.mockRejectedValueOnce(new Error('Create failed'));
    failedCreateForm.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await flush();
    expect(mocks.showToast).toHaveBeenCalledWith('Create failed', 'error');
    document.querySelector('#modal-backdrop').dispatchEvent(new MouseEvent('click', { bubbles: true }));
    expect(document.querySelector('.modal-title')).toBeNull();


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
    mocks.confirmAction.mockReset().mockResolvedValueOnce(false).mockResolvedValueOnce(true);
    mocks.api.importBackup.mockRejectedValueOnce(emptyBackup);
    importButton.click();
    await vi.waitFor(() => expect(mocks.confirmAction).toHaveBeenCalledOnce());
    await vi.waitFor(() => expect(importButton.disabled).toBe(false));
    expect(importButton.disabled).toBe(false);
    mocks.api.importBackup.mockRejectedValueOnce(emptyBackup).mockResolvedValueOnce({ success: true });
    importButton.click();
    await vi.waitFor(() => expect(mocks.api.importBackup).toHaveBeenLastCalledWith(backupFile, true));
    dataButton.click();
    document.querySelector('#modal-backdrop').dispatchEvent(new MouseEvent('click', { bubbles: true }));
    expect(document.querySelector('.modal-title')).toBeNull();

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

    const loadError = document.querySelector('#app-load-error');
    const retryButton = document.querySelector('#app-load-retry');
    mocks.api.getJobCounts.mockRejectedValue(new Error('temporary network failure'));
    retryButton.click();
    expect(retryButton.disabled).toBe(true);
    expect(retryButton.textContent).toBe('Retrying…');
    await vi.waitFor(() => expect(loadError.hidden).toBe(false));
    expect(retryButton.disabled).toBe(false);
    expect(retryButton.textContent).toBe('Try again');
    mocks.api.getJobCounts.mockResolvedValue({ all: 1, waiting: 0, ongoing: 1, accepted: 0, rejected: 0 });
    retryButton.click();
    await vi.waitFor(() => expect(loadError.hidden).toBe(true));

    const description = document.createElement('textarea');
    description.className = 'role-description-textarea';
    let descriptionContentHeight = 72;
    Object.defineProperty(description, 'scrollHeight', { configurable: true, get: () => descriptionContentHeight });
    vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => {
      if (element === description) return { maxHeight: '120px' };
      return originalGetComputedStyle(element);
    });
    document.body.append(description);
    description.dispatchEvent(new Event('input', { bubbles: true }));
    expect(description.style.height).toBe('72px');
    expect(description.style.overflowY).toBe('hidden');
    descriptionContentHeight = 180;
    description.dispatchEvent(new FocusEvent('focusin', { bubbles: true }));
    expect(description.style.height).toBe('120px');
    expect(description.style.overflowY).toBe('auto');

    const resetView = mocks.renderCardGrid.mock.calls.at(-1)[6].onResetView;
    document.querySelector('#search-input').value = 'stale search';
    document.querySelector('#search-period-filter').value = 'period-1';
    resetView();
    expect(document.querySelector('#search-input').value).toBe('');
    expect(document.querySelector('#search-period-filter').value).toBe('all');

    document.querySelector('#app-load-retry').remove();
    await import('../../public/js/app.js?missing-retry-control');
    await flush();

  });
});
