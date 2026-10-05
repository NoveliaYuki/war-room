import { beforeEach, describe, expect, it, vi } from "vitest";
import { deflateRawSync } from "node:zlib";
import { createZip } from "../../demo/zipWriter.js";

const files = vi.hoisted(() => new Map());
vi.mock("../../demo/fileStore.js", () => ({
  saveFile: vi.fn(async (id, blob) => files.set(String(id), blob)),
  readFile: vi.fn(async (id) => files.get(String(id))),
  deleteFile: vi.fn(async (id) => files.delete(String(id))),
  replaceFiles: vi.fn(async (replacement) => { files.clear(); replacement.forEach((blob, id) => files.set(String(id), blob)); }),
}));

describe("demo CV library backup", () => {
  beforeEach(() => {
    vi.resetModules();
    window.localStorage.clear();
    files.clear();
    const now = new Date();
    const localToday = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}`;
    window.localStorage.setItem("war-room-demo-data-v13", JSON.stringify([{
      id: "demo-cv-job", company_name: "Private Company", position_title: "Engineer", status: "ongoing",
      order_index: 0, stages: [], attachments: [], interviewers: [],
    }]));
    window.localStorage.setItem("war-room-demo-seed-version", "4");
    window.localStorage.setItem("war-room-demo-schedule-date", localToday);
  });

  it("manages canonical technologies, aliases, and assignments in demo storage", async () => {
    const { demoApi } = await import("../../demo/demoStore.js");
    const catalog = await demoApi.getTechnologies();
    const kubernetes = catalog.find((item) => item.name === "Kubernetes");
    expect(kubernetes.aliases).toContain("K8s");
    await expect(demoApi.createTechnology({ name: "K8s", aliases: [] })).rejects.toThrow("already exists");
    const rust = await demoApi.createTechnology({ name: "Rust", aliases: ["rs"] });
    await demoApi.updateJob("demo-cv-job", { technology_ids: [rust.id] });
    expect((await demoApi.getJob("demo-cv-job")).technologies).toEqual([{ id: rust.id, name: "Rust" }]);
    await expect(demoApi.deleteTechnology(rust.id)).rejects.toThrow("remove its assignments first");
    expect(await demoApi.removeTechnologyAssignments(rust.id)).toEqual({ removed: 1 });
    await demoApi.deleteTechnology(rust.id);
    expect((await demoApi.getTechnologies()).some((item) => item.id === rust.id)).toBe(false);
  });

  it("reuses identical file bytes while retaining version events and backup assignments", async () => {
    const { demoApi } = await import("../../demo/demoStore.js");
    const first = await demoApi.uploadCvVersion(new File(["same cv bytes"], "resume.pdf", { type: "application/pdf" }));
    const second = await demoApi.uploadCvVersion(new File(["same cv bytes"], "resume-exported.pdf", { type: "application/pdf" }));
    expect(first.version_number).toBe(1);
    expect(second.version_number).toBe(2);
    expect(first.sha256).toBe(second.sha256);
    expect(files.size).toBe(1);

    await demoApi.setJobCvVersion("demo-cv-job", second.id);
    await demoApi.deleteCvVersion(first.id);
    expect(files.size).toBe(1);
    expect((await demoApi.getJob("demo-cv-job")).selected_cv_version.id).toBe(second.id);

    const archive = await demoApi.exportBackup();
    const manifest = await readManifest(archive);
    expect(manifest.version).toBe(3);
    expect(manifest.cv_versions).toHaveLength(1);
    expect(manifest.cv_versions[0].path).toBe(`cvs/${second.sha256}`);
    expect(manifest.cv_versions[0]).not.toHaveProperty("stored_file_id");
    window.localStorage.removeItem("war-room-demo-cv-versions-v1");
    files.clear();
    await demoApi.importBackup(archive);
    expect((await demoApi.getCvVersions()).map((version) => version.id)).toEqual([second.id]);
    expect((await demoApi.getJob("demo-cv-job")).selected_cv_version.id).toBe(second.id);
    expect(await demoApi.downloadCvVersion(second.id)).toBeInstanceOf(Blob);
    const afterRestore = await demoApi.uploadCvVersion(new File(["a later CV"], "later.pdf", { type: "application/pdf" }));
    expect(afterRestore.version_number).toBe(3);
  });

  it("does not reuse a deleted latest version number", async () => {
    const { demoApi } = await import("../../demo/demoStore.js");
    const first = await demoApi.uploadCvVersion(new File(["first"], "cv.pdf", { type: "application/pdf" }));
    const latest = await demoApi.uploadCvVersion(new File(["latest"], "cv.pdf", { type: "application/pdf" }));
    await demoApi.deleteCvVersion(latest.id);
    const next = await demoApi.uploadCvVersion(new File(["next"], "cv.pdf", { type: "application/pdf" }));
    expect([first.version_number, latest.version_number, next.version_number]).toEqual([1, 2, 3]);
  });

  it("imports backend v2 CV files and legacy backend v1 job backups", async () => {
    const { demoApi } = await import("../../demo/demoStore.js");
    const job = { id: "backend-job", company_name: "Company", position_title: "Engineer", stages: [], attachments: [], interviewers: [] };
    const legacy = { format: "war-room-backup", version: 1, exported_at: new Date().toISOString(), jobs: [job], attachment_sha256: {}, logos: [] };
    const legacyArchive = createZip([["manifest.json", new TextEncoder().encode(JSON.stringify(legacy))]]);
    await demoApi.importBackup(legacyArchive);
    expect(await demoApi.getCvVersions()).toEqual([]);

    const contents = new TextEncoder().encode("backend CV contents");
    const checksum = await digest(contents);
    const version = {
      id: "backend-cv-v1", version_number: 1, original_name: "cv.pdf", file_size: contents.length,
      mime_type: "application/pdf", sha256: checksum, uploaded_at: 1_798_080_000, path: `cvs/${checksum}`,
    };
    const selectedJob = { ...job, selected_cv_version: { ...version, path: undefined } };
    const manifest = { format: "war-room-backup", version: 2, exported_at: new Date().toISOString(), jobs: [selectedJob], attachment_sha256: {}, cv_versions: [version], next_cv_version: 8, logos: [] };
    const archive = createZip([[version.path, contents], ["manifest.json", new TextEncoder().encode(JSON.stringify(manifest))]]);
    await demoApi.importBackup(archive);
    expect(await demoApi.getCvVersions()).toMatchObject([{ id: version.id, version_number: 1, sha256: checksum }]);
    expect((await demoApi.getJob(job.id)).selected_cv_version.id).toBe(version.id);
    expect(await (await demoApi.downloadCvVersion(version.id)).text()).toBe("backend CV contents");
    const next = await demoApi.uploadCvVersion(new File(["next CV contents"], "new.pdf", { type: "application/pdf" }));
    expect(next.version_number).toBe(8);
  });

  it("rejects inconsistent ZIP local headers without changing saved demo data", async () => {
    const { demoApi } = await import("../../demo/demoStore.js");
    const before = await demoApi.getJobs("all");
    const archive = await makeBackupZip();
    const bytes = new Uint8Array(await archive.arrayBuffer());
    const view = new DataView(bytes.buffer);
    const central = findCentralEntry(view, bytes, "manifest.json");
    const local = view.getUint32(central + 42, true);
    view.setUint32(local + 14, view.getUint32(local + 14, true) ^ 1, true);

    await expect(demoApi.importBackup(new Blob([bytes]))).rejects.toThrow(/inconsistent file metadata/);
    expect(await demoApi.getJobs("all")).toEqual(before);
  });

  it("rejects ZIP entries whose expanded sizes exceed the aggregate safety limit", async () => {
    const { demoApi } = await import("../../demo/demoStore.js");
    const archive = await makeBackupZip();
    const bytes = new Uint8Array(await archive.arrayBuffer());
    const view = new DataView(bytes.buffer);
    const central = findCentralEntry(view, bytes, "manifest.json");
    view.setUint32(central + 24, 1024 * 1024 * 1024 + 1, true);

    await expect(demoApi.importBackup(new Blob([bytes]))).rejects.toThrow(/expands beyond the 1 GiB/);
  });

  it("rejects overlapping ZIP payloads and filename aliases", async () => {
    const { demoApi } = await import("../../demo/demoStore.js");
    const archive = await makeBackupZip();
    const bytes = new Uint8Array(await archive.arrayBuffer());
    const view = new DataView(bytes.buffer);
    const manifest = findCentralEntry(view, bytes, "manifest.json");
    const other = findCentralEntry(view, bytes, "unused.bin");
    view.setUint32(other + 42, view.getUint32(manifest + 42, true), true);

    await expect(demoApi.importBackup(new Blob([bytes]))).rejects.toThrow(/inconsistent file metadata|overlapping or invalid/);
  });

  it("rejects ZIP payloads with a checksum mismatch", async () => {
    const { demoApi } = await import("../../demo/demoStore.js");
    const archive = await makeBackupZip();
    const bytes = new Uint8Array(await archive.arrayBuffer());
    const view = new DataView(bytes.buffer);
    const central = findCentralEntry(view, bytes, "manifest.json");
    const local = view.getUint32(central + 42, true);
    const nameLength = view.getUint16(local + 26, true);
    const extraLength = view.getUint16(local + 28, true);
    bytes[local + 30 + nameLength + extraLength] ^= 1;

    await expect(demoApi.importBackup(new Blob([bytes]))).rejects.toThrow(/invalid contents/);
  });

  it("rejects trailing data after the ZIP end-of-directory record", async () => {
    const { demoApi } = await import("../../demo/demoStore.js");
    const archive = await makeBackupZip();
    const bytes = new Uint8Array(await archive.arrayBuffer());
    const corrupted = new Uint8Array(bytes.length + 1);
    corrupted.set(bytes);
    corrupted[corrupted.length - 1] = 1;

    await expect(demoApi.importBackup(new Blob([corrupted]))).rejects.toThrow(/invalid file directory/);
  });

  it("stops inflation when a DEFLATE entry exceeds its declared size", async () => {
    const { demoApi } = await import("../../demo/demoStore.js");
    const manifest = new TextEncoder().encode(JSON.stringify({
      format: "war-room-backup", version: 2, jobs: [], attachment_sha256: {}, logos: [], cv_versions: [],
    }));
    const bomb = makeDeflatedZip("manifest.json", manifest, 8);

    await expect(demoApi.importBackup(bomb)).rejects.toThrow(/declared size limit/);
  });
});

async function digest(bytes) {
  const result = await crypto.subtle.digest("SHA-256", bytes);
  return [...new Uint8Array(result)].map((byte) => byte.toString(16).padStart(2, "0")).join("");
}

async function readManifest(archive) {
  const bytes = new Uint8Array(await archive.arrayBuffer());
  const view = new DataView(bytes.buffer);
  let end = -1;
  for (let offset = bytes.length - 22; offset >= Math.max(0, bytes.length - 65557); offset -= 1) {
    if (view.getUint32(offset, true) === 0x06054b50) { end = offset; break; }
  }
  const count = view.getUint16(end + 10, true);
  let cursor = view.getUint32(end + 16, true);
  for (let index = 0; index < count; index += 1) {
    const nameSize = view.getUint16(cursor + 28, true);
    const extraSize = view.getUint16(cursor + 30, true);
    const commentSize = view.getUint16(cursor + 32, true);
    const name = new TextDecoder().decode(bytes.subarray(cursor + 46, cursor + 46 + nameSize));
    if (name === "manifest.json") {
      const local = view.getUint32(cursor + 42, true);
      const localNameSize = view.getUint16(local + 26, true);
      const localExtraSize = view.getUint16(local + 28, true);
      const start = local + 30 + localNameSize + localExtraSize;
      const length = view.getUint32(cursor + 24, true);
      return JSON.parse(new TextDecoder().decode(bytes.subarray(start, start + length)));
    }
    cursor += 46 + nameSize + extraSize + commentSize;
  }
  throw new Error("manifest not found in test archive");
}

async function makeBackupZip() {
  const manifest = {
    format: "war-room-backup", version: 2, exported_at: new Date().toISOString(),
    jobs: [{ id: "import-job", company_name: "Company", position_title: "Engineer", stages: [], attachments: [], interviewers: [] }],
    attachment_sha256: {}, logos: [], cv_versions: [],
  };
  return createZip([
    ["manifest.json", new TextEncoder().encode(JSON.stringify(manifest))],
    ["unused.bin", new Uint8Array([1, 2, 3])],
  ]);
}

function findCentralEntry(view, bytes, name) {
  let end = -1;
  for (let offset = bytes.length - 22; offset >= Math.max(0, bytes.length - 65557); offset -= 1) {
    if (view.getUint32(offset, true) === 0x06054b50) { end = offset; break; }
  }
  let cursor = view.getUint32(end + 16, true);
  const count = view.getUint16(end + 10, true);
  for (let index = 0; index < count; index += 1) {
    const nameLength = view.getUint16(cursor + 28, true);
    const extraLength = view.getUint16(cursor + 30, true);
    const commentLength = view.getUint16(cursor + 32, true);
    const currentName = new TextDecoder().decode(bytes.subarray(cursor + 46, cursor + 46 + nameLength));
    if (currentName === name) return cursor;
    cursor += 46 + nameLength + extraLength + commentLength;
  }
  throw new Error(`ZIP entry not found: ${name}`);
}

function makeDeflatedZip(name, contents, declaredSize) {
  const nameBytes = new TextEncoder().encode(name);
  const compressed = deflateRawSync(contents);
  const checksum = testCRC32(contents);
  const local = new Uint8Array(30 + nameBytes.length);
  const localView = new DataView(local.buffer);
  localView.setUint32(0, 0x04034b50, true);
  localView.setUint16(4, 20, true);
  localView.setUint16(6, 0x0800, true);
  localView.setUint16(8, 8, true);
  localView.setUint32(14, checksum, true);
  localView.setUint32(18, compressed.length, true);
  localView.setUint32(22, declaredSize, true);
  localView.setUint16(26, nameBytes.length, true);
  local.set(nameBytes, 30);

  const central = new Uint8Array(46 + nameBytes.length);
  const centralView = new DataView(central.buffer);
  centralView.setUint32(0, 0x02014b50, true);
  centralView.setUint16(4, 20, true);
  centralView.setUint16(6, 20, true);
  centralView.setUint16(8, 0x0800, true);
  centralView.setUint16(10, 8, true);
  centralView.setUint32(16, checksum, true);
  centralView.setUint32(20, compressed.length, true);
  centralView.setUint32(24, declaredSize, true);
  centralView.setUint16(28, nameBytes.length, true);
  central.set(nameBytes, 46);

  const directoryOffset = local.length + compressed.length;
  const end = new Uint8Array(22);
  const endView = new DataView(end.buffer);
  endView.setUint32(0, 0x06054b50, true);
  endView.setUint16(8, 1, true);
  endView.setUint16(10, 1, true);
  endView.setUint32(12, central.length, true);
  endView.setUint32(16, directoryOffset, true);
  return new Blob([local, compressed, central, end], { type: "application/zip" });
}

function testCRC32(bytes) {
  let value = 0xffffffff;
  for (const byte of bytes) {
    value ^= byte;
    for (let bit = 0; bit < 8; bit += 1) value = value & 1 ? 0xedb88320 ^ (value >>> 1) : value >>> 1;
  }
  return (value ^ 0xffffffff) >>> 0;
}
