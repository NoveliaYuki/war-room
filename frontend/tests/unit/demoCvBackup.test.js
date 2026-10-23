import { beforeEach, describe, expect, it, vi } from "vitest";

const files = vi.hoisted(() => new Map());
const demoLogos = vi.hoisted(() => new Map([
  ["bluebird-analytics.svg", '<svg xmlns="http://www.w3.org/2000/svg"/>'],
  ["cedar-systems.svg", '<svg xmlns="http://www.w3.org/2000/svg"/>'],
  ["northstar-labs.svg", '<svg xmlns="http://www.w3.org/2000/svg"/>'],
]));
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
    window.localStorage.setItem("war-room-demo-seed-version", "5");
    vi.stubGlobal("fetch", vi.fn(async (url) => {
      const name = String(url).split("/").at(-1);
      const content = demoLogos.get(name) || '<svg xmlns="http://www.w3.org/2000/svg"/>';
      return new Response(new Blob([content], { type: "image/svg+xml" }), { status: 200 });
    }));
    const now = new Date();
    const localToday = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}`;
    window.localStorage.setItem("war-room-demo-data-v13", JSON.stringify([{
      id: "demo-cv-job", company_name: "Private Company", position_title: "Engineer", status: "ongoing",
      order_index: 0, created_at: Date.now(), updated_at: Date.now(), stages: [], attachments: [], interviewers: [],
    }]));
    window.localStorage.setItem("war-room-demo-schedule-date", localToday);
  });

  it("creates ongoing search periods and assigns matching existing and new jobs", async () => {
    const { demoApi } = await import("../../demo/demoStore.js");
    const period = await demoApi.createSearchPeriod({ name: "Current search", start_date: "2030-01-01", end_date: "" });
    const existing = await demoApi.getJob("demo-cv-job");
    expect(period.end_date).toBe("");
    expect(existing.search_period_id).toBeFalsy();
    await demoApi.updateSearchPeriod(period.id, { name: period.name, start_date: "2000-01-01", end_date: "" });
    expect((await demoApi.getJob("demo-cv-job")).search_period_id).toBe(period.id);
    const created = await demoApi.createJob({ position_title: "New role" });
    expect(created.search_period_id).toBe(period.id);
    const createdWithEmptySelection = await demoApi.createJob({ position_title: "Another new role", search_period_id: "" });
    expect(createdWithEmptySelection.search_period_id).toBe(period.id);
    expect((await demoApi.getSearchPeriods())[0].end_date).toBe("");
  });

  it("adds waiting samples with varied, second-based process and status dates", async () => {
    const { demoApi } = await import("../../demo/demoStore.js");
    const waiting = await demoApi.getJobs("waiting");
    const savedPrivateJob = await demoApi.getJob("demo-cv-job");
    const technologyCatalog = await demoApi.getTechnologies();
    const catalogIDs = new Set(technologyCatalog.map((item) => item.id));

    expect(waiting).toHaveLength(3);
    expect(new Set(waiting.map((job) => job.created_at)).size).toBe(3);
    expect(new Set(waiting.map((job) => job.status_changed_at)).size).toBe(3);
    expect(waiting.every((job) => job.created_at < 1e12 && job.status_changed_at < 1e12)).toBe(true);
    expect(waiting.flatMap((job) => job.technologies).every((item) => catalogIDs.has(item.id))).toBe(true);
    expect(savedPrivateJob.created_at).toBeLessThan(1e12);
    expect(savedPrivateJob.status_changed_at).toBeLessThan(1e12);
  });

  it("moves a CV-only job from waiting to ongoing when its first stage is created", async () => {
    const { demoApi } = await import("../../demo/demoStore.js");
    await demoApi.updateJob("demo-cv-job", { status: "waiting" });
    await demoApi.createStage({ job_id: "demo-cv-job", stage_type: "HR" });
    expect((await demoApi.getJob("demo-cv-job")).status).toBe("ongoing");
  });

  it("manages canonical technologies, aliases, and assignments in demo storage", async () => {
    const { demoApi } = await import("../../demo/demoStore.js");
    const catalog = await demoApi.getTechnologies();
    const kubernetes = catalog.find((item) => item.name === "Kubernetes");
    expect(kubernetes.aliases).toContain("K8s");
    const typescript = catalog.find((item) => item.name === "TypeScript");
    const javascript = catalog.find((item) => item.name === "JavaScript");
    expect(typescript.aliases).toContain("TS");
    expect(javascript.aliases).toContain("JS");
    await demoApi.updateJob("demo-cv-job", { technology_ids: [typescript.id, javascript.id, kubernetes.id] });
    const sampleJob = await demoApi.getJob("demo-cv-job");
    expect(sampleJob.technologies).toContainEqual(expect.objectContaining({ name: "TypeScript", aliases: ["TS"] }));
    expect(sampleJob.technologies).toContainEqual(expect.objectContaining({ name: "JavaScript", aliases: ["JS"] }));
    expect(sampleJob.technologies).toContainEqual(expect.objectContaining({ name: "Kubernetes", aliases: ["K8s"] }));
    await expect(demoApi.createTechnology({ name: "K8s", aliases: [] })).rejects.toThrow("already exists");
    const rust = await demoApi.createTechnology({ name: "Rust", aliases: ["rs"] });
    await demoApi.updateJob("demo-cv-job", { technology_ids: [rust.id] });
    expect((await demoApi.getJob("demo-cv-job")).technologies).toEqual([{ id: rust.id, name: "Rust", aliases: ["rs"] }]);
    await expect(demoApi.deleteTechnology(rust.id)).rejects.toThrow("remove its assignments first");
    expect(await demoApi.removeTechnologyAssignments(rust.id)).toEqual({ removed: 1 });
    await demoApi.deleteTechnology(rust.id);
    expect((await demoApi.getTechnologies()).some((item) => item.id === rust.id)).toBe(false);
  });

  it("reuses identical file bytes while retaining version events and selected assignments", async () => {
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

    await expect(demoApi.exportBackup()).rejects.toThrow(/unavailable in the browser demo/);
    await expect(demoApi.importBackup(new Blob(["unencrypted ZIP"]))).rejects.toThrow(/unavailable in the browser demo/);
    expect((await demoApi.getCvVersions()).map((version) => version.id)).toEqual([second.id]);
    expect((await demoApi.getJob("demo-cv-job")).selected_cv_version.id).toBe(second.id);
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

  it("does not expose plaintext backup transfer in browser demo mode", async () => {
    const { demoApi } = await import("../../demo/demoStore.js");
    const before = await demoApi.getJobs("all");
    await expect(demoApi.exportBackup()).rejects.toThrow(/unavailable in the browser demo/);
    await expect(demoApi.importBackup(new Blob(["PK\x03\x04"]))).rejects.toThrow(/unavailable in the browser demo/);
    expect(await demoApi.getJobs("all")).toEqual(before);
  });
});
