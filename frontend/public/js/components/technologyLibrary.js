import { api } from "../api.js";
import { activateModal } from "../modalA11y.js";
import { icon } from "../icons.js";
import { escapeAttr, escapeHtml } from "../utils/sanitize.js";
import { showToast } from "../utils/toast.js";

/** Opens the shared technology catalog manager. */
export async function openTechnologyLibrary(modalEl, onClose) {
  let editingID = "";
  let pendingRemovalID = "";
  let technologies = [];
  modalEl.innerHTML = `
    <div class="modal-header data-transfer-header"><div><div class="modal-title">Technology catalog</div><div class="modal-company">Keep canonical names and aliases for every process stack</div></div><button class="btn-secondary" id="btn-close-technology-library" type="button" aria-label="Close">${icon("close", 18)}</button></div>
    <div class="data-transfer-content technology-library-content">
      <details id="technology-form-disclosure" class="technology-form-disclosure">
        <summary class="technology-form-disclosure-summary"><span>Add technology</span>${icon("chevronDown", 16)}</summary>
        <form id="technology-form" class="technology-form">
          <label for="technology-name">Canonical technology</label><input id="technology-name" name="name" maxlength="80" required placeholder="e.g. Kubernetes" />
          <label for="technology-aliases">Aliases <span>comma or line separated</span></label><textarea id="technology-aliases" name="aliases" maxlength="500" rows="2" placeholder="e.g. K8s"></textarea>
          <div class="technology-form-actions"><button id="technology-save" class="btn-primary" type="submit">${icon("plus", 14)} Add technology</button><button id="technology-cancel-edit" class="btn-secondary" type="button" hidden>Cancel edit</button></div>
        </form>
      </details>
      <div class="technology-library-list-heading"><strong>Technologies</strong><span id="technology-library-count" aria-live="polite"></span></div>
      <label class="technology-library-search"><input id="technology-library-search" type="search" aria-label="Search technologies by name or alias" placeholder="Search by name or alias" autocomplete="off" /></label>
      <div id="technology-library-status" class="data-transfer-status" role="status" aria-live="polite"></div>
      <div id="technology-library-list" class="technology-library-list"></div>
    </div>`;
  activateModal(modalEl, "#btn-close-technology-library");
  modalEl.querySelector("#btn-close-technology-library").addEventListener("click", onClose);
  modalEl.querySelector("#technology-cancel-edit").addEventListener("click", () => resetForm());
  modalEl.querySelector("#technology-form").addEventListener("submit", saveTechnology);
  modalEl.querySelector("#technology-library-list").addEventListener("click", handleCatalogAction);
  modalEl.querySelector("#technology-library-search").addEventListener("input", renderList);
  await refreshList();

  function resetForm() {
    editingID = "";
    const form = modalEl.querySelector("#technology-form");
    const disclosure = modalEl.querySelector("#technology-form-disclosure");
    const restoreFocus = form.contains(document.activeElement);
    form.reset();
    disclosure.open = false;
    modalEl.querySelector("#technology-save").innerHTML = `${icon("plus", 14)} Add technology`;
    modalEl.querySelector("#technology-cancel-edit").hidden = true;
    if (restoreFocus) disclosure.querySelector("summary")?.focus();
  }

  async function saveTechnology(event) {
    event.preventDefault();
    const name = modalEl.querySelector("#technology-name").value;
    const aliases = modalEl.querySelector("#technology-aliases").value.split(/[\n,]/).map((alias) => alias.trim()).filter(Boolean);
    const payload = { name, aliases };
    const wasEditing = Boolean(editingID);
    const button = modalEl.querySelector("#technology-save");
    button.disabled = true;
    try {
      if (editingID) await api.updateTechnology(editingID, payload);
      else await api.createTechnology(payload);
      resetForm();
      modalEl.querySelector("#technology-library-search").value = "";
      showToast(wasEditing ? "Technology updated" : "Technology added", "success");
      await refreshList();
    } catch (error) {
      modalEl.querySelector("#technology-library-status").textContent = error.message || "Could not save technology";
    } finally { button.disabled = false; }
  }

  async function refreshList() {
    const list = modalEl.querySelector("#technology-library-list");
    const status = modalEl.querySelector("#technology-library-status");
    status.textContent = "Loading technologies…";
    try {
      technologies = await api.getTechnologies();
      status.textContent = "";
      renderList();
    } catch (error) {
      status.textContent = error.message || "Could not load the catalog";
      list.innerHTML = '<button class="btn-secondary" id="technology-retry" type="button">Try again</button>';
    }
  }

  function renderList() {
    const query = modalEl.querySelector("#technology-library-search").value.trim().toLocaleLowerCase();
    const matches = technologies.filter((item) => [item.name, ...(item.aliases || [])]
      .some((value) => value.toLocaleLowerCase().includes(query)));
    const count = modalEl.querySelector("#technology-library-count");
    count.textContent = query ? `${matches.length} of ${technologies.length}` : `${technologies.length} total`;
    if (!technologies.length) {
      modalEl.querySelector("#technology-library-list").innerHTML = '<p class="technology-library-empty">No technologies yet. Add the first canonical name above.</p>';
      return;
    }
    modalEl.querySelector("#technology-library-list").innerHTML = matches.length
      ? matches.map(renderTechnologyRow).join("")
      : `<p class="technology-library-empty">No technologies match “${escapeHtml(query)}”.</p>`;
  }

  function renderTechnologyRow(item) {
    const aliases = item.aliases || [];
    const jobs = item.jobs || [];
    const impact = pendingRemovalID === item.id ? `<div class="technology-removal-impact"><strong>Used by ${jobs.length} process${jobs.length === 1 ? "" : "es"}</strong>${jobs.length ? `<ul>${jobs.map((job) => `<li>${escapeHtml(job.company_name)} — ${escapeHtml(job.position_title)}</li>`).join("")}</ul><button class="btn-danger technology-remove-assignments" type="button" data-id="${escapeAttr(item.id)}">Remove from all listed processes and delete</button>` : `<button class="btn-danger technology-confirm-delete" type="button" data-id="${escapeAttr(item.id)}">Delete technology</button>`}</div>` : "";
    const aliasMarkup = aliases.length
      ? `<div class="technology-catalog-aliases" aria-label="Aliases"><span class="technology-catalog-alias-label">Aliases</span>${aliases.map((alias) => `<span class="technology-catalog-alias">${escapeHtml(alias)}</span>`).join("")}</div>`
      : "";
    return `<article class="technology-catalog-row"><div class="technology-catalog-info"><strong>${escapeHtml(item.name)}</strong>${aliasMarkup}<span>${jobs.length} process${jobs.length === 1 ? "" : "es"}</span></div><div class="technology-catalog-actions"><button class="btn-secondary technology-edit" type="button" data-id="${escapeAttr(item.id)}">Edit</button><button class="btn-secondary technology-remove" type="button" data-id="${escapeAttr(item.id)}">Remove</button></div>${impact}</article>`;
  }

  function focusTechnologyRemove(id, fallbackIndex = 0) {
    const buttons = [...modalEl.querySelectorAll(".technology-remove")];
    const fallback = buttons[Math.max(0, Math.min(fallbackIndex, buttons.length - 1))];
    (buttons.find((removeButton) => removeButton.dataset.id === id) || fallback || modalEl.querySelector("#technology-library-search") || modalEl.querySelector("#btn-close-technology-library"))?.focus();
  }

  async function handleCatalogAction(event) {
    const button = event.target.closest("button");
    if (!button) return;
    if (button.id === "technology-retry") { void refreshList(); return; }
    const item = await catalogItem(button.dataset.id);
    if (!item) return;
    if (button.classList.contains("technology-edit")) {
      editingID = item.id;
      modalEl.querySelector("#technology-form-disclosure").open = true;
      modalEl.querySelector("#technology-name").value = item.name;
      modalEl.querySelector("#technology-aliases").value = (item.aliases || []).join(", ");
      modalEl.querySelector("#technology-save").innerHTML = `${icon("check", 14)} Save changes`;
      modalEl.querySelector("#technology-cancel-edit").hidden = false;
      modalEl.querySelector("#technology-name").focus();
      return;
    }
    if (button.classList.contains("technology-remove")) {
      pendingRemovalID = pendingRemovalID === item.id ? "" : item.id;
      await refreshList();
      focusTechnologyRemove(item.id);
      return;
    }
    if (button.classList.contains("technology-confirm-delete")) void deleteTechnology(item.id);
    if (button.classList.contains("technology-remove-assignments")) void removeAssignmentsAndDelete(item.id);
  }

  async function catalogItem(id) {
    return (await api.getTechnologies()).find((item) => item.id === id);
  }

  async function deleteTechnology(id) {
    const fallbackIndex = [...modalEl.querySelectorAll(".technology-remove")]
      .findIndex((button) => button.dataset.id === id);
    try {
      await api.deleteTechnology(id);
      pendingRemovalID = "";
      showToast("Technology deleted", "success");
      await refreshList();
      focusTechnologyRemove(id, fallbackIndex);
    } catch (error) {
      modalEl.querySelector("#technology-library-status").textContent = error.message || "Could not delete technology";
    }
  }

  async function removeAssignmentsAndDelete(id) {
    const fallbackIndex = [...modalEl.querySelectorAll(".technology-remove")]
      .findIndex((button) => button.dataset.id === id);
    const button = modalEl.querySelector(".technology-remove-assignments");
    if (button) button.disabled = true;
    try {
      await api.removeTechnologyAssignments(id);
      await api.deleteTechnology(id);
      pendingRemovalID = "";
      showToast("Technology removed from processes and deleted", "success");
      await refreshList();
      focusTechnologyRemove(id, fallbackIndex);
    } catch (error) {
      if (button) button.disabled = false;
      await refreshList();
      modalEl.querySelector("#technology-library-status").textContent = error.message || "Could not remove technology";
      focusTechnologyRemove(id, fallbackIndex);
    }
  }
}
