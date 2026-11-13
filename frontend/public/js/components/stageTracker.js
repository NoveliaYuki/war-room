import { showToast } from "../utils/toast.js";
import { escapeHtml, escapeAttr } from "../utils/sanitize.js";
/**
 * @fileoverview Selection Process Stage Tracker Component.
 * Supports adding, deleting, reordering stages, updating progress, and embedding question banks.
 */

import { api } from "../api.js";
import { renderQuestionList } from "./questionList.js";
import { icon } from "../icons.js";
import { confirmAction } from "../utils/confirmDialog.js";
import { t } from "../i18n.js";
import { stageName as describeStage, stageDescriptionText, stageTypeLabel } from "../utils/labels.js";

/**
 * Maps stage types to clean CSS class identifiers.
 */
const STAGE_TYPE_CLASSES = new Map([
  ["HR", "HR"],
  ["Technical", "Technical"],
  ["Cultural", "Cultural"],
  ["Offer & Decision", "Offer-Decision"],
]);

function getStageTypeClass(type) {
  return STAGE_TYPE_CLASSES.get(type) || "HR";
}

/**
 * Renders the stage tracker component.
 *
 * @param {string} jobId - The current job process ID.
 * @param {Array} stages - Array of stages with questions.
 * @param {Function} onRefresh - Callback to re-render the full job modal.
 * @returns {HTMLElement} The rendered stages container.
 */
export function renderStageTracker(jobId, stages = [], onRefresh) {
  const container = document.createElement("div");
  container.className = "stages-container";

  if (stages.length === 0) {
    const emptyBox = document.createElement("div");
    emptyBox.className = "empty-state";
    emptyBox.style.padding = "30px 20px";
    emptyBox.innerHTML = `
      <p style="color: var(--text-secondary); margin-bottom: 12px;">${escapeHtml(t("stage.noneConfigured"))}</p>
    `;
    container.appendChild(emptyBox);
  }

  stages.forEach((stage, idx) => {
    const card = document.createElement("div");
    const statusClass = ["pending", "current", "completed", "skipped"].includes(stage.status) ? stage.status : "pending";
    card.className = `stage-card is-${statusClass}`;

    // Header Bar
    const header = document.createElement("div");
    header.className = "stage-header";

    const headerLeft = document.createElement("div");
    headerLeft.className = "stage-header-left";
    headerLeft.innerHTML = `
      <div class="stage-index-badge">${idx + 1}</div>
      <span class="stage-type-badge ${escapeAttr(getStageTypeClass(stage.stage_type))}">${escapeHtml(stageTypeLabel(stage.stage_type))}</span>
      <div class="stage-title">${escapeHtml(describeStage(stage))}</div>
    `;

    const headerRight = document.createElement("div");
    headerRight.className = "stage-header-right";
    const stageName = describeStage(stage);

    // Status Selector
    const statusSelect = document.createElement("select");
    statusSelect.className = "stage-status-select";
    statusSelect.setAttribute("aria-label", t("stage.statusLabel", { stage: stageName }));
    ["pending", "current", "completed", "skipped"].forEach((s) => {
      const opt = document.createElement("option");
      opt.value = s;
      opt.textContent = t(`stageStatus.${s}`);
      opt.selected = stage.status === s;
      statusSelect.appendChild(opt);
    });
    statusSelect.addEventListener("change", async () => {
      try {
        await api.updateStage(stage.id, { status: statusSelect.value });
        if (onRefresh) onRefresh();
      } catch (err) {
        showToast(err.message, "error");
      }
    });

    // Reorder Buttons
    if (idx > 0) {
      const upBtn = document.createElement("button");
      upBtn.className = "stage-action-btn";
      upBtn.innerHTML = icon("chevronUp", 12);
      upBtn.title = t("stage.moveUpTitle");
      upBtn.setAttribute("aria-label", t("stage.moveUpLabel", { stage: stageName }));
      upBtn.addEventListener("click", async () => {
        try {
          const ids = stages.map((s) => s.id);
          const temp = ids[idx];
          ids[idx] = ids[idx - 1];
          ids[idx - 1] = temp;
          await api.reorderStages(jobId, ids);
          if (onRefresh) onRefresh();
        } catch (err) {
          showToast(err.message, "error");
        }
      });
      headerRight.appendChild(upBtn);
    }

    if (idx < stages.length - 1) {
      const downBtn = document.createElement("button");
      downBtn.className = "stage-action-btn";
      downBtn.innerHTML = icon("chevronDown", 12);
      downBtn.title = t("stage.moveDownTitle");
      downBtn.setAttribute("aria-label", t("stage.moveDownLabel", { stage: stageName }));
      downBtn.addEventListener("click", async () => {
        try {
          const ids = stages.map((s) => s.id);
          const temp = ids[idx];
          ids[idx] = ids[idx + 1];
          ids[idx + 1] = temp;
          await api.reorderStages(jobId, ids);
          if (onRefresh) onRefresh();
        } catch (err) {
          showToast(err.message, "error");
        }
      });
      headerRight.appendChild(downBtn);
    }

    // Delete Stage Button
    const delBtn = document.createElement("button");
    delBtn.className = "stage-action-btn";
    delBtn.innerHTML = icon("close", 12);
    delBtn.title = t("stage.removeTitle");
    delBtn.setAttribute("aria-label", t("stage.removeLabel", { stage: stageName }));
    delBtn.addEventListener("click", async () => {
      try {
        const confirmed = await confirmAction({
          title: t("stage.removeConfirmTitle"),
          message: t("stage.deleteMessage", { stage: stageName }),
          confirmLabel: t("stage.removeConfirm"),
          danger: true,
        });
        if (!confirmed) return;
        await api.deleteStage(stage.id);
        if (onRefresh) onRefresh();
      } catch (err) {
        showToast(err.message, "error");
      }
    });

    headerRight.appendChild(statusSelect);
    headerRight.appendChild(delBtn);

    header.appendChild(headerLeft);
    header.appendChild(headerRight);
    card.appendChild(header);

    // Body
    const body = document.createElement("div");
    body.className = "stage-body";

    if (stage.description) {
      const descEl = document.createElement("div");
      descEl.className = "stage-description";
      descEl.textContent = stageDescriptionText(stage.description);
      body.appendChild(descEl);
    }

    // Embed Questions
    const questionsEl = renderQuestionList(stage.id, stage.questions || [], onRefresh);
    body.appendChild(questionsEl);

    card.appendChild(body);
    container.appendChild(card);
  });

  // Add New Stage Box
  const addStageBox = document.createElement("div");
  addStageBox.className = "add-stage-box";
  addStageBox.innerHTML = `
    <button type="button" class="btn-new-stage inline-icon-text">${icon("plus", 12)} ${escapeHtml(t("stage.addInterviewStage"))}</button>
  `;

  addStageBox.querySelector(".btn-new-stage").addEventListener("click", () => {
    renderInlineStageForm(addStageBox, jobId, onRefresh);
  });

  container.appendChild(addStageBox);
  return container;
}

/**
 * Renders an inline form to add a new stage.
 */
function renderInlineStageForm(containerEl, jobId, onRefresh) {
  containerEl.innerHTML = `
    <form class="inline-stage-form" style="width: 100%; display: flex; flex-direction: column; gap: 10px;">
      <div style="display: flex; gap: 10px; flex-wrap: wrap;">
        <select class="stage-type-input" style="background: var(--bg-surface); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 6px 10px; color: var(--text-primary);">
          <option value="HR">${escapeHtml(t("stageType.option.hr"))}</option>
          <option value="Technical" selected>${escapeHtml(t("stageType.option.technical"))}</option>
          <option value="Cultural">${escapeHtml(t("stageType.option.cultural"))}</option>
          <option value="Offer &amp; Decision">${escapeHtml(t("stageType.option.offerDecision"))}</option>
        </select>
        <input type="text" class="stage-title-input" placeholder="${escapeAttr(t("stage.titlePlaceholder"))}" style="flex: 1; background: var(--bg-surface); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 6px 12px; color: var(--text-primary);" required />
      </div>
      <input type="text" class="stage-desc-input" placeholder="${escapeAttr(t("stage.focusPlaceholder"))}" style="background: var(--bg-surface); border: 1px solid var(--border-medium); border-radius: var(--radius-sm); padding: 6px 12px; color: var(--text-primary);" />
      <div style="display: flex; gap: 8px; justify-content: flex-end;">
        <button type="button" class="btn-cancel-stage btn-secondary" style="padding: 4px 12px; font-size: 12px;">${escapeHtml(t("common.cancel"))}</button>
        <button type="submit" class="btn-primary" style="padding: 4px 14px; font-size: 12px;">${escapeHtml(t("stage.save"))}</button>
      </div>
    </form>
  `;

  const form = containerEl.querySelector(".inline-stage-form");
  const cancelBtn = containerEl.querySelector(".btn-cancel-stage");

  cancelBtn.addEventListener("click", () => {
    if (onRefresh) onRefresh();
  });

  form.addEventListener("submit", async (e) => {
    try {
      e.preventDefault();
      const type = form.querySelector(".stage-type-input").value;
      const title = form.querySelector(".stage-title-input").value.trim();
      const desc = form.querySelector(".stage-desc-input").value.trim();
      if (!title) {
        form.querySelector(".stage-title-input").focus();
        return;
      }

      await api.createStage({
        job_id: jobId,
        stage_type: type,
        custom_title: title,
        description: desc,
        status: "pending",
      });

      if (onRefresh) onRefresh();
    } catch (err) {
      showToast(err.message, "error");
    }
  });
}
