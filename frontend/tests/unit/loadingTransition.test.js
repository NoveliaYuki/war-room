import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { captureLoadingView, markLoadingStarted, revealLoadedContent } from '../../public/js/utils/loadingTransition.js';

const originalAnimate = Object.getOwnPropertyDescriptor(Element.prototype, 'animate');
const originalMatchMedia = window.matchMedia;
let animations;

beforeEach(() => {
  animations = [];
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
  Object.defineProperty(Element.prototype, 'animate', { configurable: true, value: vi.fn((frames, timing) => {
    let finish;
    const animation = { finished: new Promise((resolve) => { finish = resolve; }), cancel: vi.fn(), frames, timing };
    animations.push({ ...animation, finish });
    return animation;
  }) });
  window.matchMedia = vi.fn(() => ({ matches: false }));
  document.body.innerHTML = '<div id="host"><div class="detail-loading-skeleton"><button>Close</button></div></div>';
  document.querySelector('.detail-loading-skeleton').getBoundingClientRect = () => ({ top: 0, left: 0, width: 320, height: 240 });
});

afterEach(() => {
  if (originalAnimate) Object.defineProperty(Element.prototype, 'animate', originalAnimate);
  else delete Element.prototype.animate;
  window.matchMedia = originalMatchMedia;
  vi.restoreAllMocks();
  vi.useRealTimers();
});

describe('loading content transition', () => {
  it('dissolves placeholders before the slower content reveal and cleans up duplicate controls', async () => {
    const host = document.querySelector('#host');
    const clock = vi.spyOn(performance, 'now').mockReturnValue(0);
    markLoadingStarted(host);
    vi.advanceTimersByTime(600);
    clock.mockReturnValue(600);
    const snapshot = captureLoadingView(host);
    host.innerHTML = '<button id="loaded">Loaded content</button>';
    revealLoadedContent(host, snapshot);
    const overlay = host.querySelector('.loading-crossfade-overlay');
    expect(overlay.inert).toBe(true);
    expect(overlay.getAttribute('aria-hidden')).toBe('true');
    expect(document.querySelector('#loaded').closest('[inert]')).toBeNull();
    expect(animations.map(({ frames }) => frames)).toEqual([
      [{ opacity: 0 }, { opacity: 1 }], [{ opacity: 1 }, { opacity: 0 }],
    ]);
    const total = animations[0].timing.duration + animations[0].timing.delay;
    expect(total).toBeGreaterThanOrEqual(160);
    expect(total).toBeLessThanOrEqual(240);
    expect(animations[0].timing.delay).toBeGreaterThan(animations[1].timing.duration * 0.8);
    animations.forEach(({ finish }) => finish());
    await vi.waitFor(() => expect(host.querySelector('.loading-crossfade-overlay')).toBeNull());
    expect(host.textContent).toBe('Loaded content');
    expect(host.classList.contains('loading-content-host')).toBe(false);
  });

  it('never flashes placeholders or runs a fade for a fast request', () => {
    const host = document.querySelector('#host');
    const clock = vi.spyOn(performance, 'now').mockReturnValue(0);
    markLoadingStarted(host);
    vi.advanceTimersByTime(80);
    expect(host.classList.contains('loading-placeholder-ready')).toBe(false);
    clock.mockReturnValue(80);
    const snapshot = captureLoadingView(host);
    host.innerHTML = '<p>Ready</p>';
    revealLoadedContent(host, snapshot);
    vi.advanceTimersByTime(1000);
    expect(snapshot).toBeNull();
    expect(animations).toHaveLength(0);
    expect(host.classList.contains('loading-placeholder-ready')).toBe(false);
    expect(host.textContent).toBe('Ready');
  });

  it('shows content immediately when reduced motion is selected', () => {
    window.matchMedia = vi.fn(() => ({ matches: true }));
    const host = document.querySelector('#host');
    const clock = vi.spyOn(performance, 'now').mockReturnValue(0);
    markLoadingStarted(host);
    vi.advanceTimersByTime(600);
    clock.mockReturnValue(600);
    const snapshot = captureLoadingView(host);
    host.innerHTML = '<p>Loaded</p>';
    revealLoadedContent(host, snapshot);
    expect(snapshot).toBeNull();
    expect(animations).toHaveLength(0);
    expect(host.querySelector('.loading-crossfade-overlay')).toBeNull();
  });

  it('does not fade ordinary edits that have no loading template', () => {
    const host = document.querySelector('#host');
    host.innerHTML = '<p>Existing content</p>';
    expect(captureLoadingView(host)).toBeNull();
  });

  it('keeps the slow-load handoff brief and responsive to loading time', () => {
    const clock = vi.spyOn(performance, 'now');
    const host = document.querySelector('#host');
    const totals = [400, 1000, 4000].map((loadTime) => {
      clock.mockReturnValue(0);
      markLoadingStarted(host);
      vi.advanceTimersByTime(loadTime);
      clock.mockReturnValue(loadTime);
      const snapshot = captureLoadingView(host);
      expect(snapshot.loadDurationMs).toBe(loadTime);
      revealLoadedContent(host, snapshot);
      const { timing } = animations.at(-2);
      return timing.duration + timing.delay;
    });
    expect(totals[0]).toBeLessThan(totals[1]);
    expect(totals[1]).toBeLessThan(totals[2]);
    expect(totals[0]).toBeLessThan(200);
    expect(totals[2]).toBeLessThanOrEqual(240);
  });

  it('shows content without a fade when animation is unavailable', () => {
    const host = document.querySelector('#host');
    const clock = vi.spyOn(performance, 'now').mockReturnValue(0);
    markLoadingStarted(host);
    vi.advanceTimersByTime(600);
    clock.mockReturnValue(600);
    host.animate = undefined;
    expect(captureLoadingView(host)).toBeNull();
    delete host.animate;
  });

  it('shows content without a fade when the loading template is already gone', () => {
    const host = document.querySelector('#host');
    const clock = vi.spyOn(performance, 'now').mockReturnValue(0);
    markLoadingStarted(host);
    vi.advanceTimersByTime(600);
    clock.mockReturnValue(600);
    host.innerHTML = '<p>Ready</p>';
    expect(captureLoadingView(host)).toBeNull();
  });

  it('shows content without a fade when the loading view has no size', () => {
    const host = document.querySelector('#host');
    document.querySelector('.detail-loading-skeleton').getBoundingClientRect = () => ({ top: 0, left: 0, width: 0, height: 0 });
    const clock = vi.spyOn(performance, 'now').mockReturnValue(0);
    markLoadingStarted(host);
    vi.advanceTimersByTime(600);
    clock.mockReturnValue(600);
    expect(captureLoadingView(host)).toBeNull();
  });

  it('keeps the handoff when matchMedia is unavailable', () => {
    window.matchMedia = undefined;
    const host = document.querySelector('#host');
    const clock = vi.spyOn(performance, 'now').mockReturnValue(0);
    markLoadingStarted(host);
    vi.advanceTimersByTime(600);
    clock.mockReturnValue(600);
    const snapshot = captureLoadingView(host);
    expect(snapshot).not.toBeNull();
    expect(snapshot.loadDurationMs).toBe(600);
  });

  it('shows content without a fade when the placeholder never became visible', () => {
    const host = document.querySelector('#host');
    const clock = vi.spyOn(performance, 'now').mockReturnValue(0);
    markLoadingStarted(host);
    vi.advanceTimersByTime(600);
    host.classList.remove('loading-placeholder-ready');
    clock.mockReturnValue(600);
    const snapshot = captureLoadingView(host);
    host.innerHTML = '<p>Ready</p>';
    revealLoadedContent(host, snapshot);
    expect(snapshot).toBeNull();
    expect(animations).toHaveLength(0);
  });

  it('never shows placeholder feedback after the loading template is removed', () => {
    const host = document.querySelector('#host');
    markLoadingStarted(host);
    host.innerHTML = '<p>plain</p>';
    vi.advanceTimersByTime(500);
    expect(host.classList.contains('loading-placeholder-ready')).toBe(false);
  });

  it('restarts tracking when the skeleton is replaced before the feedback delay', () => {
    const host = document.querySelector('#host');
    const clock = vi.spyOn(performance, 'now').mockReturnValue(0);
    markLoadingStarted(host);
    host.innerHTML = '<p>plain</p>';
    clock.mockReturnValue(100);
    markLoadingStarted(host);
    host.innerHTML = '<div class="board-loading-state">Board</div>';
    document.querySelector('.board-loading-state').getBoundingClientRect = () => ({ top: 0, left: 0, width: 320, height: 240 });
    vi.advanceTimersByTime(300);
    expect(host.classList.contains('loading-placeholder-ready')).toBe(true);
    clock.mockReturnValue(700);
    expect(captureLoadingView(host).loadDurationMs).toBe(600);
  });

  it('keeps the content host marked while another loading overlay remains', async () => {
    const host = document.querySelector('#host');
    const clock = vi.spyOn(performance, 'now').mockReturnValue(0);
    markLoadingStarted(host);
    vi.advanceTimersByTime(600);
    clock.mockReturnValue(600);
    const snapshot = captureLoadingView(host);
    host.innerHTML = '<button id="loaded">Loaded content</button>';
    revealLoadedContent(host, snapshot);
    const extra = document.createElement('div');
    extra.className = 'loading-crossfade-overlay';
    host.append(extra);
    animations.forEach(({ finish }) => finish());
    await vi.waitFor(() => expect(host.querySelectorAll('.loading-crossfade-overlay')).toHaveLength(1));
    expect(host.classList.contains('loading-content-host')).toBe(true);
    expect(host.querySelector('#loaded')).not.toBeNull();
  });

  it('includes bootstrap loading time when the same skeleton is refreshed', () => {
    const clock = vi.spyOn(performance, 'now').mockReturnValue(100);
    const host = document.querySelector('#host');
    markLoadingStarted(host);
    vi.advanceTimersByTime(200);
    expect(host.classList.contains('loading-placeholder-ready')).toBe(false);
    clock.mockReturnValue(500);
    markLoadingStarted(host);
    vi.advanceTimersByTime(50);
    expect(host.classList.contains('loading-placeholder-ready')).toBe(true);
    clock.mockReturnValue(1300);
    expect(captureLoadingView(host).loadDurationMs).toBe(1200);
  });
});
