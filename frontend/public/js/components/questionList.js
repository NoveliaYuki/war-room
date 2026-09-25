import { showToast } from "../utils/toast.js";
import { escapeHtml, escapeAttr } from "../utils/sanitize.js";
/**
 * @fileoverview Interview Question List Component.
 * Allows candidates to prepare questions for each interview round and log answers.
 */

import { api } from "../api.js";
import { icon } from "../icons.js";

/** Moves a placeholder before a target only when its position would change. */
function movePlaceholderBefore(listEl, placeholder, target) {
  if (placeholder.nextElementSibling !== target) listEl.insertBefore(placeholder, target);
}

/** Appends a placeholder only when it is not already last. */
function appendPlaceholder(listEl, placeholder) {
  if (placeholder.nextElementSibling !== null) listEl.appendChild(placeholder);
}

/** Returns whether the dragged item has crossed above the first item. */
function crossedTopBoundary(midY, top, firstRect) {
  return midY < firstRect.top + firstRect.height / 2 || top <= firstRect.top;
}

/** Returns whether the dragged item has crossed below the last item. */
function crossedBottomBoundary(midY, bottom, lastRect) {
  return midY > lastRect.top + lastRect.height / 2 || bottom >= lastRect.bottom;
}

/** Places a placeholder before the first item below the dragged midpoint. */
function placeBetweenItems(items, listEl, placeholder, midY) {
  for (const target of items) {
    const rect = target.getBoundingClientRect();
    if (midY < rect.top + rect.height / 2) {
      movePlaceholderBefore(listEl, placeholder, target);
      return true;
    }
  }
  return false;
}

/**
 * Renders the question list and creation form for a specific stage.
 *
 * @param {string} stageId - The parent stage identifier.
 * @param {Array} questions - Array of question records.
 * @param {Function} onUpdated - Callback when questions are added, modified, or removed.
 * @returns {HTMLElement} The rendered DOM element.
 */
export function renderQuestionList(stageId, questions = [], onUpdated) {
  const container = document.createElement("div");
  container.className = "stage-questions-section";

  const header = document.createElement("div");
  header.className = "questions-header";
  header.innerHTML = `
    <span>Questions to Ask (${questions.length})</span>
  `;
  container.appendChild(header);

  const listEl = document.createElement("div");
  listEl.className = "questions-list";

  questions.forEach((q) => {
    const itemEl = document.createElement("div");
    itemEl.className = "question-item";

    const rowTop = document.createElement("div");
    rowTop.className = "question-row-top";

    const textEl = document.createElement("div");
    textEl.className = "question-text";
    textEl.textContent = q.question;

    const actionsGroup = document.createElement("div");
    actionsGroup.style.display = "flex";
    actionsGroup.style.alignItems = "center";
    actionsGroup.style.gap = "4px";

    const delBtn = document.createElement("button");
    delBtn.className = "question-delete-btn";
    delBtn.innerHTML = icon("close", 11);
    delBtn.title = "Delete question";
    delBtn.addEventListener("click", async () => {
      try {
        await api.deleteQuestion(q.id);
        if (onUpdated) onUpdated();
      } catch (err) {
        showToast(err.message, "error");
      }
    });

    const dragHandle = document.createElement("button");
    dragHandle.type = "button";
    dragHandle.className = "question-drag-handle q-grip";
    dragHandle.innerHTML = icon("gripLines", 14);
    dragHandle.title = "Hold and drag to reorder questions";

    actionsGroup.appendChild(delBtn);
    actionsGroup.appendChild(dragHandle);

    rowTop.appendChild(textEl);
    rowTop.appendChild(actionsGroup);
    itemEl.appendChild(rowTop);
    itemEl.setAttribute("data-qid", q.id);

    // Answer notes box
    const answerBox = document.createElement("textarea");
    answerBox.className = "question-answer-box";
    answerBox.placeholder = "Log interviewer's response or your notes...";
    answerBox.value = q.answer_notes || "";

    // Debounced blur save
    answerBox.addEventListener("change", async () => {
      try {
        await api.updateQuestion(q.id, { answer_notes: answerBox.value });
      } catch (err) {
        showToast(err.message, "error");
      }
    });

    itemEl.appendChild(answerBox);
    listEl.appendChild(itemEl);
  });

  enableQuestionReordering(listEl, stageId, onUpdated);

  container.appendChild(listEl);

  // Form to add a new question
  const form = document.createElement("form");
  form.className = "add-question-form";
  form.innerHTML = `
    <input type="text" class="add-question-input" placeholder="Add a question to ask in this interview..." required />
    <button type="submit" class="btn-add-question inline-icon-text">${icon("plus", 11)} Add</button>
  `;

  form.addEventListener("submit", async (e) => {
    try {
      e.preventDefault();
      const input = form.querySelector(".add-question-input");
      const val = input.value.trim();
      if (!val) return;

      await api.createQuestion({
        stage_id: stageId,
        question: val,
        answer_notes: "",
      });

      input.value = "";
      if (onUpdated) onUpdated();
    } catch (err) {
      showToast(err.message, "error");
    }
  });

  container.appendChild(form);
  return container;
}

/**
 * Enables smooth pointer-event drag-and-drop reordering of interview questions.
 *
 * @param {HTMLElement} listEl - The questions list container.
 * @param {string} stageId - The stage UUID.
 * @param {Function} onReordered - Callback after question order has changed.
 */
export function enableQuestionReordering(listEl, stageId, onReordered) {
  if (!listEl) return;
  const handles = listEl.querySelectorAll(".question-drag-handle, .q-grip");

  handles.forEach((handle) => {
    handle.addEventListener("pointerdown", (e) => {
      if (e.button !== 0) return;
      e.preventDefault();
      e.stopPropagation();

      const item = handle.closest(".question-item");
      if (!item) return;

      const rect = item.getBoundingClientRect();
      const offsetY = e.clientY - rect.top;

      // Capture initial order to detect actual movement
      const initialOrder = Array.from(listEl.querySelectorAll(".question-item"))
        .map((el) => el.getAttribute("data-qid"))
        .filter(Boolean);

      // Create drop placeholder matching exact geometry
      const placeholder = document.createElement("div");
      placeholder.className = "question-drop-placeholder";
      placeholder.style.height = `${rect.height}px`;
      placeholder.style.width = "100%";
      placeholder.style.boxSizing = "border-box";
      listEl.insertBefore(placeholder, item);

      // Lift item for dragging (fixed coordinates)
      item.classList.add("is-dragging");
      item.style.position = "fixed";
      item.style.width = `${rect.width}px`;
      item.style.left = `${rect.left}px`;
      item.style.top = `${rect.top}px`;
      item.style.zIndex = "10000";
      item.style.margin = "0";
      item.style.pointerEvents = "none";
      item.style.boxSizing = "border-box";

      // Detect active scroll container
      const scrollContainer = listEl.scrollHeight > listEl.clientHeight ? listEl : listEl.closest(".split-pane-right");

      let autoScrollSpeed = 0;
      let scrollAnimId = null;
      let lastClientY = e.clientY;

      /**
       * Updates the placeholder location based on the physical center of the dragged card.
       * Provides perfectly symmetric behavior for moving both upwards and downwards.
       */
      const updatePlaceholderPosition = (clientY) => {
        const draggedTop = clientY - offsetY;
        const draggedHeight = rect.height;
        const draggedMidY = draggedTop + draggedHeight / 2;
        const draggedBottom = draggedTop + draggedHeight;

        const otherItems = Array.from(listEl.querySelectorAll(".question-item:not(.is-dragging)"));
        if (otherItems.length === 0) return;

        const firstItem = otherItems[0];
        const lastItem = otherItems[otherItems.length - 1];
        const firstRect = firstItem.getBoundingClientRect();
        const lastRect = lastItem.getBoundingClientRect();

        // 1. Dragged above top of first non-dragging item
        if (crossedTopBoundary(draggedMidY, draggedTop, firstRect)) {
          movePlaceholderBefore(listEl, placeholder, firstItem);
          return;
        }

        // 2. Dragged below bottom of last non-dragging item
        if (crossedBottomBoundary(draggedMidY, draggedBottom, lastRect)) {
          appendPlaceholder(listEl, placeholder);
          return;
        }

        // 3. Dragged between items
        const placed = placeBetweenItems(otherItems, listEl, placeholder, draggedMidY);
        if (!placed) appendPlaceholder(listEl, placeholder);
      };

      const startAutoScroll = () => {
        if (scrollAnimId || !scrollContainer) return;
        const step = () => {
          if (autoScrollSpeed !== 0 && scrollContainer) {
            scrollContainer.scrollTop += autoScrollSpeed;
            updatePlaceholderPosition(lastClientY);
          }
          scrollAnimId = requestAnimationFrame(step);
        };
        scrollAnimId = requestAnimationFrame(step);
      };

      const stopAutoScroll = () => {
        if (scrollAnimId) {
          cancelAnimationFrame(scrollAnimId);
          scrollAnimId = null;
        }
        autoScrollSpeed = 0;
      };

      const onPointerMove = (moveEv) => {
        lastClientY = moveEv.clientY;
        item.style.top = `${moveEv.clientY - offsetY}px`;

        // Check auto-scroll zones near top/bottom edges of the container
        if (scrollContainer) {
          const cRect = scrollContainer.getBoundingClientRect();
          const edgeThreshold = 40;
          const maxSpeed = 10;

          if (moveEv.clientY > cRect.bottom - edgeThreshold) {
            const intensity = Math.min(1, (moveEv.clientY - (cRect.bottom - edgeThreshold)) / edgeThreshold);
            autoScrollSpeed = Math.max(1, Math.round(maxSpeed * intensity));
            startAutoScroll();
          } else if (moveEv.clientY < cRect.top + edgeThreshold) {
            const intensity = Math.min(1, ((cRect.top + edgeThreshold) - moveEv.clientY) / edgeThreshold);
            autoScrollSpeed = -Math.max(1, Math.round(maxSpeed * intensity));
            startAutoScroll();
          } else {
            autoScrollSpeed = 0;
          }
        }

        updatePlaceholderPosition(moveEv.clientY);
      };

      const onPointerUp = async () => {
        stopAutoScroll();

        window.removeEventListener("pointermove", onPointerMove);
        window.removeEventListener("pointerup", onPointerUp);
        window.removeEventListener("pointercancel", onPointerUp);

        // Snap item back into DOM where placeholder is
        listEl.insertBefore(item, placeholder);
        placeholder.remove();

        item.classList.remove("is-dragging");
        item.style.position = "";
        item.style.width = "";
        item.style.left = "";
        item.style.top = "";
        item.style.zIndex = "";
        item.style.margin = "";
        item.style.pointerEvents = "";
        item.style.boxSizing = "";

        const newOrder = Array.from(listEl.querySelectorAll(".question-item"))
          .map((el) => el.getAttribute("data-qid"))
          .filter(Boolean);

        const hasChanged =
          newOrder.length === initialOrder.length &&
          newOrder.some((id, idx) => id !== initialOrder[idx]);

        if (hasChanged) {
          try {
            await api.reorderQuestions(stageId, newOrder);
            if (onReordered) onReordered(newOrder);
          } catch (err) {
            console.error("Failed to reorder questions:", err);
          }
        }
      };

      window.addEventListener("pointermove", onPointerMove);
      window.addEventListener("pointerup", onPointerUp);
      window.addEventListener("pointercancel", onPointerUp);
    });
  });
}
