
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { showToast } from '../../public/js/utils/toast.js';

describe('toast', () => {
  beforeEach(() => {
    document.body.innerHTML = '';
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('shows toast of different types', () => {
    ['success', 'error', 'info', 'warning'].forEach(type => {
      showToast('message', type);
      const toast = document.body.querySelector('.toast-item');
      expect(toast).not.toBeNull();
      expect(toast.classList.contains(`toast-${type}`)).toBe(true);
      document.body.innerHTML = '';
    });
  });

  it('shows default info toast', () => {
    showToast('msg');
    const toast = document.body.querySelector('.toast-item');
    expect(toast.classList.contains('toast-info')).toBe(true);
  });

  it('renders messages as text', () => {
    showToast('<img src=x onerror=alert(1)>');
    expect(document.querySelector('.toast-message').textContent).toBe('<img src=x onerror=alert(1)>');
    expect(document.querySelector('.toast-message img')).toBeNull();
  });

  it('respects custom duration', () => {
    showToast('msg', 'info', 1000);
    const toast = document.querySelector('.toast-item');
    expect(toast).not.toBeNull();
    vi.advanceTimersByTime(1400);
    expect(document.querySelector('.toast-item')).toBeNull();
  });

  it('auto-dismisses', () => {
    showToast('msg', 'info');
    vi.advanceTimersByTime(2500);
    expect(document.querySelector('.toast-item')).toBeNull();
  });

  it('handles multiple toasts', () => {
    showToast('msg1');
    showToast('msg2');
    const toasts = document.querySelectorAll('.toast-item');
    expect(toasts.length).toBe(2);
  });

  it('uses the animation fallback and removes on transition or fallback timeout', () => {
    vi.stubGlobal('requestAnimationFrame', undefined);
    showToast('first', 'info', 10);
    showToast('second', 'success', 10);
    const [first, second] = document.querySelectorAll('.toast-item');
    vi.advanceTimersByTime(16);
    expect(first.classList.contains('toast-show')).toBe(true);
    vi.advanceTimersByTime(10);
    expect(first.classList.contains('toast-hide')).toBe(true);
    first.dispatchEvent(new Event('transitionend'));
    expect(first.isConnected).toBe(false);
    vi.advanceTimersByTime(300);
    expect(second.isConnected).toBe(false);
    vi.unstubAllGlobals();
  });
});
