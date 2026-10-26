import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { showBackupProgress } from '../../public/js/utils/backupProgress.js';

describe('backupProgress', () => {
  beforeEach(() => {
    document.body.innerHTML = '';
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('renders an active popup with the given message', () => {
    showBackupProgress('Backing up to Google Drive…');
    const panel = document.querySelector('#backup-progress');
    expect(panel).not.toBeNull();
    expect(panel.classList.contains('backup-progress-active')).toBe(true);
    expect(panel.querySelector('.backup-progress-message').textContent).toBe('Backing up to Google Drive…');
  });

  it('reuses the same panel across calls', () => {
    showBackupProgress('first');
    const first = document.querySelector('#backup-progress');
    showBackupProgress('second');
    expect(document.querySelectorAll('#backup-progress').length).toBe(1);
    expect(document.querySelector('#backup-progress')).toBe(first);
  });

  it('recreates the panel if it was removed from the document', () => {
    showBackupProgress('first');
    document.body.innerHTML = '';
    showBackupProgress('second');
    expect(document.querySelectorAll('#backup-progress').length).toBe(1);
  });

  it('resolves to success and clears the active state', () => {
    const progress = showBackupProgress('Backing up…');
    progress.success('Drive backup uploaded');
    const panel = document.querySelector('#backup-progress');
    expect(panel.classList.contains('backup-progress-active')).toBe(false);
    expect(panel.classList.contains('backup-progress-success')).toBe(true);
    expect(panel.querySelector('.backup-progress-message').textContent).toBe('Drive backup uploaded');
  });

  it('resolves to error and clears the active state', () => {
    const progress = showBackupProgress('Backing up…');
    progress.error('Drive backup failed');
    const panel = document.querySelector('#backup-progress');
    expect(panel.classList.contains('backup-progress-active')).toBe(false);
    expect(panel.classList.contains('backup-progress-error')).toBe(true);
    expect(panel.querySelector('.backup-progress-message').textContent).toBe('Drive backup failed');
  });

  it('shows the popup and hides it 10s after settling', () => {
    vi.stubGlobal('requestAnimationFrame', undefined);
    const progress = showBackupProgress('Backing up…');
    vi.advanceTimersByTime(16);
    const panel = document.querySelector('#backup-progress');
    expect(panel.classList.contains('backup-progress-show')).toBe(true);
    progress.success('Done');
    vi.advanceTimersByTime(9999);
    expect(panel.classList.contains('backup-progress-show')).toBe(true);
    vi.advanceTimersByTime(2);
    expect(panel.classList.contains('backup-progress-show')).toBe(false);
  });

  it('dismisses immediately when the close button is clicked', () => {
    vi.stubGlobal('requestAnimationFrame', undefined);
    showBackupProgress('Backing up…');
    vi.advanceTimersByTime(16);
    const panel = document.querySelector('#backup-progress');
    expect(panel.classList.contains('backup-progress-show')).toBe(true);
    panel.querySelector('.backup-progress-close').click();
    expect(panel.classList.contains('backup-progress-show')).toBe(false);
  });

  it('cancels a pending auto-hide when a new operation starts first', () => {
    vi.stubGlobal('requestAnimationFrame', undefined);
    const first = showBackupProgress('first');
    vi.advanceTimersByTime(16);
    first.success('done');
    vi.advanceTimersByTime(5000);
    showBackupProgress('second');
    vi.advanceTimersByTime(16);
    const panel = document.querySelector('#backup-progress');
    vi.advanceTimersByTime(5000);
    expect(panel.classList.contains('backup-progress-show')).toBe(true);
  });
});
