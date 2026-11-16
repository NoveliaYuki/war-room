/**
 * @fileoverview Test helper that reads a stored (uncompressed) ZIP archive through its central directory,
 * checking each local header against its directory entry.
 */

import { crc32 } from '../../public/js/utils/zip.js';

const decoder = new TextDecoder();

function readEntry(bytes, view, position) {
  if (view.getUint32(position, true) !== 0x02014b50) throw new Error(`no central directory entry at ${position}`);
  const nameLength = view.getUint16(position + 28, true);
  const entry = {
    flags: view.getUint16(position + 8, true),
    method: view.getUint16(position + 10, true),
    crc: view.getUint32(position + 16, true),
    size: view.getUint32(position + 24, true),
    offset: view.getUint32(position + 42, true),
    name: decoder.decode(bytes.subarray(position + 46, position + 46 + nameLength)),
  };
  return { entry, next: position + 46 + nameLength + view.getUint16(position + 30, true) + view.getUint16(position + 32, true) };
}

function readData(bytes, view, entry) {
  if (view.getUint32(entry.offset, true) !== 0x04034b50) throw new Error(`no local header for ${entry.name}`);
  if (view.getUint32(entry.offset + 14, true) !== entry.crc) throw new Error(`local CRC differs for ${entry.name}`);
  const start = entry.offset + 30 + view.getUint16(entry.offset + 26, true) + view.getUint16(entry.offset + 28, true);
  const data = bytes.subarray(start, start + entry.size);
  if (crc32(data) !== entry.crc) throw new Error(`CRC mismatch for ${entry.name}`);
  return data;
}

/**
 * Reads every entry of an archive.
 *
 * @param {Uint8Array} bytes - Archive bytes.
 * @returns {{entries: object[], files: Map<string, string>}} Directory entries and each file decoded as UTF-8.
 */
export function readZip(bytes) {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  const end = bytes.length - 22;
  if (view.getUint32(end, true) !== 0x06054b50) throw new Error('no end of central directory record');
  const count = view.getUint16(end + 10, true);
  let position = view.getUint32(end + 16, true);
  if (position + view.getUint32(end + 12, true) !== end) throw new Error('central directory size is wrong');
  const entries = [];
  const files = new Map();
  for (let index = 0; index < count; index += 1) {
    const { entry, next } = readEntry(bytes, view, position);
    entries.push(entry);
    files.set(entry.name, decoder.decode(readData(bytes, view, entry)));
    position = next;
  }
  return { entries, files };
}
