const encoder = new TextEncoder();

const crcTable = Uint32Array.from({ length: 256 }, (_, index) => {
  let value = index;
  for (let bit = 0; bit < 8; bit += 1) value = value & 1 ? 0xedb88320 ^ (value >>> 1) : value >>> 1;
  return value >>> 0;
});

function crc32(bytes) {
  let value = 0xffffffff;
  for (const byte of bytes) value = crcTable[(value ^ byte) & 255] ^ (value >>> 8);
  return (value ^ 0xffffffff) >>> 0;
}

function header(length, fields) {
  const bytes = new Uint8Array(length);
  const view = new DataView(bytes.buffer);
  for (const [offset, size, value] of fields) {
    if (size === 2) view.setUint16(offset, value, true);
    else view.setUint32(offset, value, true);
  }
  return bytes;
}

/** Writes bounded, uncompressed ZIP entries accepted by the application importer. */
export function createZip(entries) {
  if (entries.length > 10001) throw new Error("The backup contains too many files.");
  const chunks = [];
  const directory = [];
  let offset = 0;
  for (const [name, data] of entries) {
    const nameBytes = encoder.encode(name);
    if (nameBytes.length > 65535 || data.length > 50 * 1024 * 1024 || name.startsWith("/") || name.split("/").includes("..")) {
      throw new Error("The backup contains an invalid file.");
    }
    const checksum = crc32(data);
    const local = header(30, [[0, 4, 0x04034b50], [4, 2, 20], [6, 2, 0x800], [14, 4, checksum], [18, 4, data.length], [22, 4, data.length], [26, 2, nameBytes.length]]);
    const central = header(46, [[0, 4, 0x02014b50], [4, 2, 20], [6, 2, 20], [8, 2, 0x800], [16, 4, checksum], [20, 4, data.length], [24, 4, data.length], [28, 2, nameBytes.length], [42, 4, offset]]);
    chunks.push(local, nameBytes, data);
    directory.push(central, nameBytes);
    offset += local.length + nameBytes.length + data.length;
  }
  const directoryLength = directory.reduce((size, chunk) => size + chunk.length, 0);
  const end = header(22, [[0, 4, 0x06054b50], [8, 2, entries.length], [10, 2, entries.length], [12, 4, directoryLength], [16, 4, offset]]);
  const blob = new Blob([...chunks, ...directory, end], { type: "application/zip" });
  if (blob.size > 500 * 1024 * 1024) throw new Error("The backup exceeds the 500 MiB ZIP limit.");
  return blob;
}
