import { beforeEach, describe, expect, it, vi } from "vitest";

const { api, toast } = vi.hoisted(() => ({
  api: { getTechnologies: vi.fn(), createTechnology: vi.fn(), updateTechnology: vi.fn(), removeTechnologyAssignments: vi.fn(), deleteTechnology: vi.fn() },
  toast: vi.fn(),
}));
vi.mock("../../public/js/api.js", () => ({ api }));
vi.mock("../../public/js/utils/toast.js", () => ({ showToast: toast }));

import { openTechnologyLibrary } from "../../public/js/components/technologyLibrary.js";

const catalog = [{ id: "k8s", name: "Kubernetes", aliases: ["K8s"], jobs: [{ id: "job-1", company_name: "Example", position_title: "Engineer" }] }];

describe("technology library", () => {
  beforeEach(() => {
    document.body.innerHTML = '<div id="detail-modal"></div>';
    api.getTechnologies.mockReset().mockResolvedValue(catalog);
    api.createTechnology.mockReset().mockResolvedValue({});
    api.updateTechnology.mockReset().mockResolvedValue({});
    api.removeTechnologyAssignments.mockReset().mockResolvedValue({ removed: 1 });
    api.deleteTechnology.mockReset().mockResolvedValue({});
    toast.mockReset();
  });

  it("shows canonical names, aliases, and affected jobs before removing a used technology", async () => {
    const modal = document.querySelector("#detail-modal");
    await openTechnologyLibrary(modal, vi.fn());
    expect(modal.textContent).toContain("Kubernetes");
    expect(modal.textContent).toContain("K8s");
    modal.querySelector(".technology-remove").click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(modal.textContent).toContain("Example — Engineer");
    modal.querySelector(".technology-remove").click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(modal.querySelector(".technology-removal-impact")).toBeNull();
    modal.querySelector(".technology-remove").click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    modal.querySelector(".technology-remove-assignments").click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(api.removeTechnologyAssignments).toHaveBeenCalledWith("k8s");
    expect(api.deleteTechnology).toHaveBeenCalledWith("k8s");
  });

  it("handles empty aliases, unused jobs, and stale catalog actions", async () => {
    const goCatalog = [{ id: "go", name: "Go", aliases: [], jobs: [] }];
    api.getTechnologies.mockResolvedValue(goCatalog);
    const modal = document.querySelector("#detail-modal");
    await openTechnologyLibrary(modal, vi.fn());
    expect(modal.textContent).toContain("No aliases");
    expect(modal.textContent).toContain("0 processes");
    modal.querySelector(".technology-remove").click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(modal.textContent).toContain("Delete technology");
    api.getTechnologies.mockResolvedValue([]);
    const staleButton = document.createElement("button");
    staleButton.className = "technology-edit";
    staleButton.dataset.id = "go";
    modal.querySelector("#technology-library-list").append(staleButton);
    staleButton.click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(modal.querySelector("#technology-name").value).toBe("");
    modal.querySelector("#technology-library-list").dispatchEvent(new Event("click", { bubbles: true }));
  });

  it("lists every process affected by removal", async () => {
    api.getTechnologies.mockResolvedValue([{ id: "go", name: "Go", aliases: [], jobs: [
      { id: "one", company_name: "Acme", position_title: "Engineer" },
      { id: "two", company_name: "Globex", position_title: "Developer" },
    ] }]);
    const modal = document.querySelector("#detail-modal");
    await openTechnologyLibrary(modal, vi.fn());
    modal.querySelector(".technology-remove").click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(modal.textContent).toContain("Used by 2 processes");
    expect(modal.textContent).toContain("Acme — Engineer");
    expect(modal.textContent).toContain("Globex — Developer");
  });

  it("renders legacy catalog entries without optional arrays", async () => {
    api.getTechnologies.mockResolvedValue([{ id: "legacy", name: "Java", jobs: undefined }]);
    const modal = document.querySelector("#detail-modal");
    await openTechnologyLibrary(modal, vi.fn());
    expect(modal.textContent).toContain("Aliases: No aliases");
    expect(modal.textContent).toContain("0 processes");
    modal.querySelector(".technology-edit").click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(modal.querySelector("#technology-aliases").value).toBe("");
  });

  it("uses fallback messages when catalog operations fail without details", async () => {
    api.getTechnologies.mockRejectedValueOnce(new Error(""));
    const modal = document.querySelector("#detail-modal");
    await openTechnologyLibrary(modal, vi.fn());
    expect(modal.querySelector("#technology-library-status").textContent).toBe("Could not load the catalog");
    api.getTechnologies.mockResolvedValue([]);
    modal.querySelector("#technology-retry").click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    api.createTechnology.mockRejectedValueOnce(new Error(""));
    modal.querySelector("#technology-name").value = "Go";
    modal.querySelector("#technology-form").dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(modal.querySelector("#technology-library-status").textContent).toBe("Could not save technology");
    api.getTechnologies.mockResolvedValue([{ id: "unused", name: "Go", aliases: [], jobs: [] }]);
    api.deleteTechnology.mockRejectedValueOnce(new Error(""));
    await openTechnologyLibrary(modal, vi.fn());
    modal.querySelector(".technology-remove").click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    modal.querySelector(".technology-confirm-delete").click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(modal.querySelector("#technology-library-status").textContent).toBe("Could not delete technology");
  });

  it("handles assignment cleanup after its confirmation row is removed", async () => {
    api.getTechnologies.mockResolvedValue(catalog);
    api.removeTechnologyAssignments.mockRejectedValueOnce(new Error(""));
    const modal = document.querySelector("#detail-modal");
    await openTechnologyLibrary(modal, vi.fn());
    modal.querySelector(".technology-remove").click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    const button = modal.querySelector(".technology-remove-assignments");
    button.click();
    button.remove();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(modal.querySelector("#technology-library-status").textContent).toBe("Could not remove technology");
  });

  it("does not load stale catalog entries after an edit was removed", async () => {
    api.getTechnologies.mockResolvedValueOnce(catalog).mockResolvedValueOnce([]);
    const modal = document.querySelector("#detail-modal");
    await openTechnologyLibrary(modal, vi.fn());
    const staleButton = modal.querySelector(".technology-edit");
    staleButton.click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(modal.querySelector("#technology-name").value).toBe("");
  });

  it("creates a canonical entry with comma or line separated aliases", async () => {
    api.getTechnologies.mockResolvedValue([]);
    const modal = document.querySelector("#detail-modal");
    await openTechnologyLibrary(modal, vi.fn());
    modal.querySelector("#technology-name").value = "PostgreSQL";
    modal.querySelector("#technology-aliases").value = "Postgres\nPG";
    modal.querySelector("#technology-form").dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(api.createTechnology).toHaveBeenCalledWith({ name: "PostgreSQL", aliases: ["Postgres", "PG"] });
    expect(toast).toHaveBeenCalledWith("Technology added", "success");
  });

  it("edits aliases and supports canceling an edit", async () => {
    const modal = document.querySelector("#detail-modal");
    await openTechnologyLibrary(modal, vi.fn());
    api.getTechnologies.mockResolvedValue(catalog);
    modal.querySelector(".technology-edit").click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(modal.querySelector("#technology-name").value).toBe("Kubernetes");
    modal.querySelector("#technology-aliases").value = "K8s, kube";
    modal.querySelector("#technology-form").dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(api.updateTechnology).toHaveBeenCalledWith("k8s", { name: "Kubernetes", aliases: ["K8s", "kube"] });
    expect(toast).toHaveBeenCalledWith("Technology updated", "success");
    modal.querySelector(".technology-edit").click();
    modal.querySelector("#technology-cancel-edit").click();
    expect(modal.querySelector("#technology-name").value).toBe("");
  });

  it("retries a failed catalog load and deletes an unused technology", async () => {
    api.getTechnologies.mockRejectedValueOnce(new Error("offline")).mockResolvedValue([]);
    const modal = document.querySelector("#detail-modal");
    await openTechnologyLibrary(modal, vi.fn());
    expect(modal.querySelector("#technology-library-status").textContent).toBe("offline");
    modal.querySelector("#technology-retry").click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    api.getTechnologies.mockResolvedValue([{ id: "unused", name: "Go", aliases: [], jobs: [] }]);
    await openTechnologyLibrary(modal, vi.fn());
    modal.querySelector(".technology-remove").click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    modal.querySelector(".technology-confirm-delete").click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(api.deleteTechnology).toHaveBeenCalledWith("unused");
    expect(toast).toHaveBeenCalledWith("Technology deleted", "success");
  });

  it("shows save and delete failures and keeps assignment removal explicit", async () => {
    api.createTechnology.mockRejectedValueOnce(new Error("duplicate name"));
    api.deleteTechnology.mockRejectedValueOnce(new Error("delete failed"));
    api.removeTechnologyAssignments.mockRejectedValueOnce(new Error("assignment cleanup failed"));
    const modal = document.querySelector("#detail-modal");
    await openTechnologyLibrary(modal, vi.fn());
    modal.querySelector("#technology-name").value = "PostgreSQL";
    modal.querySelector("#technology-form").dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(modal.querySelector("#technology-library-status").textContent).toBe("duplicate name");
    modal.querySelector(".technology-remove").click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    modal.querySelector(".technology-remove-assignments").click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(modal.querySelector("#technology-library-status").textContent).toBe("assignment cleanup failed");
    expect(api.deleteTechnology).not.toHaveBeenCalled();
  });

  it("reports technology rename and unused-item deletion failures", async () => {
    api.updateTechnology.mockRejectedValueOnce(new Error("rename failed"));
    api.deleteTechnology.mockRejectedValueOnce(new Error("delete failed"));
    api.getTechnologies.mockResolvedValue([{ id: "unused", name: "Go", aliases: [], jobs: [] }]);
    const modal = document.querySelector("#detail-modal");
    await openTechnologyLibrary(modal, vi.fn());
    api.getTechnologies.mockResolvedValue([{ id: "unused", name: "Go", aliases: [], jobs: [] }]);
    modal.querySelector(".technology-edit").click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    modal.querySelector("#technology-name").value = "Go Lang";
    modal.querySelector("#technology-form").dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(modal.querySelector("#technology-library-status").textContent).toBe("rename failed");
    modal.querySelector(".technology-remove").click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    modal.querySelector(".technology-confirm-delete").click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(modal.querySelector("#technology-library-status").textContent).toBe("delete failed");
  });
});
