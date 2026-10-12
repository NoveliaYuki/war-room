let dialogSequence = 0;

/** Shows a themed confirmation dialog and resolves to whether the action was confirmed. */
export function confirmAction({ title, message, confirmLabel = "Continue", cancelLabel = "Cancel", danger = false }) {
  const dialog = document.createElement("dialog");
  if (typeof dialog.showModal !== "function") {
    return Promise.resolve(window.confirm(`${title}\n\n${message}`));
  }

  const sequence = ++dialogSequence;
  const titleId = `confirm-dialog-title-${sequence}`;
  const messageId = `confirm-dialog-message-${sequence}`;
  const heading = document.createElement("h2");
  heading.className = "app-confirm-title";
  heading.id = titleId;
  heading.textContent = title;
  const description = document.createElement("p");
  description.className = "app-confirm-message";
  description.id = messageId;
  description.textContent = message;
  const actions = document.createElement("div");
  actions.className = "app-confirm-actions";
  const cancelButton = document.createElement("button");
  cancelButton.className = "btn-secondary";
  cancelButton.type = "button";
  cancelButton.textContent = cancelLabel;
  const confirmButton = document.createElement("button");
  confirmButton.className = danger ? "btn-danger" : "btn-primary";
  confirmButton.type = "button";
  confirmButton.textContent = confirmLabel;
  actions.append(cancelButton, confirmButton);
  dialog.className = "app-confirm-dialog";
  dialog.setAttribute("aria-labelledby", titleId);
  dialog.setAttribute("aria-describedby", messageId);
  dialog.append(heading, description, actions);

  return new Promise((resolve) => {
    const returnFocusTo = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    let settled = false;
    const settle = (confirmed) => {
      if (settled) return;
      settled = true;
      if (dialog.open) dialog.close();
      dialog.remove();
      if (returnFocusTo?.isConnected) returnFocusTo.focus();
      resolve(confirmed);
    };
    cancelButton.addEventListener("click", () => settle(false));
    confirmButton.addEventListener("click", () => settle(true));
    dialog.addEventListener("cancel", (event) => {
      event.preventDefault();
      settle(false);
    });
    dialog.addEventListener("keydown", (event) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      settle(false);
    });
    dialog.addEventListener("close", () => settle(false));
    dialog.addEventListener("click", (event) => {
      if (event.target !== dialog) return;
      const bounds = dialog.getBoundingClientRect();
      const outside = event.clientX < bounds.left || event.clientX > bounds.right
        || event.clientY < bounds.top || event.clientY > bounds.bottom;
      if (outside) settle(false);
    });
    document.body.append(dialog);
    dialog.showModal();
    cancelButton.focus();
  });
}
