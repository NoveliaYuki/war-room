import { api } from "../api.js";
import { activateModal } from "../modalA11y.js";
import { icon } from "../icons.js";
import { escapeAttr, escapeHtml } from "../utils/sanitize.js";
import { showToast } from "../utils/toast.js";
import { confirmAction } from "../utils/confirmDialog.js";
import { getLocale, t } from "../i18n.js";

/** Formats an upload timestamp for the user's locale. */
export function formatCvUploadDate(value) {
  if (value === null || value === undefined || value === "") return t("date.unavailable");
  const numeric = typeof value === "number" || /^\d+(?:\.\d+)?$/.test(String(value)) ? Number(value) : null;
  const timestamp = numeric === null ? value : (numeric < 1e12 ? numeric * 1000 : numeric);
  const date = new Date(timestamp);
  if (Number.isNaN(date.getTime())) return t("date.unavailable");
  return new Intl.DateTimeFormat(getLocale(), { dateStyle: "medium", timeStyle: "short" }).format(date);
}

function versionsFrom(response) {
  return Array.isArray(response) ? response : Array.isArray(response?.versions) ? response.versions : [];
}

/** Renders and wires the standalone shared CV version library. */
export async function openCvLibrary(modalEl, onClose) {
  const load = async (focusSelector = "#btn-cv-library-upload") => {
    modalEl.innerHTML = `
      <div class="modal-header data-transfer-header">
        <div><div class="modal-title">${escapeHtml(t("cv.title"))}</div><div class="modal-company">${escapeHtml(t("cv.subtitle"))}</div></div>
        <button class="btn-secondary" id="btn-close-cv-library" type="button" aria-label="${escapeAttr(t("common.close"))}">${icon("close", 18)}</button>
      </div>
      <div class="data-transfer-content cv-library-content">
        <div class="cv-library-toolbar"><button id="btn-cv-library-upload" class="btn-primary cv-upload-label" type="button">${icon("plus", 15)} ${escapeHtml(t("cv.upload"))}</button>
          <input id="cv-library-upload-file" class="cv-library-file-input" type="file" accept="application/pdf,.pdf" aria-label="${escapeAttr(t("cv.chooseFile"))}" tabindex="-1" />
        </div>
        <div class="data-transfer-status" id="cv-library-status" role="status" aria-live="polite"></div>
        <div id="cv-version-list" class="cv-version-list" aria-live="polite"></div>
      </div>`;
    modalEl.querySelector("#btn-close-cv-library").addEventListener("click", onClose);
    const input = modalEl.querySelector("#cv-library-upload-file");
    modalEl.querySelector("#btn-cv-library-upload").addEventListener("click", () => input.click());
    input.addEventListener("change", async () => {
      const file = input.files?.[0];
      if (!file) return;
      const status = modalEl.querySelector("#cv-library-status");
      status.textContent = t("cv.uploading");
      input.disabled = true;
      try {
        await api.uploadCvVersion(file);
        showToast(t("cv.uploaded"), "success");
        await load("#btn-cv-library-upload");
      } catch (error) {
        status.textContent = error.message || t("cv.uploadFailed");
        input.disabled = false;
        input.value = "";
        modalEl.querySelector("#btn-cv-library-upload")?.focus();
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
    status.textContent = t("cv.loading");
    try {
      const versions = versionsFrom(await api.getCvVersions());
      status.textContent = "";
      if (!versions.length) {
        list.innerHTML = `<div class="cv-library-empty" role="status"><span class="cv-library-empty-icon" aria-hidden="true">${icon("fileText", 20)}</span><div class="cv-library-empty-copy"><strong>${escapeHtml(t("cv.emptyTitle"))}</strong><span>${escapeHtml(t("cv.emptyBody"))}</span></div></div>`;
        return;
      }
      const latestVersion = versions.reduce((latest, version) => (
        !latest || Number(version.version_number) > Number(latest.version_number) ? version : latest
      ), null);
      list.innerHTML = versions.map((version) => `
        <article class="cv-version-row">
          <div class="cv-version-info"><div class="cv-version-heading"><strong>${escapeHtml(t("cv.version", { version: version.version_number }))}</strong>${version.id === latestVersion?.id ? `<span class="cv-version-latest">${escapeHtml(t("cv.latest"))}</span>` : ""}</div>
            <span class="cv-version-filename" title="${escapeAttr(version.original_name)}">${escapeHtml(version.original_name)}</span>
            <span class="cv-version-date">${escapeHtml(t("cv.uploadedOn", { date: formatCvUploadDate(version.uploaded_at) }))}</span>
          </div>
          <div class="cv-version-actions">
            <button class="btn-secondary cv-version-download" type="button" data-id="${escapeAttr(version.id)}" data-name="${escapeAttr(version.original_name)}" aria-label="${escapeAttr(t("cv.downloadLabel", { version: version.version_number }))}">${icon("download", 14)}<span>${escapeHtml(t("common.download"))}</span></button>
            <button class="btn-danger cv-version-delete" type="button" data-id="${escapeAttr(version.id)}" data-version="${escapeAttr(version.version_number)}" aria-label="${escapeAttr(t("cv.deleteLabel", { version: version.version_number }))}">${icon("trash", 14)}<span>${escapeHtml(t("common.delete"))}</span></button>
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
          showToast(error.message || t("cv.downloadFailed"), "error");
        } finally {
          button.disabled = false;
        }
      }));
      list.querySelectorAll(".cv-version-delete").forEach((button) => button.addEventListener("click", async () => {
        const fallbackIndex = [...list.querySelectorAll(".cv-version-delete")].indexOf(button);
        const confirmed = await confirmAction({
          title: t("cv.deleteTitle", { version: button.dataset.version }),
          message: t("cv.deleteMessage"),
          confirmLabel: t("cv.deleteConfirm"),
          danger: true,
        });
        if (!confirmed) return;
        button.disabled = true;
        try {
          await api.deleteCvVersion(button.dataset.id);
          showToast(t("cv.deleted"), "success");
          await refreshList();
          focusCvVersionDelete(button.dataset.id, fallbackIndex);
        } catch (error) {
          status.textContent = error.message || t("cv.deleteFailed");
          button.disabled = false;
        }
      }));
    } catch (error) {
      status.textContent = error.message || t("cv.loadFailed");
      list.innerHTML = `<button class="btn-secondary" id="btn-cv-library-retry" type="button">${escapeHtml(t("common.tryAgain"))}</button>`;
      list.querySelector("#btn-cv-library-retry").addEventListener("click", refreshList);
    }
  }

  function focusCvVersionDelete(id, fallbackIndex = 0) {
    const buttons = [...modalEl.querySelectorAll(".cv-version-delete")];
    const fallback = buttons[Math.max(0, Math.min(fallbackIndex, buttons.length - 1))];
    (buttons.find((button) => button.dataset.id === id) || fallback || modalEl.querySelector("#btn-cv-library-upload"))?.focus();
  }

  await load();
}
