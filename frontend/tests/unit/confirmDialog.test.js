import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { confirmAction } from "../../public/js/utils/confirmDialog.js";

describe("themed confirmation dialog", () => {
  beforeEach(() => {
    document.body.innerHTML = '<button id="origin">Delete</button>';
  });

  afterEach(() => {
    document.body.querySelectorAll("dialog.app-confirm-dialog").forEach((dialog) => dialog.remove());
    vi.restoreAllMocks();
  });

  it("resolves false when canceled and returns focus to the opener", async () => {
    const origin = document.querySelector("#origin");
    origin.focus();
    const result = confirmAction({ title: "Delete process?", message: "This cannot be undone." });
    const dialog = document.querySelector("dialog.app-confirm-dialog");

    expect(dialog.getAttribute("aria-labelledby")).toBeTruthy();
    expect(dialog.getAttribute("aria-describedby")).toBeTruthy();
    dialog.querySelector(".btn-secondary").click();

    await expect(result).resolves.toBe(false);
    expect(dialog.isConnected).toBe(false);
    expect(document.activeElement).toBe(origin);
  });

  it("resolves true when the primary action is confirmed", async () => {
    const result = confirmAction({ title: "Clear schedule?", message: "The date will be removed.", confirmLabel: "Clear schedule" });
    document.querySelector("dialog.app-confirm-dialog .btn-primary").click();
    await expect(result).resolves.toBe(true);
  });

  it("resolves false on Escape and ignores repeated settlement", async () => {
    const result = confirmAction({ title: "Discard changes?", message: "Unsaved changes will be lost." });
    const dialog = document.querySelector("dialog.app-confirm-dialog");
    dialog.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }));
    dialog.querySelector(".btn-primary").click();

    await expect(result).resolves.toBe(false);
    expect(dialog.isConnected).toBe(false);
  });

  it("uses the native confirm fallback when modal dialogs are unavailable", async () => {
    const showModal = HTMLDialogElement.prototype.showModal;
    const confirm = vi.fn(() => true);
    vi.stubGlobal("confirm", confirm);
    Object.defineProperty(HTMLDialogElement.prototype, "showModal", { configurable: true, value: undefined });
    try {
      await expect(confirmAction({ title: "Delete?", message: "Remove this job?" })).resolves.toBe(true);
      expect(confirm).toHaveBeenCalledWith("Delete?\n\nRemove this job?");
    } finally {
      Object.defineProperty(HTMLDialogElement.prototype, "showModal", { configurable: true, value: showModal });
    }
  });

  it("resolves false when the user clicks outside the dialog", async () => {
    const origin = document.querySelector("#origin");
    origin.focus();
    const result = confirmAction({ title: "Delete?", message: "Remove this job?" });
    const dialog = document.querySelector("dialog.app-confirm-dialog");
    dialog.dispatchEvent(new MouseEvent("click", { bubbles: true, clientX: -1, clientY: -1 }));

    await expect(result).resolves.toBe(false);
    expect(dialog.isConnected).toBe(false);
    expect(document.activeElement).toBe(origin);
  });

  it("keeps the dialog open for clicks inside its bounds", async () => {
    const result = confirmAction({ title: "Delete?", message: "Remove this job?" });
    const dialog = document.querySelector("dialog.app-confirm-dialog");
    vi.spyOn(dialog, "getBoundingClientRect").mockReturnValue({ left: 10, right: 100, top: 20, bottom: 80 });
    dialog.dispatchEvent(new MouseEvent("click", { bubbles: true, clientX: 50, clientY: 50 }));

    expect(dialog.isConnected).toBe(true);
    dialog.querySelector(".btn-secondary").click();
    await expect(result).resolves.toBe(false);
  });

  it("uses a danger action and keeps the dialog open for unrelated keys", async () => {
    const result = confirmAction({ title: "Delete job?", message: "This cannot be undone.", danger: true });
    const dialog = document.querySelector("dialog.app-confirm-dialog");
    const unrelatedKey = new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true });
    dialog.dispatchEvent(unrelatedKey);

    expect(dialog.querySelector(".btn-danger").textContent).toBe("Continue");
    expect(dialog.isConnected).toBe(true);
    expect(unrelatedKey.defaultPrevented).toBe(false);
    dialog.querySelector(".btn-danger").click();
    await expect(result).resolves.toBe(true);
  });

  it("cancels when the native dialog emits a cancel event", async () => {
    const result = confirmAction({ title: "Discard changes?", message: "Your edits will be lost." });
    const dialog = document.querySelector("dialog.app-confirm-dialog");
    const cancel = new Event("cancel", { bubbles: true, cancelable: true });
    dialog.dispatchEvent(cancel);

    expect(cancel.defaultPrevented).toBe(true);
    await expect(result).resolves.toBe(false);
  });

  it("settles when the native dialog closes before a button is selected", async () => {
    const result = confirmAction({ title: "Discard changes?", message: "Your edits will be lost." });
    const dialog = document.querySelector("dialog.app-confirm-dialog");
    dialog.close();

    await expect(result).resolves.toBe(false);
    expect(dialog.isConnected).toBe(false);
  });

  it("resolves when the dialog opens without an element to return focus to", async () => {
    const descriptor = Object.getOwnPropertyDescriptor(document, "activeElement");
    Object.defineProperty(document, "activeElement", { configurable: true, value: document });
    const result = confirmAction({ title: "Continue?", message: "Proceed without a focused opener." });
    if (descriptor) Object.defineProperty(document, "activeElement", descriptor);
    else delete document.activeElement;

    document.querySelector("dialog.app-confirm-dialog .btn-secondary").click();
    await expect(result).resolves.toBe(false);
  });

  it("does not restore focus to an opener removed while the dialog was open", async () => {
    const origin = document.querySelector("#origin");
    origin.focus();
    const result = confirmAction({ title: "Discard?", message: "The form will reset." });
    origin.remove();
    document.querySelector("dialog.app-confirm-dialog .btn-secondary").click();

    await expect(result).resolves.toBe(false);
    expect(document.activeElement).not.toBe(origin);
  });
});
