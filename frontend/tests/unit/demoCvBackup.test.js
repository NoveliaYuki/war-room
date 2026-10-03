import { beforeEach, describe, expect, it, vi } from "vitest";
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
    expect(manifest.version).toBe(2);
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
