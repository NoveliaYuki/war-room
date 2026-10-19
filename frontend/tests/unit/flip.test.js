import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { openWithFlip, closeWithFlip, cancelPendingFlipClose } from '../../public/js/flip.js';

describe('modal visibility without card transitions', () => {
  let source, modal, backdrop;
  beforeEach(() => {
    document.body.innerHTML = '<div id="source"></div><div id="backdrop"><div id="modal"></div></div>';
    source = document.querySelector('#source');
    modal = document.querySelector('#modal');
    backdrop = document.querySelector('#backdrop');
    vi.useFakeTimers();
  });
  afterEach(() => vi.useRealTimers());

  it('shows the modal immediately without scheduling a transform animation', () => {
    openWithFlip(source, modal, backdrop);
    expect(backdrop.classList.contains('active')).toBe(true);
    expect(modal.style.transform).toBe('');
    expect(modal.classList.contains('modal-animating')).toBe(false);
    expect(vi.getTimerCount()).toBe(0);
  });

  it('finishes closing immediately even if the source card was removed', () => {
    openWithFlip(source, modal, backdrop);
    source.remove();
    const complete = vi.fn();
    closeWithFlip(modal, backdrop, complete);
    expect(backdrop.classList.contains('active')).toBe(false);
    expect(complete).toHaveBeenCalledOnce();
    expect(modal.style.transform).toBe('');
    expect(vi.getTimerCount()).toBe(0);
  });

  it('keeps a newly opened modal visible after closing the previous one', () => {
    openWithFlip(source, modal, backdrop);
    closeWithFlip(modal, backdrop);
    cancelPendingFlipClose(true);
    openWithFlip(source, modal, backdrop);
    vi.runAllTimers();
    expect(backdrop.classList.contains('active')).toBe(true);
    expect(modal.style.opacity).toBe('');
  });
});
