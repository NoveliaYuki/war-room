import { beforeEach, describe, expect, it, vi } from "vitest";

const { api, toast, confirmAction } = vi.hoisted(() => ({
  api: {
    getCvVersions: vi.fn(),
    uploadCvVersion: vi.fn(),
    downloadCvVersion: vi.fn(),
    deleteCvVersion: vi.fn(),
  },
  toast: vi.fn(),
  confirmAction: vi.fn(),
}));
vi.mock("../../public/js/api.js", () => ({ api }));
vi.mock("../../public/js/utils/toast.js", () => ({ showToast: toast }));
vi.mock("../../public/js/utils/confirmDialog.js", () => ({ confirmAction }));

import { formatCvUploadDate, openCvLibrary } from "../../public/js/components/cvLibrary.js";

describe("CV library", () => {
  beforeEach(() => {
    document.body.innerHTML = '<main class="app-container"></main><div id="detail-modal"></div>';
    api.getCvVersions.mockReset();
    api.uploadCvVersion.mockReset();
    api.downloadCvVersion.mockReset();
    api.deleteCvVersion.mockReset();
    toast.mockReset();
    confirmAction.mockReset().mockImplementation(async ({ title, message }) => window.confirm(`${title} ${message}`));
  });

  it("formats backend Unix-second upload dates as localized timestamps", () => {
    const expected = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(new Date(1_798_080_000_000));
    expect(formatCvUploadDate(1_798_080_000)).toBe(expected);
    expect(formatCvUploadDate(1_798_080_000_000)).toBe(expected);
    expect(formatCvUploadDate(null)).toBe("Date unavailable");
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
    expect(confirm).toHaveBeenCalledWith("Delete CV v1? This version will be permanently removed from your CV library.");
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
    input.dispatchEvent(new Event("change"));
    expect(api.uploadCvVersion).not.toHaveBeenCalled();
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

  it("accepts an enveloped CV version response and marks the latest version", async () => {
    api.getCvVersions.mockResolvedValue({ versions: [
      { id: "v1", version_number: 1, original_name: "CV.pdf", uploaded_at: 1_798_000_000 },
      { id: "v3", version_number: 3, original_name: "CV latest.pdf", uploaded_at: 1_798_080_000 },
    ] });
    const modal = document.querySelector("#detail-modal");
    await openCvLibrary(modal, vi.fn());

    expect(modal.querySelectorAll(".cv-version-row")).toHaveLength(2);
    expect(modal.querySelector(".cv-version-latest").previousElementSibling.textContent).toBe("CV v3");
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

  it("downloads the selected CV with its original filename", async () => {
    api.getCvVersions.mockResolvedValue([{ id: "v2", version_number: 2, original_name: "Latest CV.pdf", uploaded_at: 1_798_080_000 }]);
    const blobUrl = "blob:latest-cv";
    const createObjectURL = vi.spyOn(URL, "createObjectURL").mockReturnValue(blobUrl);
    const revokeObjectURL = vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => {});
    const anchorClick = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
    const modal = document.querySelector("#detail-modal");
    await openCvLibrary(modal, vi.fn());

    const download = modal.querySelector(".cv-version-download");
    download.click();
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(api.downloadCvVersion).toHaveBeenCalledWith("v2");
    expect(createObjectURL).toHaveBeenCalled();
    expect(anchorClick).toHaveBeenCalledOnce();
    expect(download.disabled).toBe(false);
    expect(revokeObjectURL).not.toHaveBeenCalled();
  });

  it("returns focus to a neighboring version or the upload control after deletion", async () => {
    api.getCvVersions
      .mockResolvedValueOnce([
        { id: "v2", version_number: 2, original_name: "CV v2.pdf", uploaded_at: 1_798_080_000 },
        { id: "v1", version_number: 1, original_name: "CV v1.pdf", uploaded_at: 1_798_000_000 },
      ])
      .mockResolvedValueOnce([{ id: "v1", version_number: 1, original_name: "CV v1.pdf", uploaded_at: 1_798_000_000 }])
      .mockResolvedValueOnce([]);
    api.deleteCvVersion.mockResolvedValue({});
    confirmAction.mockResolvedValue(true);
    const modal = document.querySelector("#detail-modal");
    await openCvLibrary(modal, vi.fn());

    modal.querySelector('[data-id="v2"].cv-version-delete').click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(document.activeElement.dataset.id).toBe("v1");

    modal.querySelector('[data-id="v1"].cv-version-delete').click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(document.activeElement).toBe(modal.querySelector("#btn-cv-library-upload"));
    expect(modal.querySelector(".cv-library-empty")).not.toBeNull();
  });

  it("shows a fallback message and restores upload focus when upload errors omit a message", async () => {
    api.getCvVersions.mockResolvedValue([]);
    api.uploadCvVersion.mockRejectedValue({});
    const modal = document.querySelector("#detail-modal");
    await openCvLibrary(modal, vi.fn());
    const input = modal.querySelector("#cv-library-upload-file");
    Object.defineProperty(input, "files", { configurable: true, value: [new File(["pdf"], "resume.pdf")] });

    input.dispatchEvent(new Event("change"));
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(modal.querySelector("#cv-library-status").textContent).toBe("Failed to upload CV version");
    expect(input.disabled).toBe(false);
    expect(document.activeElement).toBe(modal.querySelector("#btn-cv-library-upload"));
  });

  it("uses fallback messages for load, download, and delete errors without text", async () => {
    api.getCvVersions.mockRejectedValueOnce({}).mockResolvedValueOnce([
      { id: "v1", version_number: 1, original_name: "CV.pdf", uploaded_at: 1_798_080_000 },
    ]);
    const modal = document.querySelector("#detail-modal");
    await openCvLibrary(modal, vi.fn());
    expect(modal.querySelector("#cv-library-status").textContent).toBe("Failed to load CV versions");
    modal.querySelector("#btn-cv-library-retry").click();
    await new Promise((resolve) => setTimeout(resolve, 0));

    api.downloadCvVersion.mockRejectedValueOnce({});
    modal.querySelector(".cv-version-download").click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(toast).toHaveBeenCalledWith("Failed to download CV", "error");

    api.deleteCvVersion.mockRejectedValueOnce({});
    confirmAction.mockResolvedValue(true);
    modal.querySelector(".cv-version-delete").click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(modal.querySelector("#cv-library-status").textContent).toBe("Failed to delete CV version");
  });

  it("treats malformed CV version responses as an empty library", async () => {
    api.getCvVersions.mockResolvedValue({ versions: "invalid" });
    const modal = document.querySelector("#detail-modal");
    await openCvLibrary(modal, vi.fn());
    expect(modal.querySelector(".cv-library-empty")).not.toBeNull();
  });

  it("uses the default filename if a download row has no filename", async () => {
    api.getCvVersions.mockResolvedValue([{ id: "v1", version_number: 1, uploaded_at: 1_798_080_000 }]);
    api.downloadCvVersion.mockResolvedValue(new Blob(["pdf"]));
    vi.spyOn(URL, "createObjectURL").mockReturnValue("blob:cv");
    vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => {});
    let downloadedName = "";
    const anchorClick = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function () {
      downloadedName = this.download;
    });
    const modal = document.querySelector("#detail-modal");
    await openCvLibrary(modal, vi.fn());
    const download = modal.querySelector(".cv-version-download");
    download.dataset.name = "";
    download.click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(downloadedName).toBe("cv.pdf");
    anchorClick.mockRestore();
  });
});
