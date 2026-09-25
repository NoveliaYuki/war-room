
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { openWithFlip, closeWithFlip, cancelPendingFlipClose } from '../../public/js/flip.js';

describe('flip', () => {
  let sourceEl, targetEl;
  beforeEach(() => {
    sourceEl = document.createElement('div');
    targetEl = document.createElement('div');
    document.body.appendChild(sourceEl);
    document.body.appendChild(targetEl);
    vi.useFakeTimers();
    vi.stubGlobal('requestAnimationFrame', (callback) => callback());
  });

  it('openWithFlip executes flow', () => {
    sourceEl.getBoundingClientRect = vi.fn(() => ({ top: 10, left: 10, width: 100, height: 100 }));
    targetEl.getBoundingClientRect = vi.fn(() => ({ top: 20, left: 20, width: 200, height: 200 }));
    const onComplete = vi.fn();

    openWithFlip(sourceEl, targetEl, document.createElement('div'));
    vi.advanceTimersByTime(400);

    expect(targetEl.classList.contains('modal-animating')).toBe(false);
  });

  it('closeWithFlip executes flow', () => {
    sourceEl.getBoundingClientRect = vi.fn(() => ({ top: 20, left: 20, width: 200, height: 200 }));
    targetEl.getBoundingClientRect = vi.fn(() => ({ top: 10, left: 10, width: 100, height: 100 }));
    const onComplete = vi.fn();

    const backdrop = document.createElement('div');
    openWithFlip(sourceEl, targetEl, backdrop);
    closeWithFlip(targetEl, backdrop, onComplete);
    vi.advanceTimersByTime(300);

    expect(onComplete).toHaveBeenCalled();
  });

  it('uses the fallback close when the source card is gone and cancels pending closes', () => {
    const backdrop = document.createElement('div');
    const complete = vi.fn();
    openWithFlip(sourceEl, targetEl, backdrop);
    sourceEl.remove();
    closeWithFlip(targetEl, backdrop, complete);
    expect(backdrop.classList.contains('active')).toBe(false);
    expect(targetEl.style.opacity).toBe('0');
    cancelPendingFlipClose();
    vi.advanceTimersByTime(201);
    expect(complete).not.toHaveBeenCalled();
    closeWithFlip(targetEl, backdrop, complete);
    vi.advanceTimersByTime(201);
    expect(complete).toHaveBeenCalledOnce();
    expect(targetEl.style.opacity).toBe('');
  });

  it('completes a fallback close when no callback is supplied', () => {
    sourceEl.getBoundingClientRect = vi.fn(() => ({ top: 0, left: 0, width: 80, height: 60 }));
    targetEl.getBoundingClientRect = vi.fn(() => ({ top: 10, left: 10, width: 160, height: 120 }));
    const backdrop = document.createElement('div');
    openWithFlip(sourceEl, targetEl, backdrop);
    sourceEl.remove();

    closeWithFlip(targetEl, backdrop);
    vi.advanceTimersByTime(201);

    expect(backdrop.classList.contains('active')).toBe(false);
    expect(targetEl.style.transition).toBe('');
    expect(targetEl.style.opacity).toBe('');
  });
});
