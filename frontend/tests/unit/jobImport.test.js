import { beforeEach, describe, expect, it, vi } from "vitest";

const { api } = vi.hoisted(() => ({
  api: { previewJobImport: vi.fn(), createTechnology: vi.fn(), getTechnologies: vi.fn() },
}));
vi.mock("../../public/js/api.js", () => ({ api }));

import { applyDraft, bindJobImport, renderJobImportStatus, renderProcessEntry, renderTechnologySuggestions } from "../../public/js/components/jobImport.js";

const draft = {
  position_title: "Engineer", company_name: "Acme", company_overview: "## Role", job_post_url: "https://a.example/job",
  application_url: "", company_domain: "a.example", keyword_note: "Madrid", work_arrangement: "hybrid", employment_type: "permanent",
  salary_min: 50000, salary_max: 60000, salary_currency: "USD", warnings: ["Check this"],
};
const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

function mountForm() {
  document.body.innerHTML = `${renderProcessEntry()}<form id="f" hidden>
    <input type="hidden" name="work_arrangement" /><input type="hidden" name="employment_type" />
    ${renderJobImportStatus()}
    <input name="position_title" /><input name="company_name" /><input name="job_post_url" /><input name="application_url" />
    <textarea name="company_overview"></textarea>
    ${renderTechnologySuggestions()}
    <details><input name="company_domain" /><input name="keyword_note" /><input name="salary_min" /><input name="salary_max" />
      <select name="salary_currency"><option>EUR</option><option>USD</option></select></details>
  </form>`;
  return document.querySelector("#f");
}

async function importLink(_form, url = "https://a.example/job") {
  document.querySelector("#job-import-url").value = url;
  document.querySelector("#job-import-submit").click();
  await flush();
}

describe("job import", () => {
  beforeEach(() => {
    Object.values(api).forEach((mock) => mock.mockReset());
  });

  it("fills the form and selects only recommended technologies", async () => {
    const form = mountForm();
    const controller = bindJobImport(form);
    api.previewJobImport.mockResolvedValue({
      draft,
      technologies: [{ id: "py", name: "Python", recommended: true }, { id: "tf", name: "<b>TF</b>", recommended: false }],
    });
    await importLink(form);
    expect(form.hidden).toBe(false);
    expect(document.querySelector("#process-entry").hidden).toBe(true);
    expect(document.activeElement).toBe(form.elements.position_title);
    expect(form.elements.position_title.value).toBe("Engineer");
    expect(form.elements.salary_min.value).toBe("50000");
    expect(form.elements.salary_currency.value).toBe("USD");
    expect(form.elements.work_arrangement.value).toBe("hybrid");
    expect(form.querySelector("details").open).toBe(true);
    expect(form.querySelector("#job-import-technologies").hidden).toBe(false);
    expect(form.querySelector(".job-import-technology-list b")).toBeNull();
    expect(form.querySelector("#job-import-status").textContent).toContain("Check this");
    await expect(controller.technologyIds()).resolves.toEqual(["py"]);
  });

  it("never replaces what the user typed", () => {
    const form = mountForm();
    form.elements.position_title.value = "Mine";
    form.elements.salary_max.value = "1";
    applyDraft(form, draft);
    expect(form.elements.position_title.value).toBe("Mine");
    expect(form.elements.salary_min.value).toBe("");
    expect(form.elements.salary_currency.value).toBe("EUR");
  });

  it("leaves salary and details untouched when a draft has none", () => {
    const form = mountForm();
    applyDraft(form, { position_title: "Role", salary_min: null, salary_max: null });
    expect(form.elements.salary_min.value).toBe("");
    expect(form.querySelector("details").open).toBe(false);
  });

  it("imports with Enter, warns about duplicates, and ignores empty input", async () => {
    const form = mountForm();
    bindJobImport(form);
    api.previewJobImport.mockResolvedValue({ draft: { ...draft, warnings: [] }, technologies: [], duplicate: { position_title: "Engineer", company_name: "Acme" } });
    const input = document.querySelector("#job-import-url");
    input.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
    expect(api.previewJobImport).not.toHaveBeenCalled();
    input.value = "https://a.example/job";
    input.dispatchEvent(new KeyboardEvent("keydown", { key: "a", bubbles: true }));
    expect(api.previewJobImport).not.toHaveBeenCalled();
    input.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
    await flush();
    expect(form.querySelector("#job-import-status").dataset.tone).toBe("warning");
    expect(form.querySelector("#job-import-status").textContent).toContain("You already track this posting");
  });

  it("keeps the link and points to paste when a site cannot be read", async () => {
    const form = mountForm();
    bindJobImport(form);
    api.previewJobImport.mockRejectedValue(Object.assign(new Error("No automatic imports"), { code: "paste_required" }));
    await importLink(form, "https://www.linkedin.com/jobs/view/1");
    expect(form.elements.job_post_url.value).toBe("https://www.linkedin.com/jobs/view/1");
    expect(form.hidden).toBe(false);
    expect(document.activeElement).toBe(form.elements.company_overview);
    expect(form.querySelector("#job-import-status").textContent).toContain("paste it into the overview");
    expect(document.querySelector("#job-import-submit").disabled).toBe(false);
  });

  it("does not offer paste for an invalid link", async () => {
    const form = mountForm();
    bindJobImport(form);
    api.previewJobImport.mockRejectedValue(Object.assign(new Error("Enter a public link"), { code: "invalid_url" }));
    await importLink(form, "http://a.example");
    expect(form.elements.job_post_url.value).toBe("");
    expect(form.hidden).toBe(true);
    expect(document.querySelector("#job-import-entry-status").textContent).toBe("Enter a public link");
  });

  it("opens an empty form when filling in by hand and drops a pending import", async () => {
    const form = mountForm();
    bindJobImport(form);
    let finishImport;
    api.previewJobImport.mockReturnValueOnce(new Promise((resolve) => { finishImport = resolve; }));
    await importLink(form);
    document.querySelector("#process-entry-manual").click();
    expect(form.hidden).toBe(false);
    expect(document.querySelector("#process-entry").hidden).toBe(true);
    finishImport({ draft, technologies: [] });
    await flush();
    expect(form.elements.position_title.value).toBe("");
  });

  it("ignores a slower earlier import", async () => {
    const form = mountForm();
    bindJobImport(form);
    let finishFirst;
    api.previewJobImport.mockReturnValueOnce(new Promise((resolve, reject) => { finishFirst = reject; }));
    await importLink(form);
    api.previewJobImport.mockResolvedValueOnce({ draft: { ...draft, warnings: [] }, technologies: [] });
    document.querySelector("#job-import-url").dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
    await flush();
    finishFirst(new Error("late failure"));
    await flush();
    expect(form.querySelector("#job-import-status").textContent).not.toContain("late failure");
  });

  it("converts pasted HTML to Markdown and suggests technologies", async () => {
    const form = mountForm();
    const controller = bindJobImport(form);
    api.previewJobImport.mockResolvedValue({ technologies: [{ name: "Go", aliases: ["Golang"], recommended: true }] });
    const overview = form.elements.company_overview;
    overview.setRangeText = vi.fn((text) => { overview.value = text; });
    const event = new Event("paste", { bubbles: true, cancelable: true });
    event.clipboardData = { getData: () => "<h2>Role</h2>" };
    overview.dispatchEvent(event);
    await flush();
    await flush();
    expect(event.defaultPrevented).toBe(true);
    expect(overview.value).toBe("## Role");
    expect(api.previewJobImport).toHaveBeenCalledWith({ description: "## Role" });
    api.createTechnology.mockResolvedValue({ id: "new-go" });
    await expect(controller.technologyIds()).resolves.toEqual(["new-go"]);
    expect(api.createTechnology).toHaveBeenCalledWith({ name: "Go", aliases: ["Golang"] });
  });

  it("lets plain text paste normally and survives suggestion failures", async () => {
    const form = mountForm();
    const controller = bindJobImport(form);
    api.previewJobImport.mockRejectedValue(new Error("offline"));
    const event = new Event("paste", { bubbles: true, cancelable: true });
    form.elements.company_overview.dispatchEvent(event);
    await flush();
    await flush();
    expect(event.defaultPrevented).toBe(false);
    await expect(controller.technologyIds()).resolves.toBeUndefined();
  });

  it("reuses a technology another request already created", async () => {
    const form = mountForm();
    const controller = bindJobImport(form);
    api.previewJobImport.mockResolvedValue({ draft: { ...draft, warnings: [] }, technologies: [{ name: "Go", recommended: true }] });
    await importLink(form);
    api.createTechnology.mockRejectedValue(new Error("exists"));
    api.getTechnologies.mockResolvedValue([{ id: "go", name: "go" }]);
    await expect(controller.technologyIds()).resolves.toEqual(["go"]);
    api.getTechnologies.mockResolvedValue([]);
    await expect(controller.technologyIds()).rejects.toThrow("exists");
  });

  it("binds without the entry step when import is disabled", () => {
    document.body.innerHTML = `<form id="f"><textarea name="company_overview"></textarea>${renderTechnologySuggestions()}</form>`;
    expect(() => bindJobImport(document.querySelector("#f"))).not.toThrow();
  });
});
