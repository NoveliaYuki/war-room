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
        <button id="btn-account-menu" aria-expanded="false">
          <span id="account-avatar"><img id="account-avatar-img" alt="" hidden /><span id="account-avatar-fallback">?</span></span>
          <span id="account-menu-label">Account</span>
        </button>
        <div id="account-menu-panel" hidden>
          <div id="account-identity" hidden>
            <span class="account-identity-avatar">
              <img id="account-identity-avatar-img" alt="" hidden />
              <span id="account-identity-avatar-fallback">?</span>
              <button id="btn-change-avatar" type="button">Change</button>
            </span>
            <div><strong id="account-identity-name"></strong><span id="account-identity-email"></span></div>
          </div>
          <button id="btn-remove-avatar" type="button" hidden>Use Google photo instead</button>
          <input id="avatar-upload-file" type="file" hidden />
          <div id="google-account-section"></div>
          <button id="btn-theme-toggle" type="button"><span id="theme-current-label"></span></button>
          <button id="btn-data-management-menu" type="button">Data</button>
          <button id="btn-sign-out" type="button">Sign out</button>
        </div>
      </div>
      <div id="process-toolbar" hidden></div><input id="search-input" />
      <span id="count-meetings"></span><span id="count-all"></span>
      <span id="count-waiting"></span><span id="count-ongoing"></span>
      <span id="count-accepted"></span><span id="count-rejected"></span>`;
}


const mocks = vi.hoisted(() => {
  const backupProgressHandle = { success: vi.fn(), error: vi.fn() };
  return {
    api: {
      getJobCounts: vi.fn(), getMeetings: vi.fn(), getJobs: vi.fn(), getTechnologies: vi.fn(),
      getSearchPeriods: vi.fn(), getCvVersions: vi.fn(), getGoogleStatus: vi.fn(),
      getGoogleFolders: vi.fn(), getGooglePickerToken: vi.fn(), setGoogleFolder: vi.fn(),
      revokeGoogleDrive: vi.fn(), backupNowGoogle: vi.fn(), getAuthMe: vi.fn(), signOut: vi.fn(),
      uploadAvatar: vi.fn(), removeAvatar: vi.fn(),
    },
    renderCardGrid: vi.fn(), renderScheduleView: vi.fn(), closeWithFlip: vi.fn(), cancelPendingFlipClose: vi.fn(),
    showToast: vi.fn(), confirmAction: vi.fn(),
    showBackupProgress: vi.fn(() => backupProgressHandle),
    backupProgressHandle,
  };
});
vi.mock('../../public/js/api.js', () => ({ api: mocks.api }));
vi.mock('../../public/js/components/cardGrid.js', () => ({ renderCardGrid: mocks.renderCardGrid }));
vi.mock('../../public/js/components/scheduleView.js', () => ({ renderScheduleView: mocks.renderScheduleView }));
vi.mock('../../public/js/flip.js', () => ({ closeWithFlip: mocks.closeWithFlip, cancelPendingFlipClose: mocks.cancelPendingFlipClose }));
vi.mock('../../public/js/utils/toast.js', () => ({ showToast: mocks.showToast }));
vi.mock('../../public/js/utils/backupProgress.js', () => ({ showBackupProgress: mocks.showBackupProgress }));
vi.mock('../../public/js/utils/confirmDialog.js', () => ({ confirmAction: mocks.confirmAction }));

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

describe('google account menu', () => {
  it('disables encrypted backup transfer in the browser demo', async () => {
    vi.resetModules();
    document.body.dataset.demo = 'true';
    document.body.innerHTML = accountDom();
    mocks.api.getGoogleStatus.mockClear();
    mocks.api.getAuthMe.mockClear();
    window.matchMedia = vi.fn(() => ({ matches: false, addEventListener: vi.fn() }));
    await import('../../public/js/app.js?demo-backup-transfer');
    await flush();
    expect(document.querySelector('#btn-sign-out').hidden).toBe(true);
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
    expect(mocks.api.getAuthMe).not.toHaveBeenCalled();
    document.querySelector('#btn-close-data-modal').click();
    expect(document.activeElement).toBe(document.querySelector('#btn-account-menu'));
    document.body.dataset.demo = 'false';
  });

  it('connects state, folder choice, backup now, theme, sign out and revoke', async () => {
    document.body.dataset.demo = 'false';
    document.body.innerHTML = accountDom();
    mocks.api.getJobCounts.mockResolvedValue({ all: 0, waiting: 0, ongoing: 0, accepted: 0, rejected: 0 });
    mocks.api.getMeetings.mockResolvedValue([]);
    mocks.api.getJobs.mockResolvedValue([]);
    mocks.api.getTechnologies.mockResolvedValue([]);
    mocks.api.getSearchPeriods.mockResolvedValue([]);
    mocks.api.getCvVersions.mockResolvedValue([]);
    mocks.api.getAuthMe.mockResolvedValue({ id: 'u1', email: 'a@example.com', name: 'A', picture_url: '' });
    mocks.api.getGoogleStatus.mockResolvedValue({ configured: true, connected: true, picker_api_key: 'public-key', picker_app_id: '12345', folder_name: '', last_status: 'ok' });
    mocks.api.getGooglePickerToken.mockResolvedValue({ access_token: 'short-lived-token' });
    mocks.api.setGoogleFolder.mockResolvedValue({ configured: true, connected: true, folder_id: 'folder-1', folder_name: 'Backups', last_status: 'ok' });
    mocks.api.backupNowGoogle.mockResolvedValue({ configured: true, connected: true, folder_name: 'Backups', last_status: 'ok', last_backup_at: '2026-01-01T00:00:00Z' });
    mocks.api.revokeGoogleDrive.mockResolvedValue({ success: true });
    mocks.api.signOut.mockResolvedValue({ success: true });
    mocks.confirmAction.mockResolvedValue(true);
    window.matchMedia = vi.fn(() => ({ matches: false, addEventListener: vi.fn() }));
    window.location.reload = vi.fn();

    await import('../../public/js/app.js');
    await flush();

    expect(document.querySelector('#account-avatar-fallback').textContent).toBe('A');
    expect(document.querySelector('#account-avatar-img').hidden).toBe(true);
    expect(document.querySelector('#btn-sign-out').hidden).toBe(false);

    const panel = document.querySelector('#account-menu-panel');
    document.querySelector('#btn-account-menu').click();
    await flush();
    expect(panel.hidden).toBe(false);
    expect(document.querySelector('#google-account-section').textContent).toContain('Google Drive backup');

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
    expect(document.querySelector('#btn-google-drive-revoke')).not.toBeNull();
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

    mocks.api.getGoogleStatus.mockResolvedValue({ configured: true, connected: true, picker_api_key: 'public-key', picker_app_id: '12345', folder_name: 'Backups', last_status: 'ok' });
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

    document.querySelector('#btn-google-drive-revoke').click();
    await flush();
    expect(mocks.confirmAction).toHaveBeenCalledWith(expect.objectContaining({ danger: true }));
    expect(mocks.api.revokeGoogleDrive).toHaveBeenCalled();
    expect(window.location.reload).toHaveBeenCalled();

    window.location.reload.mockClear();
    document.querySelector('#btn-sign-out').click();
    await flush();
    expect(mocks.api.signOut).toHaveBeenCalled();
    expect(window.location.reload).toHaveBeenCalled();
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
    mocks.api.getAuthMe.mockResolvedValue({ id: 'u2', email: 'b@example.com' });
    mocks.api.getGoogleStatus.mockResolvedValue({ configured: false, connected: false });
    await import('../../public/js/app.js');
    await flush();
    document.querySelector('#btn-account-menu').click();
    await flush();
    expect(document.querySelector('#google-account-section').textContent).toContain('Not configured');

    vi.resetModules();
    document.body.innerHTML = accountDom();
    mocks.api.getGoogleStatus.mockResolvedValue({ configured: true, connected: true, folder_id: 'f1', folder_name: 'Backups', last_status: 'error' });
    mocks.api.setGoogleFolder.mockResolvedValue({ configured: true, connected: true, last_status: 'ok' });
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
    mocks.api.getAuthMe.mockResolvedValue({ id: 'u3', email: 'd@example.com' });
    mocks.api.getGoogleStatus.mockResolvedValue({ configured: true, connected: true, folder_name: 'Backups', last_status: 'ok' });
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
    expect(mocks.showBackupProgress).toHaveBeenCalledWith('Backing up to Google Drive…');
    expect(mocks.backupProgressHandle.error).toHaveBeenCalledWith('Drive is down');

    document.querySelector('#btn-data-management-menu').click();
    await flush();
    mocks.api.revokeGoogleDrive.mockClear();
    document.querySelector('#btn-google-drive-revoke').click();
    await flush();
    expect(mocks.api.revokeGoogleDrive).not.toHaveBeenCalled();
    mocks.confirmAction.mockResolvedValue(true);
    mocks.api.revokeGoogleDrive.mockRejectedValue(new Error('No route'));
    document.querySelector('#btn-google-drive-revoke').click();
    await flush();
    expect(mocks.showToast).toHaveBeenCalledWith('No route', 'error');

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
    mocks.api.getAuthMe.mockResolvedValue({ id: 'u4', email: 'c@example.com' });
    mocks.api.getGoogleStatus.mockResolvedValue({ configured: true, connected: true });
    window.matchMedia = vi.fn(() => ({ matches: false, addEventListener: vi.fn() }));
    await import('../../public/js/app.js');
    await flush();
    document.querySelector('#google-choose-folder').click();
    await flush();
    expect(document.querySelector('#account-avatar-fallback').textContent).toBe('C');
    expect(document.querySelector('#google-folder-status').textContent).toContain('Google Picker is not configured');
  });
});

describe('account identity block', () => {
  it('shows the first name on the pill and the full identity in the panel, preferring a custom avatar', async () => {
    vi.resetModules();
    document.body.dataset.demo = 'false';
    document.body.innerHTML = accountDom();
    mocks.api.getAuthMe.mockResolvedValue({
      id: 'u5', email: 'jordan.lee@example.com', name: '  Jordan Lee  ',
      picture_url: 'https://google.example/photo.jpg', avatar_url: '/api/account/avatar?v=1',
    });
    mocks.api.getGoogleStatus.mockResolvedValue({ configured: true, connected: false });
    window.matchMedia = vi.fn(() => ({ matches: false, addEventListener: vi.fn() }));
    await import('../../public/js/app.js?identity-basic');
    await flush();

    expect(document.querySelector('#account-menu-label').textContent).toBe('Jordan');
    expect(document.querySelector('#account-identity').hidden).toBe(false);
    expect(document.querySelector('#account-identity-name').textContent).toBe('Jordan Lee');
    expect(document.querySelector('#account-identity-email').textContent).toBe('jordan.lee@example.com');
    expect(document.querySelector('#account-identity-avatar-img').src).toContain('/api/account/avatar?v=1');
    expect(document.querySelector('#account-identity-avatar-img').hidden).toBe(false);
    expect(document.querySelector('#btn-remove-avatar').hidden).toBe(false);
  });

  it('falls back from a custom avatar to the Google photo to a fallback initial, and to the email/"Account" label', async () => {
    vi.resetModules();
    document.body.dataset.demo = 'false';
    document.body.innerHTML = accountDom();
    mocks.api.getAuthMe.mockResolvedValue({ id: 'u6', email: 'no-name@example.com', picture_url: 'https://google.example/broken.jpg' });
    mocks.api.getGoogleStatus.mockResolvedValue({ configured: true, connected: false });
    window.matchMedia = vi.fn(() => ({ matches: false, addEventListener: vi.fn() }));
    await import('../../public/js/app.js?identity-fallback');
    await flush();

    expect(document.querySelector('#account-menu-label').textContent).toBe('no-name');
    expect(document.querySelector('#account-identity-name').textContent).toBe('no-name@example.com');
    expect(document.querySelector('#account-identity-avatar-img').hidden).toBe(false);
    expect(document.querySelector('#account-identity-avatar-fallback').hidden).toBe(true);
    expect(document.querySelector('#btn-remove-avatar').hidden).toBe(true);

    const img = document.querySelector('#account-identity-avatar-img');
    img.onerror();
    expect(img.hidden).toBe(true);
    expect(document.querySelector('#account-identity-avatar-fallback').textContent).toBe('N');
    expect(document.querySelector('#account-identity-avatar-fallback').hidden).toBe(false);
  });

  it('falls back to literal defaults when the account fetch fails or the profile has neither a name nor an email', async () => {
    vi.resetModules();
    document.body.dataset.demo = 'false';
    document.body.innerHTML = accountDom();
    mocks.api.getAuthMe.mockRejectedValueOnce(new Error('network down'));
    mocks.api.getGoogleStatus.mockResolvedValue({ configured: true, connected: false });
    window.matchMedia = vi.fn(() => ({ matches: false, addEventListener: vi.fn() }));
    await import('../../public/js/app.js?identity-fetch-fails');
    await flush();

    expect(document.querySelector('#account-menu-label').textContent).toBe('Account');
    expect(document.querySelector('#account-avatar-fallback').textContent).toBe('?');
    expect(document.querySelector('#account-identity').hidden).toBe(true);

    vi.resetModules();
    document.body.innerHTML = accountDom();
    mocks.api.getAuthMe.mockResolvedValueOnce({ id: 'u9' });
    mocks.api.getGoogleStatus.mockResolvedValue({ configured: true, connected: false });
    await import('../../public/js/app.js?identity-sparse');
    await flush();
    expect(document.querySelector('#account-identity-name').textContent).toBe('Account');
    expect(document.querySelector('#account-identity-email').textContent).toBe('');
  });

  it('keeps the identity block hidden and the pill label unchanged in the browser demo', async () => {
    vi.resetModules();
    document.body.dataset.demo = 'true';
    document.body.innerHTML = accountDom();
    mocks.api.getAuthMe.mockClear();
    window.matchMedia = vi.fn(() => ({ matches: false, addEventListener: vi.fn() }));
    await import('../../public/js/app.js?identity-demo');
    await flush();
    expect(mocks.api.getAuthMe).not.toHaveBeenCalled();
    expect(document.querySelector('#account-identity').hidden).toBe(true);
    expect(document.querySelector('#account-menu-label').textContent).toBe('Account');
    document.body.dataset.demo = 'false';
  });

  it('uploads a new avatar via the pencil button and reports upload failures', async () => {
    vi.resetModules();
    document.body.dataset.demo = 'false';
    document.body.innerHTML = accountDom();
    mocks.api.getAuthMe.mockResolvedValue({ id: 'u7', email: 'pat@example.com', name: 'Pat' });
    mocks.api.getGoogleStatus.mockResolvedValue({ configured: true, connected: false });
    mocks.api.uploadAvatar
      .mockResolvedValueOnce({ id: 'u7', email: 'pat@example.com', name: 'Pat', avatar_url: '/api/account/avatar?v=2' })
      .mockRejectedValueOnce(new Error('Choose a PNG or JPEG image'))
      .mockRejectedValueOnce({});
    window.matchMedia = vi.fn(() => ({ matches: false, addEventListener: vi.fn() }));
    await import('../../public/js/app.js?identity-upload');
    await flush();

    const input = document.querySelector('#avatar-upload-file');
    const clickSpy = vi.spyOn(input, 'click');
    document.querySelector('#btn-change-avatar').click();
    expect(clickSpy).toHaveBeenCalled();

    input.dispatchEvent(new Event('change'));
    expect(mocks.api.uploadAvatar).not.toHaveBeenCalled();

    const file = new File(['png'], 'me.png', { type: 'image/png' });
    Object.defineProperty(input, 'files', { configurable: true, value: [file] });
    input.dispatchEvent(new Event('change'));
    await flush();
    expect(mocks.api.uploadAvatar).toHaveBeenCalledWith(file);
    expect(mocks.showToast).toHaveBeenCalledWith('Profile picture updated', 'success');
    expect(document.querySelector('#account-identity-avatar-img').src).toContain('/api/account/avatar?v=2');
    expect(input.value).toBe('');

    input.dispatchEvent(new Event('change'));
    await flush();
    expect(mocks.showToast).toHaveBeenCalledWith('Choose a PNG or JPEG image', 'error');

    Object.defineProperty(input, 'files', { configurable: true, value: [file] });
    input.dispatchEvent(new Event('change'));
    await flush();
    expect(mocks.showToast).toHaveBeenCalledWith('Failed to update profile picture', 'error');
  });

  it('removes a custom avatar and reports removal failures', async () => {
    vi.resetModules();
    document.body.dataset.demo = 'false';
    document.body.innerHTML = accountDom();
    mocks.api.getAuthMe.mockResolvedValue({
      id: 'u8', email: 'sam@example.com', name: 'Sam', avatar_url: '/api/account/avatar?v=3',
    });
    mocks.api.getGoogleStatus.mockResolvedValue({ configured: true, connected: false });
    mocks.api.removeAvatar
      .mockResolvedValueOnce({ id: 'u8', email: 'sam@example.com', name: 'Sam' })
      .mockRejectedValueOnce({});
    window.matchMedia = vi.fn(() => ({ matches: false, addEventListener: vi.fn() }));
    await import('../../public/js/app.js?identity-remove');
    await flush();

    expect(document.querySelector('#btn-remove-avatar').hidden).toBe(false);
    document.querySelector('#btn-remove-avatar').click();
    await flush();
    expect(mocks.api.removeAvatar).toHaveBeenCalled();
    expect(mocks.showToast).toHaveBeenCalledWith('Reverted to your Google photo', 'success');
    expect(document.querySelector('#btn-remove-avatar').hidden).toBe(true);

    document.querySelector('#btn-remove-avatar').hidden = false;
    document.querySelector('#btn-remove-avatar').click();
    await flush();
    expect(mocks.showToast).toHaveBeenCalledWith('Failed to remove profile picture', 'error');
  });
  it('retries a broken custom avatar with the Google photo before showing the initial', async () => {
    vi.resetModules();
    document.body.dataset.demo = 'false';
    document.body.innerHTML = accountDom();
    mocks.api.getAuthMe.mockResolvedValue({
      id: 'u10', email: 'kim@example.com', name: 'Kim',
      picture_url: 'https://google.example/kim.jpg', avatar_url: '/api/account/avatar?v=2',
    });
    mocks.api.getGoogleStatus.mockResolvedValue({ configured: true, connected: false });
    window.matchMedia = vi.fn(() => ({ matches: false, addEventListener: vi.fn() }));
    await import('../../public/js/app.js?identity-avatar-retry');
    await flush();

    const img = document.querySelector('#account-identity-avatar-img');
    expect(img.src).toContain('/api/account/avatar?v=2');
    img.onerror();
    expect(img.src).toBe('https://google.example/kim.jpg');
    expect(img.hidden).toBe(false);
    img.onerror();
    expect(img.hidden).toBe(true);
    expect(document.querySelector('#account-identity-avatar-fallback').hidden).toBe(false);
  });

  it('returns to the sign-in gate when a request reveals an expired session', async () => {
    vi.resetModules();
    document.body.dataset.demo = 'false';
    document.body.innerHTML = accountDom();
    mocks.api.getAuthMe.mockResolvedValue({ id: 'u11', email: 'lee@example.com', name: 'Lee' });
    mocks.api.getGoogleStatus.mockResolvedValue({ configured: true, connected: false });
    window.matchMedia = vi.fn(() => ({ matches: false, addEventListener: vi.fn() }));
    window.location.reload = vi.fn();
    const originalFetch = window.fetch;
    const nativeFetch = vi.fn()
      .mockResolvedValueOnce({ status: 200 })
      .mockResolvedValueOnce({ status: 401 });
    window.fetch = nativeFetch;
    try {
      await import('../../public/js/app.js?identity-expired-session');
      await flush();
      await window.fetch('/api/jobs');
      expect(window.location.reload).not.toHaveBeenCalled();
      await window.fetch('/api/jobs');
      expect(window.location.reload).toHaveBeenCalledTimes(1);
    } finally {
      window.fetch = originalFetch;
    }
  });
});
