import { afterEach, describe, expect, it, vi } from 'vitest';
import { saveBlob } from '../../public/js/utils/download.js';

afterEach(() => {
  vi.restoreAllMocks();
  vi.useRealTimers();
});

describe('saveBlob', () => {
  it('downloads through a temporary link and releases the object URL', () => {
    vi.useFakeTimers();
    const createObjectURL = vi.fn(() => 'blob:sheet');
    const revokeObjectURL = vi.fn();
    vi.stubGlobal('URL', Object.assign(URL, { createObjectURL, revokeObjectURL }));
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function record() {
      expect(this.download).toBe('board.xlsx');
      expect(this.getAttribute('href')).toBe('blob:sheet');
      expect(this.isConnected).toBe(true);
    });
    const blob = new Blob(['x']);
    saveBlob(blob, 'board.xlsx');
    expect(createObjectURL).toHaveBeenCalledWith(blob);
    expect(click).toHaveBeenCalledOnce();
    expect(document.querySelector('a[download]')).toBeNull();
    vi.advanceTimersByTime(1000);
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:sheet');
  });
});
