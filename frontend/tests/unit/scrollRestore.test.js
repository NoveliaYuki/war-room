import { describe, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({
  api: { getJobCounts: vi.fn(), getMeetings: vi.fn(), getJobs: vi.fn(), getTechnologies: vi.fn(), getSearchPeriods: vi.fn(), getCvVersions: vi.fn() },
  renderCardGrid: vi.fn(), renderScheduleView: vi.fn(), closeWithFlip: vi.fn(), cancelPendingFlipClose: vi.fn(),
  showToast: vi.fn(), confirmAction: vi.fn(),
}));
vi.mock('../../public/js/api.js', () => ({ api: mocks.api }));
vi.mock('../../public/js/components/cardGrid.js', () => ({ renderCardGrid: mocks.renderCardGrid }));
vi.mock('../../public/js/components/scheduleView.js', () => ({ renderScheduleView: mocks.renderScheduleView }));
vi.mock('../../public/js/flip.js', () => ({ closeWithFlip: mocks.closeWithFlip, cancelPendingFlipClose: mocks.cancelPendingFlipClose }));
vi.mock('../../public/js/utils/toast.js', () => ({ showToast: mocks.showToast }));
vi.mock('../../public/js/utils/confirmDialog.js', () => ({ confirmAction: mocks.confirmAction }));

describe('scroll position restore', () => {
  it('saves the scroll position unless a modal is open', async () => {
    delete document.documentElement.dataset.theme;
    document.body.innerHTML = `
      <header class="app-header"></header>
      <div id="app-load-error" hidden><button id="app-load-retry" type="button">Try again</button></div>
      <div id="cards-grid" aria-busy="true"><div class="board-loading-state" role="status">Loading selection processes</div></div><div id="modal-backdrop"><div id="detail-modal"></div></div>
      <button id="btn-menu-toggle" aria-expanded="false"></button>
      <button id="btn-new-process" class="new-process-trigger"></button>
      <button id="btn-theme-toggle"></button>
      <div id="process-toolbar">
        <div class="toolbar-search-filter"><div class="process-filter-control"><button id="process-filter-trigger" aria-expanded="false"><span>Filters</span><span id="process-filter-count" hidden></span></button>
          <div id="process-filter-panel" hidden><div class="process-filter-panel-header"><strong>Filters</strong><button class="process-filter-close" type="button"></button></div><section class="process-filter-active" hidden><div class="process-filter-chips" data-mobile-filter-chips></div></section><fieldset class="process-filter-status"><legend>Status</legend><input type="checkbox" name="process-status-filter" value="waiting"><input type="checkbox" name="process-status-filter" value="ongoing"><input type="checkbox" name="process-status-filter" value="accepted"><input type="checkbox" name="process-status-filter" value="rejected"><input type="checkbox" name="process-status-filter" value="all"><input type="checkbox" name="process-status-filter" value="invalid"></fieldset><fieldset class="process-filter-period"><legend>Search period</legend><div id="search-period-control" class="search-period-control search-period-select-wrap"><input id="search-period-filter" type="hidden" value="all"><button id="search-period-trigger" aria-expanded="false"><span id="search-period-current"></span></button><div id="search-period-menu" role="menu" hidden></div></div></fieldset><input type="checkbox" name="process-filter-arrangement" value="remote"><input type="checkbox" name="process-filter-arrangement" value="hybrid"><input type="checkbox" name="process-filter-arrangement" value="on_site"><input id="process-filter-any-referral" type="radio" name="process-filter-referral"><input id="process-filter-referral" type="radio" name="process-filter-referral"><input id="process-filter-no-referral" type="radio" name="process-filter-referral"><input id="process-filter-expected-salary"><input id="process-filter-salary-min"><input id="process-filter-salary-max"><input id="process-filter-posted-salary-min"><input id="process-filter-posted-salary-max"><input id="process-filter-technology-search"><div id="process-filter-technology-options"></div><select id="process-filter-currency"><option value="">Any</option><option value="EUR">EUR</option></select><button class="process-filter-done" type="button">Done</button></div></div>
        </div>
        <div id="process-filter-chips"></div><div id="process-filter-panel-footer" hidden><button id="process-filter-clear"></button></div><span id="process-filter-summary"></span><div class="toolbar-view-controls"><div class="process-sort-control"><button id="process-sort-trigger"><span id="process-sort-current"></span></button><div id="process-sort-menu" hidden><button data-sort-mode="added-newest"></button><button data-sort-mode="added-oldest"></button><button data-sort-mode="status-newest"></button><button data-sort-mode="status-oldest"></button><button data-sort-mode="advanced"></button><button data-sort-mode="early"></button><button data-sort-mode="manual"></button></div></div></div>
      </div>
      <div id="header-controls"><div class="search-wrapper"><button class="search-focus"></button><input id="search-input"></div>
      <div class="more-actions-menu"><button id="toolbar-more-trigger" aria-expanded="false"></button><div id="toolbar-more-options" role="menu" hidden><button id="btn-cv-library" role="menuitem"></button><button id="btn-data-management-menu" role="menuitem"></button></div></div>
      <nav class="board-view-switch"><button id="tab-board" class="board-view-option" aria-pressed="true">Board</button>
      <button id="tab-schedule" class="board-view-option" data-filter="schedule" aria-pressed="false"><span>Schedule</span><span class="tab-count" id="count-meetings"></span></button></nav>
      <span id="count-all"></span><span id="count-waiting"></span><span id="count-ongoing"></span><span id="count-accepted"></span><span id="count-rejected"></span></div>
      `;
    mocks.api.getJobCounts.mockResolvedValue({ all: 0, waiting: 0, ongoing: 0, accepted: 0, rejected: 0 });
    mocks.api.getMeetings.mockResolvedValue([]);
    mocks.api.getJobs.mockResolvedValue([]);
    mocks.api.getTechnologies.mockResolvedValue([]);
    mocks.api.getSearchPeriods.mockResolvedValue([]);
    mocks.api.getCvVersions.mockResolvedValue([]);
    mocks.renderScheduleView.mockResolvedValue(undefined);
    window.matchMedia = vi.fn(() => ({ matches: false, addEventListener: vi.fn() }));
    await import('../../public/js/app.js');
    expect(document.body.dataset.appReady).toBe('true');

    const backdrop = document.querySelector('#modal-backdrop');
    const setItem = vi.spyOn(window.localStorage, 'setItem');
    window.dispatchEvent(new Event('scroll'));
    expect(setItem).toHaveBeenCalledWith('war-room.window-scroll', String(window.scrollY));
    setItem.mockClear();
    backdrop.classList.add('active');
    window.dispatchEvent(new Event('scroll'));
    expect(setItem).not.toHaveBeenCalled();
    window.dispatchEvent(new Event('pagehide'));
    expect(setItem).not.toHaveBeenCalled();
    backdrop.classList.remove('active');
    window.dispatchEvent(new Event('pagehide'));
    expect(setItem).toHaveBeenCalledWith('war-room.window-scroll', String(window.scrollY));
    setItem.mockRestore();
  });
});
