import { describe, expect, it, vi } from 'vitest';

function accountDom() {
  return `
      <header class="app-header"></header>
      <nav class="board-view-switch"><button id="tab-board" type="button"></button><button id="tab-schedule" type="button"></button></nav>
      <div id="app-load-error" hidden><button id="app-load-retry" type="button">Try again</button></div>
      <div id="cards-grid"></div><div id="modal-backdrop"><div id="detail-modal"></div></div>
      <button id="btn-menu-toggle" aria-expanded="false"></button>
      <button id="btn-new-process" class="new-process-trigger"></button>
      <div class="account-menu">
        <button id="btn-account-menu" aria-expanded="false"><span id="account-avatar">?</span></button>
        <div id="account-menu-panel" hidden>
          <div id="google-account-section"></div>
          <button id="btn-theme-toggle" type="button"><span id="theme-current-label"></span></button>
          <button id="btn-data-management-menu" type="button">Data</button>
        </div>
      </div>
      <div id="process-toolbar" hidden></div><input id="search-input" />
      <span id="count-meetings"></span><span id="count-all"></span>
      <span id="count-waiting"></span><span id="count-ongoing"></span>
      <span id="count-accepted"></span><span id="count-rejected"></span>`;
}


const mocks = vi.hoisted(() => ({
  api: {
    getJobCounts: vi.fn(), getMeetings: vi.fn(), getJobs: vi.fn(), getTechnologies: vi.fn(),
    getSearchPeriods: vi.fn(), getCvVersions: vi.fn(), getGoogleStatus: vi.fn(),
    getGoogleFolders: vi.fn(), getGooglePickerToken: vi.fn(), setGoogleFolder: vi.fn(), disconnectGoogle: vi.fn(), backupNowGoogle: vi.fn(),
  },
  renderCardGrid: vi.fn(), renderScheduleView: vi.fn(), closeWithFlip: vi.fn(), cancelPendingFlipClose: vi.fn(),
  showToast: vi.fn(), confirmAction: vi.fn(),
}));
vi.mock('../../public/js/api.js', () => ({ api: mocks.api }));
vi.mock('../../public/js/components/cardGrid.js', () => ({ renderCardGrid: mocks.renderCardGrid }));
vi.mock('../../public/js/components/scheduleView.js', () => ({ renderScheduleView: mocks.renderScheduleView }));
vi.mock('../../public/js/flip.js', () => ({ closeWithFlip: mocks.closeWithFlip, cancelPendingFlipClose: mocks.cancelPendingFlipClose }));
vi.mock('../../public/js/utils/toast.js', () => ({ showToast: mocks.showToast }));
vi.mock('../../public/js/utils/confirmDialog.js', () => ({ confirmAction: mocks.confirmAction }));

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

describe('google account menu', () => {
  it('disables encrypted backup transfer in the browser demo', async () => {
    vi.resetModules();
    document.body.dataset.demo = 'true';
    document.body.innerHTML = accountDom();
    mocks.api.getGoogleStatus.mockClear();
    window.matchMedia = vi.fn(() => ({ matches: false, addEventListener: vi.fn() }));
    await import('../../public/js/app.js?demo-backup-transfer');
    await flush();
    document.querySelector('#btn-account-menu').click();
    expect(document.querySelector('#google-account-section').textContent).toContain('not available in the browser demo');
    document.querySelector('#btn-data-management-menu').click();
    await flush();
    expect(document.querySelector('#btn-export-backup').disabled).toBe(true);
    expect(document.querySelector('#backup-import-file').disabled).toBe(true);
    expect(document.querySelector('#backup-import-choose').disabled).toBe(true);
    expect(document.querySelector('#backup-import-confirm').disabled).toBe(true);
    expect(document.querySelector('#detail-modal').textContent).toContain('unavailable in the browser demo');
    expect(mocks.api.getGoogleStatus).not.toHaveBeenCalled();
    document.querySelector('#btn-close-data-modal').click();
    expect(document.activeElement).toBe(document.querySelector('#btn-account-menu'));
    document.body.dataset.demo = 'false';
  });

  it('connects state, folder choice, backup now, theme and disconnect', async () => {
    document.body.dataset.demo = 'false';
    document.body.innerHTML = `
      <header class="app-header"></header>
      <nav class="board-view-switch"><button id="tab-board" type="button"></button><button id="tab-schedule" type="button"></button></nav>
      <div id="app-load-error" hidden><button id="app-load-retry" type="button">Try again</button></div>
      <div id="cards-grid"></div><div id="modal-backdrop"><div id="detail-modal"></div></div>
      <button id="btn-menu-toggle" aria-expanded="false"></button>
      <button id="btn-new-process" class="new-process-trigger"></button>
      <div class="account-menu">
        <button id="btn-account-menu" aria-expanded="false"><span id="account-avatar">?</span></button>
        <div id="account-menu-panel" hidden>
          <div id="google-account-section"></div>
          <button id="btn-theme-toggle" type="button"><span id="theme-current-label"></span></button>
          <button id="btn-data-management-menu" type="button">Data</button>
        </div>
      </div>
      <div id="process-toolbar" hidden></div><input id="search-input" />
      <span id="count-meetings"></span><span id="count-all"></span>
      <span id="count-waiting"></span><span id="count-ongoing"></span>
      <span id="count-accepted"></span><span id="count-rejected"></span>`;
    mocks.api.getJobCounts.mockResolvedValue({ all: 0, waiting: 0, ongoing: 0, accepted: 0, rejected: 0 });
    mocks.api.getMeetings.mockResolvedValue([]);
    mocks.api.getJobs.mockResolvedValue([]);
    mocks.api.getTechnologies.mockResolvedValue([]);
    mocks.api.getSearchPeriods.mockResolvedValue([]);
    mocks.api.getCvVersions.mockResolvedValue([]);
    mocks.api.getGoogleStatus.mockResolvedValue({ configured: true, connected: true, email: 'a@example.com', picker_api_key: 'public-key', picker_app_id: '12345', folder_name: '', last_status: 'ok' });
    mocks.api.getGooglePickerToken.mockResolvedValue({ access_token: 'short-lived-token' });
    mocks.api.setGoogleFolder.mockResolvedValue({ configured: true, connected: true, email: 'a@example.com', folder_id: 'folder-1', folder_name: 'Backups', last_status: 'ok' });
    mocks.api.backupNowGoogle.mockResolvedValue({ configured: true, connected: true, email: 'a@example.com', folder_name: 'Backups', last_status: 'ok', last_backup_at: '2026-01-01T00:00:00Z' });
    mocks.api.disconnectGoogle.mockResolvedValue({ success: true });
    mocks.confirmAction.mockResolvedValue(true);
    window.matchMedia = vi.fn(() => ({ matches: false, addEventListener: vi.fn() }));

    await import('../../public/js/app.js');
    await flush();

    const panel = document.querySelector('#account-menu-panel');
    document.querySelector('#btn-account-menu').click();
    await flush();
    expect(panel.hidden).toBe(false);
    expect(document.querySelector('#google-account-section').textContent).toContain('a@example.com');
    expect(document.querySelector('#account-avatar').textContent).toBe('A');

    document.querySelector('#btn-data-management-menu').click();
    await flush();
    const driveBackupButton = document.querySelector('#btn-google-drive-backup');
    expect(driveBackupButton.textContent).toContain('Back up to Google Drive');
    driveBackupButton.click();
    await flush();
    expect(mocks.api.backupNowGoogle).toHaveBeenCalled();
    expect(document.querySelector('#google-drive-backup-status').textContent).toContain('uploaded');
    expect(document.querySelector('#google-drive-backup-folder').textContent).toBe('My Drive (root)');
    expect(document.querySelector('#btn-google-drive-choose-folder')).not.toBeNull();
    document.querySelector('#btn-close-data-modal').click();
    document.querySelector('#btn-account-menu').click();
    await flush();

    let failPickerLoad = false;
    let pickerLoadError;
    const originalHeadAppend = document.head.append.bind(document.head);
    delete window.gapi;
    document.head.append = (...nodes) => {
      for (const node of nodes) {
        if (node.src === 'https://apis.google.com/js/api.js') {
          queueMicrotask(() => {
            window.gapi = { load: (_name, options) => failPickerLoad ? options.onerror(pickerLoadError) : options.callback() };
            node.onload();
          });
        } else originalHeadAppend(node);
      }
    };
    let pickerCallback;
    window.google = { picker: {
      DocsView: class { setIncludeFolders() { return this; } setSelectFolderEnabled() { return this; } setMimeTypes() { return this; } },
      ViewId: { FOLDERS: 'folders' }, Action: { PICKED: 'picked' },
      PickerBuilder: class {
        addView() { return this; } setOAuthToken() { return this; } setDeveloperKey() { return this; } setAppId() { return this; }
        setOrigin() { return this; } setCallback(callback) { pickerCallback = callback; return this; }
        build() { return { setVisible: vi.fn() }; }
      },
    } };
    document.querySelector('#google-choose-folder').click();
    await flush();
    pickerCallback({ action: 'cancel' });
    pickerCallback({ action: 'picked', docs: [] });
    expect(mocks.api.setGoogleFolder).not.toHaveBeenCalled();
    failPickerLoad = true;
    pickerLoadError = new Error('Google Picker initialization failed');
    document.querySelector('#google-choose-folder').click();
    await flush();
    expect(document.querySelector('#google-folder-status').textContent).toContain('initialization failed');
    pickerLoadError = undefined;
    document.querySelector('#google-choose-folder').click();
    await flush();
    expect(document.querySelector('#google-folder-status').textContent).toContain('Could not open');
    failPickerLoad = false;
    document.querySelector('#google-choose-folder').click();
    await flush();
    pickerCallback({ action: 'picked', docs: [{ id: 'folder-1', name: 'Backups' }] });
    await flush();
    document.head.append = originalHeadAppend;
    expect(mocks.api.setGoogleFolder).toHaveBeenCalledWith('folder-1', 'Backups');

    mocks.api.getGoogleStatus.mockResolvedValue({ configured: true, connected: true, email: 'a@example.com', picker_api_key: 'public-key', picker_app_id: '12345', folder_name: 'Backups', last_status: 'ok' });
    document.querySelector('#btn-data-management-menu').click();
    await flush();
    expect(document.querySelector('#google-drive-backup-folder').textContent).toBe('Backups');
    document.querySelector('#btn-google-drive-choose-folder').click();
    await flush();
    pickerCallback({ action: 'picked', docs: [{ id: 'folder-2', name: 'Archive' }] });
    await flush();
    expect(mocks.api.setGoogleFolder).toHaveBeenCalledWith('folder-2', 'Archive');

    document.querySelector('#google-backup-now').click();
    await flush();
    expect(mocks.api.backupNowGoogle).toHaveBeenCalled();

    document.querySelector('#btn-theme-toggle').click();
    expect(document.querySelector('#theme-current-label').textContent).toBe('Light');

    mocks.api.getGoogleStatus.mockResolvedValue({ configured: true, connected: false });
    document.querySelector('#google-disconnect').click();
    await flush();
    expect(mocks.api.disconnectGoogle).toHaveBeenCalled();
    expect(document.querySelector('#google-account-section').textContent).toContain('Connect Google account');
    expect(document.querySelector('#google-account-section').textContent).toContain('when a job card is created or its status changes');
  });

  it('shows demo, unconfigured, and missing Picker configuration states', async () => {
    vi.resetModules();
    document.body.dataset.demo = 'true';
    document.body.innerHTML = accountDom();
    window.matchMedia = vi.fn(() => ({ matches: false, addEventListener: vi.fn() }));
    await import('../../public/js/app.js');
    await flush();
    document.querySelector('#btn-account-menu').click();
    await flush();
    expect(document.querySelector('#google-account-section').textContent).toContain('not available in the browser demo');

    vi.resetModules();
    document.body.dataset.demo = 'false';
    document.body.innerHTML = accountDom();
    mocks.api.getGoogleStatus.mockResolvedValue({ configured: false, connected: false });
    await import('../../public/js/app.js');
    await flush();
    document.querySelector('#btn-account-menu').click();
    await flush();
    expect(document.querySelector('#google-account-section').textContent).toContain('Not configured');

    vi.resetModules();
    document.body.innerHTML = accountDom();
    mocks.api.getGoogleStatus.mockResolvedValue({ configured: true, connected: true, email: 'b@example.com', folder_id: 'f1', folder_name: 'Backups', last_status: 'error' });
    mocks.api.setGoogleFolder.mockResolvedValue({ configured: true, connected: true, email: 'b@example.com', last_status: 'ok' });
    await import('../../public/js/app.js');
    await flush();
    document.querySelector('#btn-account-menu').click();
    await flush();
    expect(document.querySelector('#google-account-section').textContent).toContain('Last backup failed');
    document.querySelector('#google-choose-folder').click();
    await flush();
    expect(document.querySelector('#google-folder-status').textContent).toContain('Google Picker is not configured');
    expect(document.querySelector('#google-folder-picker')).toBeNull();
    await flush();
    expect(mocks.api.setGoogleFolder).not.toHaveBeenCalled();
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    expect(document.querySelector('#account-menu-panel').hidden).toBe(true);
  });

  it('handles OAuth return, outside clicks, and account action errors', async () => {
    vi.resetModules();
    document.body.dataset.demo = 'false';
    document.body.innerHTML = accountDom();
    window.history.replaceState(null, '', '/board?google=connected');
    mocks.api.getGoogleStatus.mockResolvedValue({ configured: true, connected: true, email: 'd@example.com', folder_name: 'Backups', last_status: 'ok' });
    mocks.api.backupNowGoogle.mockRejectedValue(new Error('Drive is down'));
    mocks.confirmAction.mockResolvedValue(false);
    window.matchMedia = vi.fn(() => ({ matches: false, addEventListener: vi.fn() }));
    await import('../../public/js/app.js');
    await flush();
    expect(mocks.showToast).toHaveBeenCalledWith('Google account connected', 'success');
    expect(window.location.search).toBe('');

    document.querySelector('#btn-account-menu').click();
    await flush();
    document.body.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    expect(document.querySelector('#account-menu-panel').hidden).toBe(true);

    document.querySelector('#btn-account-menu').click();
    await flush();
    document.querySelector('#btn-data-management-menu').click();
    expect(document.querySelector('#account-menu-panel').hidden).toBe(true);

    document.querySelector('#btn-account-menu').click();
    await flush();
    document.querySelector('#google-choose-folder').click();
    for (let i = 0; i < 6; i += 1) await flush();
    expect(document.querySelector('#google-folder-status').textContent).toContain('Google Picker is not configured');

    document.querySelector('#google-backup-now').click();
    await flush();
    expect(mocks.showToast).toHaveBeenCalledWith('Drive is down', 'error');

    mocks.api.disconnectGoogle.mockClear();
    document.querySelector('#google-disconnect').click();
    await flush();
    expect(mocks.api.disconnectGoogle).not.toHaveBeenCalled();
    mocks.confirmAction.mockResolvedValue(true);
    mocks.api.disconnectGoogle.mockRejectedValue(new Error('No route'));
    mocks.api.getGoogleStatus.mockResolvedValue({ configured: true, connected: false });
    document.querySelector('#google-disconnect').click();
    await flush();
    expect(mocks.showToast).toHaveBeenCalledWith('No route', 'error');
    expect(mocks.api.getGoogleStatus).toHaveBeenCalled();

    vi.resetModules();
    document.body.innerHTML = accountDom();
    window.history.replaceState(null, '', '/board?google=error');
    mocks.api.getGoogleStatus.mockResolvedValue({ configured: true, connected: false });
    delete window.gapi;
    await import('../../public/js/app.js');
    await flush();
    expect(mocks.showToast).toHaveBeenCalledWith('Google sign-in failed', 'error');

    vi.resetModules();
    document.body.innerHTML = accountDom();
    window.history.replaceState(null, '', '/board');
    mocks.api.getGoogleStatus.mockResolvedValue({ configured: true, connected: false });
    await import('../../public/js/app.js?drive-backup-connect');
    document.querySelector('#btn-data-management-menu').click();
    await flush();
    const connectLink = document.querySelector('#btn-google-drive-backup');
    expect(connectLink.tagName).toBe('A');
    expect(connectLink.getAttribute('href')).toBe('/api/google/connect');
  });

  it('shows an actionable message when Picker setup is missing', async () => {
    vi.resetModules();
    document.body.dataset.demo = 'false';
    document.body.innerHTML = accountDom();
    mocks.api.getGoogleStatus.mockResolvedValue({ configured: true, connected: true, email: 'c@example.com' });
    window.matchMedia = vi.fn(() => ({ matches: false, addEventListener: vi.fn() }));
    await import('../../public/js/app.js');
    await flush();
    document.querySelector('#google-choose-folder').click();
    await flush();
    expect(document.querySelector('#google-account-section').textContent).toContain('c@example.com');
    expect(document.querySelector('#google-folder-status').textContent).toContain('Google Picker is not configured');
  });
});
