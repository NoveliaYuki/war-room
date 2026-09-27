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
});

describe('shared frontend boot', () => {
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
});
