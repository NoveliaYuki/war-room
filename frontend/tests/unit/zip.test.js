import { describe, expect, it } from 'vitest';
import { crc32, createZip } from '../../public/js/utils/zip.js';
import { readZip } from './zipReader.js';

const encode = (text) => new TextEncoder().encode(text);

describe('crc32', () => {
  it('matches the standard check values', () => {
    expect(crc32(new Uint8Array())).toBe(0);
    expect(crc32(encode('123456789'))).toBe(0xcbf43926);
    expect(crc32(encode('The quick brown fox jumps over the lazy dog'))).toBe(0x414fa339);
    expect(crc32(new Uint8Array([0xff, 0x00, 0x80]))).toBeGreaterThanOrEqual(0);
  });
});

describe('createZip', () => {
  const modified = new Date(2026, 10, 16, 13, 45, 31);

  it('writes stored entries that a central directory reader can open', () => {
    const binary = new Uint8Array([0, 1, 2, 250]);
    const archive = createZip([{ name: 'a.txt', data: 'héllo' }, { name: 'dir/b.bin', data: binary }, { name: 'empty', data: '' }], modified);
    expect([...archive.slice(0, 4)]).toEqual([0x50, 0x4b, 0x03, 0x04]);
    const { entries, files } = readZip(archive);
    expect(entries.map((entry) => entry.name)).toEqual(['a.txt', 'dir/b.bin', 'empty']);
    expect(entries.every((entry) => entry.method === 0 && entry.flags === 0x0800)).toBe(true);
    expect(files.get('a.txt')).toBe('héllo');
    expect(entries[0].size).toBe(6);
    expect(entries[1].crc).toBe(crc32(binary));
    expect(files.get('empty')).toBe('');
  });

  it('records the local modification time in MS-DOS format', () => {
    const view = new DataView(createZip([{ name: 'x', data: 'x' }], modified).buffer);
    const time = view.getUint16(10, true);
    const date = view.getUint16(12, true);
    expect([time >> 11, (time >> 5) & 63, (time & 31) * 2]).toEqual([13, 45, 30]);
    expect([(date >> 9) + 1980, (date >> 5) & 15, date & 31]).toEqual([2026, 11, 16]);
    const early = new DataView(createZip([{ name: 'x', data: 'x' }], new Date(1975, 0, 1)).buffer);
    expect(early.getUint16(12, true) >> 9).toBe(0);
  });

  it('writes an empty archive as a lone end record', () => {
    const archive = createZip([]);
    expect(archive).toHaveLength(22);
    expect(readZip(archive).entries).toEqual([]);
  });
});
