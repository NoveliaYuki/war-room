/**
 * @fileoverview Writes uncompressed (STORE) ZIP archives, enough for an Office Open XML package.
 */

const encoder = new TextEncoder();
const CRC_TABLE = Array.from({ length: 256 }, (_, index) => {
  let value = index;
  for (let bit = 0; bit < 8; bit += 1) value = value & 1 ? 0xedb88320 ^ (value >>> 1) : value >>> 1;
  return value >>> 0;
});
// General purpose flag bit 11: file names are UTF-8.
const UTF8_NAMES = 0x0800;
const VERSION = 20;

/**
 * Computes the CRC-32 (IEEE 802.3) checksum ZIP uses.
 *
 * @param {Uint8Array} bytes - Data to checksum.
 * @returns {number} The unsigned 32-bit checksum.
 */
export function crc32(bytes) {
  let crc = 0xffffffff;
  for (const byte of bytes) crc = CRC_TABLE[(crc ^ byte) & 0xff] ^ (crc >>> 8);
  return (crc ^ 0xffffffff) >>> 0;
}

/** Packs a local date and time into MS-DOS date and time fields. */
function dosDateTime(date) {
  const year = Math.max(date.getFullYear(), 1980);
  return {
    time: (date.getHours() << 11) | (date.getMinutes() << 5) | Math.floor(date.getSeconds() / 2),
    date: ((year - 1980) << 9) | ((date.getMonth() + 1) << 5) | date.getDate(),
  };
}

/** Returns a little-endian writer over a new buffer of the given size. */
function record(size) {
  const bytes = new Uint8Array(size);
  const view = new DataView(bytes.buffer);
  return { bytes, u16: (offset, value) => view.setUint16(offset, value, true), u32: (offset, value) => view.setUint32(offset, value, true) };
}

/** Writes the fields the local header and central directory entry share, starting at offset. */
function writeCommonFields(writer, offset, entry, stamp) {
  writer.u16(offset, VERSION);
  writer.u16(offset + 2, UTF8_NAMES);
  writer.u16(offset + 4, 0);
  writer.u16(offset + 6, stamp.time);
  writer.u16(offset + 8, stamp.date);
  writer.u32(offset + 10, entry.crc);
  writer.u32(offset + 14, entry.data.length);
  writer.u32(offset + 18, entry.data.length);
  writer.u16(offset + 22, entry.name.length);
}

function localHeader(entry, stamp) {
  const writer = record(30 + entry.name.length);
  writer.u32(0, 0x04034b50);
  writeCommonFields(writer, 4, entry, stamp);
  writer.bytes.set(entry.name, 30);
  return writer.bytes;
}

function centralHeader(entry, stamp) {
  const writer = record(46 + entry.name.length);
  writer.u32(0, 0x02014b50);
  writer.u16(4, VERSION);
  writeCommonFields(writer, 6, entry, stamp);
  writer.u32(42, entry.offset);
  writer.bytes.set(entry.name, 46);
  return writer.bytes;
}

function endOfCentralDirectory(count, size, offset) {
  const writer = record(22);
  writer.u32(0, 0x06054b50);
  writer.u16(8, count);
  writer.u16(10, count);
  writer.u32(12, size);
  writer.u32(16, offset);
  return writer.bytes;
}

/** Joins byte arrays into one. */
function concat(parts) {
  const output = new Uint8Array(parts.reduce((total, part) => total + part.length, 0));
  let offset = 0;
  for (const part of parts) {
    output.set(part, offset);
    offset += part.length;
  }
  return output;
}

/**
 * Builds a ZIP archive whose entries are stored without compression.
 *
 * @param {{name: string, data: string|Uint8Array}[]} files - Entries in archive order; strings are encoded as UTF-8.
 * @param {Date} [modified=new Date()] - Modification time recorded for every entry.
 * @returns {Uint8Array} The archive bytes.
 */
export function createZip(files, modified = new Date()) {
  const stamp = dosDateTime(modified);
  const local = [];
  const entries = [];
  let offset = 0;
  for (const file of files) {
    const data = typeof file.data === "string" ? encoder.encode(file.data) : file.data;
    const entry = { name: encoder.encode(file.name), data, crc: crc32(data), offset };
    const header = localHeader(entry, stamp);
    local.push(header, data);
    entries.push(entry);
    offset += header.length + data.length;
  }
  const central = entries.map((entry) => centralHeader(entry, stamp));
  const centralSize = central.reduce((total, part) => total + part.length, 0);
  return concat([...local, ...central, endOfCentralDirectory(entries.length, centralSize, offset)]);
}
