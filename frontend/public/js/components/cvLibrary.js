import { api } from "../api.js";
import { activateModal } from "../modalA11y.js";
import { icon } from "../icons.js";
import { escapeAttr, escapeHtml } from "../utils/sanitize.js";
import { showToast } from "../utils/toast.js";

/** Formats an upload timestamp for the user's locale. */
export function formatCvUploadDate(value) {
  if (value === null || value === undefined || value === "") return "Date unavailable";
  const numeric = typeof value === "number" || /^\d+(?:\.\d+)?$/.test(String(value)) ? Number(value) : null;
  const timestamp = numeric === null ? value : (numeric < 1e12 ? numeric * 1000 : numeric);
  const date = new Date(timestamp);
  if (Number.isNaN(date.getTime())) return "Date unavailable";
  return new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(date);
}

function versionsFrom(response) {
  return Array.isArray(response) ? response : Array.isArray(response?.versions) ? response.versions : [];
}

/** Renders and wires the standalone shared CV version library. */
export async function openCvLibrary(modalEl, onBack, onClose) {
  const load = async (focusSelector = "#btn-close-cv-library") => {
    modalEl.innerHTML = `
      <div class="modal-header data-transfer-header">
        <div><div class="modal-title">CV Library</div><div class="modal-company">Manage the versions of your CV</div></div>
        <button class="btn-secondary" id="btn-close-cv-library" type="button" aria-label="Close">${icon("close", 18)}</button>
      </div>
      <div class="data-transfer-content cv-library-content">
        <div class="cv-library-toolbar"><button id="btn-cv-library-back" class="cv-library-back" type="button">${icon("chevronLeft", 16)}<span>Back to processes</span></button>
          <button id="btn-cv-library-upload" class="btn-primary cv-upload-label" type="button">${icon("plus", 15)} Upload new version</button>
          <input id="cv-library-upload-file" class="cv-library-file-input" type="file" accept="application/pdf,.pdf" aria-label="Choose a new CV version" />
        </div>
        <div class="data-transfer-status" id="cv-library-status" role="status" aria-live="polite"></div>
        <div id="cv-version-list" class="cv-version-list" aria-live="polite"></div>
      </div>`;
    modalEl.querySelector("#btn-close-cv-library").addEventListener("click", onClose);
    modalEl.querySelector("#btn-cv-library-back").addEventListener("click", onBack);
    const input = modalEl.querySelector("#cv-library-upload-file");
    modalEl.querySelector("#btn-cv-library-upload").addEventListener("click", () => input.click());
    input.addEventListener("change", async () => {
      const file = input.files?.[0];
      if (!file) return;
      const status = modalEl.querySelector("#cv-library-status");
      status.textContent = "Uploading CV version…";
      input.disabled = true;
      try {
        await api.uploadCvVersion(file);
        showToast("CV version uploaded", "success");
        await load();
      } catch (error) {
        status.textContent = error.message || "Failed to upload CV version";
        input.disabled = false;
        input.value = "";
      }
    });
    activateModal(modalEl, focusSelector);
    await refreshList();
  };

  async function refreshList() {
    const list = modalEl.querySelector("#cv-version-list");
    const status = modalEl.querySelector("#cv-library-status");
    if (!list || !status) return;
    list.innerHTML = "";
    status.textContent = "Loading CV versions…";
    try {
      const versions = versionsFrom(await api.getCvVersions());
      status.textContent = "";
      if (!versions.length) {
        list.innerHTML = '<p class="cv-library-empty">No CV versions yet. Upload your current CV to get started.</p>';
        return;
      }
      list.innerHTML = versions.map((version) => `
        <article class="cv-version-row">
          <div class="cv-version-info"><strong>CV v${escapeHtml(version.version_number)}</strong>
            <span class="cv-version-filename" title="${escapeAttr(version.original_name)}">${escapeHtml(version.original_name)}</span>
            <span class="cv-version-date">Uploaded ${escapeHtml(formatCvUploadDate(version.uploaded_at))}</span>
          </div>
          <div class="cv-version-actions">
            <button class="btn-secondary cv-version-download" type="button" data-id="${escapeAttr(version.id)}" data-name="${escapeAttr(version.original_name)}" aria-label="Download CV version ${escapeAttr(version.version_number)}">${icon("download", 14)}<span>Download</span></button>
            <button class="btn-danger cv-version-delete" type="button" data-id="${escapeAttr(version.id)}" data-version="${escapeAttr(version.version_number)}" aria-label="Delete CV version ${escapeAttr(version.version_number)}">${icon("trash", 14)}<span>Delete</span></button>
          </div>
        </article>`).join("");
      list.querySelectorAll(".cv-version-download").forEach((button) => button.addEventListener("click", async () => {
        button.disabled = true;
        try {
          const blob = await api.downloadCvVersion(button.dataset.id);
          const url = URL.createObjectURL(blob);
          const anchor = document.createElement("a");
          anchor.href = url;
          anchor.download = button.dataset.name || "cv.pdf";
          document.body.append(anchor);
          anchor.click();
          anchor.remove();
          window.setTimeout(() => URL.revokeObjectURL(url), 1000);
        } catch (error) {
          showToast(error.message || "Failed to download CV", "error");
        } finally {
          button.disabled = false;
        }
      }));
      list.querySelectorAll(".cv-version-delete").forEach((button) => button.addEventListener("click", async () => {
        if (!window.confirm(`Delete CV v${button.dataset.version}? This cannot be undone.`)) return;
        button.disabled = true;
        try {
          await api.deleteCvVersion(button.dataset.id);
          showToast("CV version deleted", "success");
          await refreshList();
        } catch (error) {
          status.textContent = error.message || "Failed to delete CV version";
          button.disabled = false;
        }
      }));
    } catch (error) {
      status.textContent = error.message || "Failed to load CV versions";
      list.innerHTML = '<button class="btn-secondary" id="btn-cv-library-retry" type="button">Try again</button>';
      list.querySelector("#btn-cv-library-retry").addEventListener("click", refreshList);
    }
  }

  await load();
}
