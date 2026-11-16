import { beforeEach, describe, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({
  useApiAdapter: vi.fn(),
  demoApi: { getJobs: vi.fn() },
}));

vi.mock('../../public/js/app.js', () => ({}));
vi.mock('../../public/js/api.js', () => ({ useApiAdapter: mocks.useApiAdapter }));
vi.mock('../../demo/demoStore.js', () => ({ demoApi: mocks.demoApi }));

beforeEach(() => {
  vi.resetModules();
  mocks.useApiAdapter.mockClear();
  window.history.replaceState({}, '', '/');
  delete document.body.dataset.demo;
  window.localStorage.clear();
  document.body.removeAttribute('style');
  document.documentElement.style.overflow = '';
  document.body.innerHTML = '<div class="app-container"><button id="tab-board" class="active" aria-pressed="true"></button><button id="tab-schedule" aria-pressed="false"></button><div id="process-toolbar"></div><div id="cards-grid"><div class="board-loading-state"></div></div></div><div id="modal-backdrop"><div id="detail-modal" tabindex="-1"></div></div><div id="sign-in-screen" hidden><p id="sign-in-message"></p></div>';
});

describe('shared frontend boot', () => {
  it('shows a saved job skeleton before runtime config resolves and allows closing it', async () => {
    window.localStorage.setItem('war-room.open-job', 'saved-job');
    let finishConfig;
    vi.stubGlobal('fetch', vi.fn((url) => {
      if (url === '/runtime-config.json') return new Promise((resolve) => { finishConfig = resolve; });
      return Promise.resolve({ ok: false }); // No session; the sign-in gate renders harmlessly without blocking this test.
    }));
    const boot = import('../../public/js/boot.js');
    await vi.waitFor(() => expect(document.querySelector('.detail-loading-skeleton')).not.toBeNull());
    expect(document.querySelector('#modal-backdrop').classList.contains('active')).toBe(true);
    expect(document.body.style.position).toBe('fixed');
    document.querySelector('.modal-close-btn').click();
    expect(window.localStorage.getItem('war-room.open-job')).toBeNull();
    expect(document.querySelector('#modal-backdrop').classList.contains('active')).toBe(false);
    finishConfig({ ok: false });
    await boot;
    vi.unstubAllGlobals();
  });

  it('shows the saved schedule skeleton before runtime config resolves', async () => {
    window.localStorage.setItem('war-room.active-filter', 'schedule');
    let finishConfig;
    vi.stubGlobal('fetch', vi.fn((url) => {
      if (url === '/runtime-config.json') return new Promise((resolve) => { finishConfig = resolve; });
      return Promise.resolve({ ok: false }); // No session; the sign-in gate renders harmlessly without blocking this test.
    }));
    const boot = import('../../public/js/boot.js');
    await vi.waitFor(() => expect(document.querySelector('.schedule-loading-state')).not.toBeNull());
    expect(document.querySelector('#tab-schedule').getAttribute('aria-pressed')).toBe('true');
    expect(document.querySelector('#process-toolbar').hidden).toBe(true);
    finishConfig({ ok: false });
    await boot;
    vi.unstubAllGlobals();
  });

  it('keeps the backend adapter for the local app', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ mode: 'backend' }) }));
    await import('../../public/js/boot.js');
    expect(document.body.dataset.demo).toBe('false');
    expect(mocks.useApiAdapter).not.toHaveBeenCalled();
    vi.unstubAllGlobals();
  });

  it('selects the browser adapter from static runtime config', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ mode: 'demo' }) }));
    await import('../../public/js/boot.js');
    expect(document.body.dataset.demo).toBe('true');
    expect(mocks.useApiAdapter).toHaveBeenCalledWith(mocks.demoApi);
    vi.unstubAllGlobals();
  });

  it('selects the same browser adapter for the local demo query', async () => {
    window.history.replaceState({}, '', '/?demo');
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false }));
    await import('../../public/js/boot.js');
    expect(document.body.dataset.demo).toBe('true');
    expect(mocks.useApiAdapter).toHaveBeenCalledWith(mocks.demoApi);
    vi.unstubAllGlobals();
  });

  it('retains the local app when runtime config cannot be loaded', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('offline')));
    await import('../../public/js/boot.js');
    expect(document.body.dataset.demo).toBe('false');
    expect(mocks.useApiAdapter).not.toHaveBeenCalled();
    vi.unstubAllGlobals();
  });

  it('shows the sign-in gate and never fetches app data when there is no backend session', async () => {
    vi.stubGlobal('fetch', vi.fn((url) => {
      if (url === '/runtime-config.json') return Promise.resolve({ ok: true, json: async () => ({ mode: 'backend' }) });
      if (url === '/api/auth/me') return Promise.resolve({ ok: false });
      throw new Error(`Unexpected fetch: ${url}`);
    }));
    await import('../../public/js/boot.js');
    expect(document.querySelector('.app-container').hidden).toBe(true);
    expect(document.querySelector('#sign-in-screen').hidden).toBe(false);
    vi.unstubAllGlobals();
  });

  it('shows the authorization-failed message after a failed Google redirect', async () => {
    window.history.replaceState({}, '', '/?google=error');
    vi.stubGlobal('fetch', vi.fn((url) => {
      if (url === '/runtime-config.json') return Promise.resolve({ ok: true, json: async () => ({ mode: 'backend' }) });
      if (url === '/api/auth/me') return Promise.resolve({ ok: false });
      throw new Error(`Unexpected fetch: ${url}`);
    }));
    await import('../../public/js/boot.js');
    expect(document.querySelector('#sign-in-screen').hidden).toBe(false);
    expect(document.querySelector('#sign-in-message').textContent).toContain("isn't authorized");
    vi.unstubAllGlobals();
  });

  it('hides the sign-in gate and keeps the app shell visible when a backend session exists', async () => {
    vi.stubGlobal('fetch', vi.fn((url) => {
      if (url === '/runtime-config.json') return Promise.resolve({ ok: true, json: async () => ({ mode: 'backend' }) });
      if (url === '/api/auth/me') return Promise.resolve({ ok: true, json: async () => ({ id: 'u1', email: 'a@example.com' }) });
      throw new Error(`Unexpected fetch: ${url}`);
    }));
    await import('../../public/js/boot.js');
    expect(document.querySelector('.app-container').hidden).toBe(false);
    expect(document.querySelector('#sign-in-screen').hidden).toBe(true);
    vi.unstubAllGlobals();
  });

  it('never checks the backend session in demo mode', async () => {
    window.history.replaceState({}, '', '/?demo');
    const fetchSpy = vi.fn().mockResolvedValue({ ok: false });
    vi.stubGlobal('fetch', fetchSpy);
    await import('../../public/js/boot.js');
    expect(fetchSpy).not.toHaveBeenCalledWith('/api/auth/me', expect.anything());
    expect(document.querySelector('.app-container').hidden).toBe(false);
    expect(document.querySelector('#sign-in-screen').hidden).toBe(true);
    vi.unstubAllGlobals();
  });
});
