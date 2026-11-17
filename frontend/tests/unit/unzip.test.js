import { describe, expect, it } from 'vitest';
import { isZip, openZip, readError, ZIP_LIMITS } from '../../public/js/utils/unzip.js';
import { createZip } from '../../public/js/utils/zip.js';
import { buildZip } from './zipBuilder.js';

const text = 'War Room '.repeat(200);

/** Returns the error code a promise rejects with. */
async function failure(promise) {
  try {
    await promise;
  } catch (error) {
    return error.code;
  }
  return 'resolved';
}

describe('openZip', () => {
  it('reads stored entries written by War Room', async () => {
    const zip = openZip(createZip([{ name: 'a.txt', data: 'hello' }, { name: 'dir/b.xml', data: '<b/>' }]));
    expect(zip.has('a.txt')).toBe(true);
    expect(zip.has('missing')).toBe(false);
    expect(await zip.text('a.txt')).toBe('hello');
    expect(await zip.text('dir/b.xml')).toBe('<b/>');
  });

  it('inflates deflated entries, including ones sized only by a data descriptor', async () => {
    expect(await openZip(buildZip([{ name: 'x.xml', data: text }])).text('x.xml')).toBe(text);
    expect(await openZip(buildZip([{ name: 'x.xml', data: text }], { descriptor: true, comment: 'saved by an app' })).text('x.xml')).toBe(text);
  });

  it('rejects files that are not complete ZIP archives', () => {
    expect(() => openZip(new Uint8Array(10))).toThrow('unreadable');
    expect(() => openZip(new Uint8Array(100))).toThrow('unreadable');
    const archive = buildZip([{ name: 'x', data: 'x' }]);
    const view = new DataView(archive.buffer);
    view.setUint32(archive.length - 6, 9999, true);
    expect(() => openZip(archive)).toThrow('unreadable');
  });

  it('rejects damaged, encrypted, and unsupported entries', async () => {
    expect(await failure(openZip(buildZip([{ name: 'x', data: text, crc: 1 }])).read('x'))).toBe('unreadable');
    expect(await failure(openZip(buildZip([{ name: 'x', data: text, size: 10 }])).read('x'))).toBe('unreadable');
    expect(await failure(openZip(buildZip([{ name: 'x', data: text, size: 99999 }])).read('x'))).toBe('unreadable');
    expect(await failure(openZip(buildZip([{ name: 'x', data: text, body: new Uint8Array([255, 255, 255]) }])).read('x'))).toBe('unreadable');
    expect(await failure(openZip(buildZip([{ name: 'x', data: 'x', flags: 1 }])).read('x'))).toBe('unreadable');
    expect(await failure(openZip(buildZip([{ name: 'x', data: 'x', method: 12, body: new Uint8Array([1]) }])).read('x'))).toBe('unreadable');
    expect(await failure(openZip(buildZip([{ name: 'x', data: 'x' }])).read('y'))).toBe('unreadable');
  });

  it('rejects an entry whose data or local header lies outside the archive', async () => {
    const archive = buildZip([{ name: 'x', data: 'stored', method: 0 }]);
    const moved = archive.slice();
    new DataView(moved.buffer).setUint32(moved.length - 22 - 46 - 1 + 42, 4000, true);
    expect(await failure(openZip(moved).read('x'))).toBe('unreadable');
    const oversized = archive.slice();
    new DataView(oversized.buffer).setUint32(oversized.length - 22 - 46 - 1 + 20, 4000, true);
    expect(await failure(openZip(oversized).read('x'))).toBe('unreadable');
  });

  it('limits entry count, entry size, and total size', async () => {
    const limits = { ...ZIP_LIMITS, maxEntries: 1, maxEntrySize: 100, maxTotalSize: 150 };
    expect(() => openZip(buildZip([{ name: 'a', data: 'a' }, { name: 'b', data: 'b' }]), limits)).toThrow('tooLarge');
    expect(await failure(openZip(buildZip([{ name: 'a', data: 'a'.repeat(101) }]), limits).read('a'))).toBe('tooLarge');
    const zip = openZip(buildZip([{ name: 'a', data: 'a'.repeat(100) }]), { ...limits, maxEntries: 2 });
    expect(await zip.text('a')).toHaveLength(100);
    expect(await failure(zip.read('a'))).toBe('tooLarge');
  });
});

describe('ZIP helpers', () => {
  it('recognizes a ZIP archive by its first bytes', () => {
    expect(isZip(createZip([]))).toBe(false);
    expect(isZip(createZip([{ name: 'a', data: 'a' }]))).toBe(true);
    expect(isZip(new TextEncoder().encode('Company,Role'))).toBe(false);
    expect(isZip(new Uint8Array(2))).toBe(false);
  });

  it('names the reason a file cannot be read', () => {
    expect(readError('tooLarge')).toMatchObject({ code: 'tooLarge', message: 'tooLarge' });
  });
});
