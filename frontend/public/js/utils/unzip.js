/**
 * @fileoverview Reads ZIP archives, such as an .xlsx workbook, in the browser. Entries may be stored
 * or deflated. Every size is bounded, so a crafted archive cannot exhaust memory.
 */

import { crc32 } from "./zip.js";

const END_SIGNATURE = 0x06054b50;
const CENTRAL_SIGNATURE = 0x02014b50;
const LOCAL_SIGNATURE = 0x04034b50;
const END_RECORD_SIZE = 22;
const MAX_COMMENT_SIZE = 0xffff;
const STORED = 0;
const DEFLATED = 8;
const ENCRYPTED_FLAG = 0x0001;
const decoder = new TextDecoder();

/** Default limits: enough for a large board, far below what would strain a phone. */
export const ZIP_LIMITS = Object.freeze({ maxEntries: 500, maxEntrySize: 32 * 1024 * 1024, maxTotalSize: 96 * 1024 * 1024 });

/**
 * Returns an error whose code names why a file could not be read.
 *
 * @param {"unreadable"|"tooLarge"} code - Failure kind.
 * @returns {Error} The error.
 */
export function readError(code) {
  return Object.assign(new Error(code), { code });
}

/**
 * Returns whether bytes start like a ZIP archive.
 *
 * @param {Uint8Array} bytes - File contents.
 * @returns {boolean} True for a local file header signature.
 */
export function isZip(bytes) {
  return bytes.length >= 4 && new DataView(bytes.buffer, bytes.byteOffset, 4).getUint32(0, true) === LOCAL_SIGNATURE;
}

function findEndRecord(view) {
  const last = view.byteLength - END_RECORD_SIZE;
  for (let position = last; position >= Math.max(0, last - MAX_COMMENT_SIZE); position -= 1) {
    if (view.getUint32(position, true) === END_SIGNATURE) return position;
  }
  throw readError("unreadable");
}

function readCentralEntry(bytes, view, position) {
  if (position + 46 > bytes.length || view.getUint32(position, true) !== CENTRAL_SIGNATURE) throw readError("unreadable");
  const nameLength = view.getUint16(position + 28, true);
  const entry = {
    flags: view.getUint16(position + 8, true),
    method: view.getUint16(position + 10, true),
    crc: view.getUint32(position + 16, true),
    compressedSize: view.getUint32(position + 20, true),
    size: view.getUint32(position + 24, true),
    offset: view.getUint32(position + 42, true),
    name: decoder.decode(bytes.subarray(position + 46, position + 46 + nameLength)),
  };
  const next = position + 46 + nameLength + view.getUint16(position + 30, true) + view.getUint16(position + 32, true);
  return { entry, next };
}

function readDirectory(bytes, limits) {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  if (bytes.length < END_RECORD_SIZE) throw readError("unreadable");
  const end = findEndRecord(view);
  const count = view.getUint16(end + 10, true);
  if (count > limits.maxEntries) throw readError("tooLarge");
  const entries = new Map();
  let position = view.getUint32(end + 16, true);
  for (let index = 0; index < count; index += 1) {
    const { entry, next } = readCentralEntry(bytes, view, position);
    entries.set(entry.name, entry);
    position = next;
  }
  return { view, entries };
}

/** Returns the bytes an entry stores, as sized by the central directory (local sizes may be zero). */
function compressedData(bytes, view, entry) {
  if (entry.offset + 30 > bytes.length || view.getUint32(entry.offset, true) !== LOCAL_SIGNATURE) throw readError("unreadable");
  const start = entry.offset + 30 + view.getUint16(entry.offset + 26, true) + view.getUint16(entry.offset + 28, true);
  if (start + entry.compressedSize > bytes.length) throw readError("unreadable");
  return bytes.subarray(start, start + entry.compressedSize);
}

function concat(chunks, size) {
  const output = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) {
    output.set(chunk, offset);
    offset += chunk.length;
  }
  return output;
}

/** Inflates raw DEFLATE data, stopping as soon as the output passes its declared size. */
async function inflate(data, declaredSize) {
  const source = new ReadableStream({ start(controller) { controller.enqueue(data); controller.close(); } });
  const reader = source.pipeThrough(new DecompressionStream("deflate-raw")).getReader();
  const chunks = [];
  let size = 0;
  try {
    for (let part = await reader.read(); !part.done; part = await reader.read()) {
      size += part.value.length;
      if (size > declaredSize) throw readError("unreadable");
      chunks.push(part.value);
    }
  } catch {
    await reader.cancel().catch(() => {});
    throw readError("unreadable");
  }
  return concat(chunks, size);
}

async function entryData(bytes, view, entry) {
  if (entry.flags & ENCRYPTED_FLAG) throw readError("unreadable");
  const data = compressedData(bytes, view, entry);
  if (entry.method === STORED) return data;
  if (entry.method === DEFLATED) return inflate(data, entry.size);
  throw readError("unreadable");
}

/**
 * Opens a ZIP archive and reads its entries on demand.
 *
 * @param {Uint8Array} bytes - Archive bytes.
 * @param {typeof ZIP_LIMITS} [limits=ZIP_LIMITS] - Entry count and size limits.
 * @returns {{has: (name: string) => boolean, read: (name: string) => Promise<Uint8Array>, text: (name: string) => Promise<string>}}
 *   Entry lookup and readers; each read checks the entry's size and CRC-32.
 */
export function openZip(bytes, limits = ZIP_LIMITS) {
  const { view, entries } = readDirectory(bytes, limits);
  let total = 0;
  const read = async (name) => {
    const entry = entries.get(name);
    if (!entry) throw readError("unreadable");
    total += entry.size;
    if (entry.size > limits.maxEntrySize || total > limits.maxTotalSize) throw readError("tooLarge");
    const data = await entryData(bytes, view, entry);
    if (data.length !== entry.size || crc32(data) !== entry.crc) throw readError("unreadable");
    return data;
  };
  return { has: (name) => entries.has(name), read, text: async (name) => decoder.decode(await read(name)) };
}
