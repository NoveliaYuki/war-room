import { beforeEach, describe, expect, it, vi } from "vitest";

const { api, toast } = vi.hoisted(() => ({
  api: {
    getCvVersions: vi.fn(),
    uploadCvVersion: vi.fn(),
    downloadCvVersion: vi.fn(),
    deleteCvVersion: vi.fn(),
  },
  toast: vi.fn(),
}));
vi.mock("../../public/js/api.js", () => ({ api }));
vi.mock("../../public/js/utils/toast.js", () => ({ showToast: toast }));

import { formatCvUploadDate, openCvLibrary } from "../../public/js/components/cvLibrary.js";

describe("CV library", () => {
  beforeEach(() => {
    document.body.innerHTML = '<main class="app-container"></main><div id="detail-modal"></div>';
    api.getCvVersions.mockReset();
    api.uploadCvVersion.mockReset();
    api.downloadCvVersion.mockReset();
    api.deleteCvVersion.mockReset();
    toast.mockReset();
  });

  it("formats backend Unix-second upload dates as localized timestamps", () => {
    const expected = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(new Date(1_798_080_000_000));
    expect(formatCvUploadDate(1_798_080_000)).toBe(expected);
    expect(formatCvUploadDate("not a date")).toBe("Date unavailable");
  });

  it("shows ordered CV versions, localized dates, and download and delete controls", async () => {
    api.getCvVersions.mockResolvedValue([
      { id: "v2", version_number: 2, original_name: "CV final.pdf", uploaded_at: 1_798_080_000, file_size: 12 },
      { id: "v1", version_number: 1, original_name: "CV.pdf", uploaded_at: 1_798_000_000, file_size: 11 },
    ]);
    const modal = document.querySelector("#detail-modal");
    await openCvLibrary(modal, vi.fn(), vi.fn());
    expect(modal.querySelectorAll(".cv-version-row")).toHaveLength(2);
    expect(modal.querySelectorAll(".cv-version-row")[0].textContent).toContain("CV v2");
    expect(modal.textContent).toContain(formatCvUploadDate(1_798_080_000));
    expect(modal.querySelectorAll(".cv-version-download")).toHaveLength(2);
    expect(modal.querySelectorAll(".cv-version-delete")).toHaveLength(2);
  });

  it("confirms before deleting a version and reloads the library", async () => {
    api.getCvVersions.mockResolvedValue([{ id: "v1", version_number: 1, original_name: "CV.pdf", uploaded_at: 1_798_080_000 }]);
    api.deleteCvVersion.mockResolvedValue({});
    const confirm = vi.fn().mockReturnValue(true);
    window.confirm = confirm;
    const modal = document.querySelector("#detail-modal");
    await openCvLibrary(modal, vi.fn(), vi.fn());
    modal.querySelector(".cv-version-delete").click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(confirm).toHaveBeenCalledWith("Delete CV v1? This cannot be undone.");
    expect(api.deleteCvVersion).toHaveBeenCalledWith("v1");
    expect(api.getCvVersions).toHaveBeenCalledTimes(2);
  });

  it("supports an empty library and retrying a failed load", async () => {
    api.getCvVersions.mockRejectedValueOnce(new Error("offline")).mockResolvedValueOnce([]);
    const modal = document.querySelector("#detail-modal");
    await openCvLibrary(modal, vi.fn(), vi.fn());
    expect(modal.querySelector("#cv-library-status").textContent).toBe("offline");
    modal.querySelector("#btn-cv-library-retry").click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(modal.querySelector(".cv-library-empty").textContent).toContain("No CV versions yet");
  });

  it("uploads a version, refreshes the list, and reports upload failures", async () => {
    api.getCvVersions.mockResolvedValue([]);
    api.uploadCvVersion.mockResolvedValueOnce({}).mockRejectedValueOnce(new Error("upload failed"));
    const modal = document.querySelector("#detail-modal");
    await openCvLibrary(modal, vi.fn(), vi.fn());
    const input = modal.querySelector("#cv-library-upload-file");
    const file = new File(["pdf"], "resume.pdf", { type: "application/pdf" });
    Object.defineProperty(input, "files", { configurable: true, value: [file] });
    input.dispatchEvent(new Event("change"));
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(api.uploadCvVersion).toHaveBeenCalledWith(file);
    expect(toast).toHaveBeenCalledWith("CV version uploaded", "success");
    input.dispatchEvent(new Event("change"));
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(modal.querySelector("#cv-library-status").textContent).toBe("upload failed");
    expect(input.disabled).toBe(false);
  });

  it("downloads versions and reports download or delete failures", async () => {
    api.getCvVersions.mockResolvedValue([{ id: "v1", version_number: 1, original_name: "CV.pdf", uploaded_at: 1_798_080_000 }]);
    api.downloadCvVersion.mockRejectedValueOnce(new Error("download failed"));
    api.deleteCvVersion.mockRejectedValueOnce(new Error("delete failed"));
    window.confirm = vi.fn().mockReturnValueOnce(false).mockReturnValueOnce(true);
    const modal = document.querySelector("#detail-modal");
    await openCvLibrary(modal, vi.fn(), vi.fn());
    modal.querySelector(".cv-version-download").click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(toast).toHaveBeenCalledWith("download failed", "error");
    modal.querySelector(".cv-version-delete").click();
    expect(api.deleteCvVersion).not.toHaveBeenCalled();
    modal.querySelector(".cv-version-delete").click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(modal.querySelector("#cv-library-status").textContent).toBe("delete failed");
    expect(modal.querySelector(".cv-version-delete").disabled).toBe(false);
  });
});
