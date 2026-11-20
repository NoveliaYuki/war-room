/**
 * @fileoverview Test helper that writes ZIP archives the way spreadsheet apps save them: deflated entries,
 * optionally with sizes only in a trailing data descriptor. Every header field can be overridden to build
 * damaged archives.
 */

import { deflateRawSync } from 'node:zlib';
import { crc32 } from '../../public/js/utils/zip.js';

const encoder = new TextEncoder();
const DATA_DESCRIPTOR = 0x0008;

function concat(parts) {
  const output = new Uint8Array(parts.reduce((total, part) => total + part.length, 0));
  let offset = 0;
  for (const part of parts) {
    output.set(part, offset);
    offset += part.length;
  }
  return output;
}

function record(size) {
  const bytes = new Uint8Array(size);
  return { bytes, view: new DataView(bytes.buffer) };
}

function prepare(file, options) {
  const data = typeof file.data === 'string' ? encoder.encode(file.data) : file.data;
  const method = file.method ?? options.method ?? 8;
  const body = file.body ?? (method === 8 ? new Uint8Array(deflateRawSync(data)) : data);
  return {
    name: encoder.encode(file.name), body, method,
    flags: (file.flags ?? 0) | (options.descriptor ? DATA_DESCRIPTOR : 0),
    crc: file.crc ?? crc32(data), size: file.size ?? data.length,
  };
}

function localHeader(entry, descriptor) {
  const { bytes, view } = record(30 + entry.name.length);
  view.setUint32(0, 0x04034b50, true);
  view.setUint16(4, 20, true);
  view.setUint16(6, entry.flags, true);
  view.setUint16(8, entry.method, true);
  if (!descriptor) {
    view.setUint32(14, entry.crc, true);
    view.setUint32(18, entry.body.length, true);
    view.setUint32(22, entry.size, true);
  }
  view.setUint16(26, entry.name.length, true);
  bytes.set(entry.name, 30);
  return bytes;
}

function descriptorRecord(entry) {
  const { bytes, view } = record(16);
  view.setUint32(0, 0x08074b50, true);
  view.setUint32(4, entry.crc, true);
  view.setUint32(8, entry.body.length, true);
  view.setUint32(12, entry.size, true);
  return bytes;
}

function centralHeader(entry, offset) {
  const { bytes, view } = record(46 + entry.name.length);
  view.setUint32(0, 0x02014b50, true);
  view.setUint16(4, 20, true);
  view.setUint16(6, 20, true);
  view.setUint16(8, entry.flags, true);
  view.setUint16(10, entry.method, true);
  view.setUint32(16, entry.crc, true);
  view.setUint32(20, entry.body.length, true);
  view.setUint32(24, entry.size, true);
  view.setUint16(28, entry.name.length, true);
  view.setUint32(42, offset, true);
  bytes.set(entry.name, 46);
  return bytes;
}

/**
 * Builds a ZIP archive.
 *
 * @param {{name: string, data: string|Uint8Array, method?: number, body?: Uint8Array, flags?: number, crc?: number, size?: number}[]} files
 *   Entries; body replaces the stored bytes, and crc and size replace what the headers declare.
 * @param {{method?: number, descriptor?: boolean, comment?: string}} [options] - Default method (8 deflates), data descriptors, and an archive comment.
 * @returns {Uint8Array} The archive.
 */
export function buildZip(files, options = {}) {
  const parts = [];
  const central = [];
  let offset = 0;
  for (const file of files) {
    const entry = prepare(file, options);
    const header = localHeader(entry, options.descriptor);
    const trailer = options.descriptor ? descriptorRecord(entry) : new Uint8Array();
    central.push(centralHeader(entry, offset));
    parts.push(header, entry.body, trailer);
    offset += header.length + entry.body.length + trailer.length;
  }
  const directory = concat(central);
  const comment = encoder.encode(options.comment ?? '');
  const { bytes: end, view } = record(22 + comment.length);
  view.setUint32(0, 0x06054b50, true);
  view.setUint16(8, files.length, true);
  view.setUint16(10, files.length, true);
  view.setUint32(12, directory.length, true);
  view.setUint32(16, offset, true);
  view.setUint16(20, comment.length, true);
  end.set(comment, 22);
  return concat([...parts, directory, end]);
}
