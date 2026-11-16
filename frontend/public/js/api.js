/**
 * @fileoverview Clean async API fetch wrapper for client-server communication.
 */

import { t } from "./i18n.js";
import { serverMessage } from "./utils/serverMessages.js";

/** Fetches a resource, reporting a failed connection in the selected language. */
async function send(...request) {
  try {
    return await fetch(...request);
  } catch (error) {
    if (error?.name === "AbortError") throw error;
    throw new Error(t("api.networkError"), { cause: error });
  }
}

/** Encodes an identifier as one safe API path segment. */
function pathSegment(value) {
  return encodeURIComponent(String(value));
}

/** Sends typed API requests for jobs, stages, questions, and attachments. */
const backendApi = {
  getCompanyLogoUrl(companyName, companyDomain, jobId) {
    return `/api/company-logo?company=${encodeURIComponent(companyName)}&domain=${encodeURIComponent(companyDomain || "")}&job_id=${encodeURIComponent(jobId || "")}`;
  },

  /** Returns the signed-in user, or null when not signed in. */
  async getAuthMe() {
    const res = await send("/api/auth/me", { credentials: "same-origin", cache: "no-store" });
    if (!res.ok) return null;
    return res.json();
  },

  /** Ends only the current device's session. */
  async signOut() {
    const res = await send("/api/auth/signout", { method: "POST" });
    if (!res.ok) throw new Error(t("api.httpError", { status: res.status }));
    return res.json();
  },

  /** Uploads a new custom profile picture (PNG or JPEG, up to 5 MiB). */
  async uploadAvatar(file) {
    const form = new FormData();
    form.append("file", file);
    const res = await send("/api/account/avatar", { method: "POST", body: form });
    if (!res.ok) {
      const error = await res.json().catch(() => ({ error: t("api.updatePhotoFailed") }));
      throw new Error(serverMessage(error.error, t("api.updatePhotoFailed")));
    }
    return res.json();
  },

  /** Reverts to the Google-provided photo (or fallback initial) by clearing the custom avatar. */
  async removeAvatar() {
    const res = await send("/api/account/avatar", { method: "DELETE" });
    if (!res.ok) throw new Error(t("api.httpError", { status: res.status }));
    return res.json();
  },
  /** Downloads an encrypted portable backup. */
  async exportBackup() {
    const res = await send("/api/backup/export");
    if (!res.ok) {
      const error = await res.json().catch(() => ({ error: t("api.createBackupFailed") }));
      throw new Error(serverMessage(error.error, t("api.createBackupFailed")));
    }
    return res.blob();
  },

  /** Imports an encrypted backup, decrypting it before replacing saved processes. */
  async importBackup(file, allowEmpty = false) {
    const form = new FormData();
    form.append("backup", file);
    form.append("allow_empty", String(allowEmpty));
    const res = await send("/api/backup/import", { method: "POST", body: form });
    if (!res.ok) {
      const error = await res.json().catch(() => ({ error: t("api.importBackupFailed") }));
      const exception = new Error(serverMessage(error.error, t("api.importBackupFailed")));
      exception.requiresEmptyConfirmation = error.requires_confirmation === true;
      throw exception;
    }
    return res.json();
  },

  /** Returns the Google Drive connection status (no tokens). */
  async getGoogleStatus() {
    const res = await send("/api/google/status");
    if (!res.ok) throw new Error(t("api.httpError", { status: res.status }));
    return res.json();
  },

  /** Lists Drive folders the user can choose as the backup destination. */
  async getGoogleFolders(query = "") {
    const res = await send(`/api/google/folders?q=${encodeURIComponent(query)}`);
    if (!res.ok) {
      const error = await res.json().catch(() => ({ error: t("api.listDriveFoldersFailed") }));
      throw new Error(serverMessage(error.error, t("api.listDriveFoldersFailed")));
    }
    return res.json();
  },

  /** Returns a short-lived Drive token for the user-invoked Google Picker. */
  async getGooglePickerToken() {
    const res = await send("/api/google/picker-token", { cache: "no-store" });
    if (!res.ok) {
      const error = await res.json().catch(() => ({ error: t("api.openDrivePickerFailed") }));
      throw new Error(serverMessage(error.error, t("api.openDrivePickerFailed")));
    }
    return res.json();
  },

  /** Saves the chosen Drive backup folder (empty id means Drive root). */
  async setGoogleFolder(folderId, folderName) {
    const res = await send("/api/google/folder", {
      method: "PUT", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ folder_id: folderId, folder_name: folderName }),
    });
    if (!res.ok) {
      const error = await res.json().catch(() => ({ error: t("api.saveDriveFolderFailed") }));
      throw new Error(serverMessage(error.error, t("api.saveDriveFolderFailed")));
    }
    return res.json();
  },

  /** Revokes the Google Drive grant and signs out every session for the account. */
  async revokeGoogleDrive() {
    const res = await send("/api/google/revoke", { method: "POST" });
    if (!res.ok) throw new Error(t("api.httpError", { status: res.status }));
    return res.json();
  },

  /** Uploads a Drive backup immediately. */
  async backupNowGoogle() {
    const res = await send("/api/google/backup", { method: "POST" });
    if (!res.ok) {
      const error = await res.json().catch(() => ({ error: t("api.driveBackupFailed") }));
      throw new Error(serverMessage(error.error, t("api.driveBackupFailed")));
    }
    return res.json();
  },

  /** Lists the saved versions of the shared CV. */
  async getCvVersions() {
    const res = await send("/api/cv/versions");
    if (!res.ok) throw new Error(t("api.httpError", { status: res.status }));
    return res.json();
  },

  /** Uploads a new version of the shared CV. */
  async uploadCvVersion(file) {
    const form = new FormData();
    form.append("file", file);
    const res = await send("/api/cv/versions", { method: "POST", body: form });
    if (!res.ok) {
      const error = await res.json().catch(() => ({ error: t("api.uploadCvFailed") }));
      throw new Error(serverMessage(error.error, t("api.uploadCvFailed")));
    }
    return res.json();
  },

  /** Downloads a saved CV version. */
  async downloadCvVersion(id) {
    const res = await send(`/api/cv/versions/${pathSegment(id)}/download`);
    if (!res.ok) throw new Error(t("api.httpError", { status: res.status }));
    return res.blob();
  },

  /** Deletes an unassigned CV version. */
  async deleteCvVersion(id) {
    const res = await send(`/api/cv/versions/${pathSegment(id)}`, { method: "DELETE" });
    if (!res.ok) {
      const error = await res.json().catch(() => ({ error: t("api.httpError", { status: res.status }) }));
      throw new Error(serverMessage(error.error, t("api.httpError", { status: res.status })));
    }
    return res.status === 204 ? { success: true } : res.json();
  },

  /** Selects or clears the CV version associated with a job. */
  async setJobCvVersion(jobId, versionId) {
    const res = await send(`/api/jobs/${pathSegment(jobId)}/cv-version`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ version_id: versionId }),
    });
    if (!res.ok) throw new Error(t("api.httpError", { status: res.status }));
    return res.json();
  },

  /**
   * Fetches job processes with optional status and search filters.
   */
  async getJobs(status = "all", search = "") {
    const params = new URLSearchParams();
    if (status && status !== "all") params.set("status", status);
    if (search && search.trim()) params.set("search", search.trim());

    const res = await send(`/api/jobs?${params.toString()}`);
    if (!res.ok) throw new Error(t("api.httpError", { status: res.status }));
    return res.json();
  },

  /** Lists dated job-search periods and their assignment counts. */
  async getSearchPeriods() {
    const res = await send("/api/search-periods");
    if (!res.ok) throw new Error(t("api.httpError", { status: res.status }));
    return res.json();
  },

  /** Creates a dated job-search period. */
  async createSearchPeriod(payload) {
    const res = await send("/api/search-periods", {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(payload),
    });
    if (!res.ok) {
      const error = await res.json().catch(() => ({ error: t("api.createSearchPeriodFailed") }));
      throw new Error(serverMessage(error.error, t("api.createSearchPeriodFailed")));
    }
    return res.json();
  },

  /** Updates a dated job-search period. */
  async updateSearchPeriod(id, payload) {
    const res = await send(`/api/search-periods/${pathSegment(id)}`, {
      method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(payload),
    });
    if (!res.ok) {
      const error = await res.json().catch(() => ({ error: t("api.updateSearchPeriodFailed") }));
      throw new Error(serverMessage(error.error, t("api.updateSearchPeriodFailed")));
    }
    return res.json();
  },

  /** Deletes a period and leaves its jobs unassigned. */
  async deleteSearchPeriod(id) {
    const res = await send(`/api/search-periods/${pathSegment(id)}`, { method: "DELETE" });
    if (!res.ok) {
      const error = await res.json().catch(() => ({ error: t("api.deleteSearchPeriodFailed") }));
      throw new Error(serverMessage(error.error, t("api.deleteSearchPeriodFailed")));
    }
    return res.json();
  },

  /**
   * Retrieves aggregate counts for filter tabs.
   */
  async getJobCounts() {
    const res = await send("/api/jobs/counts");
    if (!res.ok) throw new Error(t("api.httpError", { status: res.status }));
    return res.json();
  },

  /**
   * Fetches full composite job details by ID.
   */
  async getJob(id) {
    const res = await send(`/api/jobs/${pathSegment(id)}`);
    if (!res.ok) throw new Error(t("api.httpError", { status: res.status }));
    const job = await res.json();
    for (const attachment of job.attachments || []) {
      attachment.download_url = `/api/attachments/${pathSegment(attachment.id)}/download`;
    }
    return job;
  },

  /** Reads a posting link, or matches technologies in pasted text, without saving anything. */
  async previewJobImport(payload) {
    const res = await send("/api/jobs/import-preview", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    const body = await res.json().catch(() => ({}));
    if (res.ok) return body;
    throw Object.assign(new Error(serverMessage(body.error, t("api.importPostingFailed"))), { code: body.code, duplicate: body.duplicate });
  },

  /**
   * Creates a new job process.
   */
  async createJob(payload) {
    const res = await send("/api/jobs", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: t("api.createFailed") }));
      throw new Error(serverMessage(err.error, t("api.createJobFailed")));
    }
    return res.json();
  },

  /**
   * Updates an existing job process.
   */
  async updateJob(id, payload) {
    const res = await send(`/api/jobs/${pathSegment(id)}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    if (!res.ok) throw new Error(t("api.httpError", { status: res.status }));
    return res.json();
  },

  /**
   * Deletes a job process.
   */
  async deleteJob(id) {
    const res = await send(`/api/jobs/${pathSegment(id)}`, { method: "DELETE" });
    if (!res.ok) throw new Error(t("api.httpError", { status: res.status }));
    return res.json();
  },

  async getTechnologies() {
    const res = await send("/api/technologies");
    if (!res.ok) throw new Error(t("api.httpError", { status: res.status }));
    return res.json();
  },

  async createTechnology(payload) {
    const res = await send("/api/technologies", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(payload) });
    if (!res.ok) { const error = await res.json().catch(() => ({ error: t("api.createTechnologyFailed") })); throw new Error(serverMessage(error.error, t("api.createTechnologyFailed"))); }
    return res.json();
  },

  async updateTechnology(id, payload) {
    const res = await send(`/api/technologies/${pathSegment(id)}`, { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(payload) });
    if (!res.ok) { const error = await res.json().catch(() => ({ error: t("api.updateTechnologyFailed") })); throw new Error(serverMessage(error.error, t("api.updateTechnologyFailed"))); }
    return res.json();
  },

  async removeTechnologyAssignments(id) {
    const res = await send(`/api/technologies/${pathSegment(id)}/assignments`, { method: "DELETE" });
    if (!res.ok) throw new Error(t("api.httpError", { status: res.status }));
    return res.json();
  },

  async deleteTechnology(id) {
    const res = await send(`/api/technologies/${pathSegment(id)}`, { method: "DELETE" });
    if (!res.ok) { const error = await res.json().catch(() => ({ error: t("api.deleteTechnologyFailed") })); throw new Error(serverMessage(error.error, t("api.deleteTechnologyFailed"))); }
    return res.json();
  },

  /**
   * Reorders selection processes cards in the grid.
   */
  async reorderJobs(jobIds) {
    const res = await send("/api/jobs/reorder", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ job_ids: jobIds }),
    });
    if (!res.ok) throw new Error(t("api.httpError", { status: res.status }));
    return res.json();
  },

  /**
   * Adds a new interview stage.
   */
  async createStage(payload) {
    const res = await send("/api/stages", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    if (!res.ok) throw new Error(t("api.httpError", { status: res.status }));
    return res.json();
  },

  /**
   * Updates a stage.
   */
  async updateStage(id, payload) {
    const res = await send(`/api/stages/${pathSegment(id)}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    if (!res.ok) throw new Error(t("api.httpError", { status: res.status }));
    return res.json();
  },

  /**
   * Sets an interview stage as the current step in the process with 1 single click.
   */
  async setCurrentStage(stageId) {
    const res = await send(`/api/stages/${pathSegment(stageId)}/set-current`, {
      method: "PUT",
    });
    if (!res.ok) throw new Error(t("api.httpError", { status: res.status }));
    return res.json();
  },

  /**
   * Fetches all scheduled meetings or filters by date.
   */
  async getMeetings(date = "") {
    const params = date ? `?date=${encodeURIComponent(date)}` : "";
    const res = await send(`/api/meetings${params}`);
    if (!res.ok) throw new Error(t("api.httpError", { status: res.status }));
    return res.json();
  },

  /**
   * Schedules or updates an interview meeting date and time.
   */
  async scheduleMeeting(stageId, payload) {
    const res = await send(`/api/stages/${pathSegment(stageId)}/schedule`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    if (!res.ok) throw new Error(t("api.httpError", { status: res.status }));
    return res.json();
  },

  /**
   * Deletes a stage.
   */
  async deleteStage(id) {
    const res = await send(`/api/stages/${pathSegment(id)}`, { method: "DELETE" });
    if (!res.ok) throw new Error(t("api.httpError", { status: res.status }));
    return res.json();
  },

  /**
   * Reorders stages for a job.
   */
  async reorderStages(jobId, stageIds) {
    const res = await send("/api/stages/reorder", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ job_id: jobId, stage_ids: stageIds }),
    });
    if (!res.ok) throw new Error(t("api.httpError", { status: res.status }));
    return res.json();
  },

  /**
   * Adds an interview question to a stage.
   */
  async createQuestion(payload) {
    const res = await send("/api/questions", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    if (!res.ok) throw new Error(t("api.httpError", { status: res.status }));
    return res.json();
  },

  /**
   * Reorders interview questions within a stage.
   */
  async reorderQuestions(stageId, questionIds) {
    const res = await send("/api/questions/reorder", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ stage_id: stageId, question_ids: questionIds }),
    });
    if (!res.ok) throw new Error(t("api.httpError", { status: res.status }));
    return res.json();
  },

  /**
   * Updates an interview question.
   */
  async updateQuestion(id, payload) {
    const res = await send(`/api/questions/${pathSegment(id)}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    if (!res.ok) throw new Error(t("api.httpError", { status: res.status }));
    return res.json();
  },

  /**
   * Deletes an interview question.
   */
  async deleteQuestion(id) {
    const res = await send(`/api/questions/${pathSegment(id)}`, { method: "DELETE" });
    if (!res.ok) throw new Error(t("api.httpError", { status: res.status }));
    return res.json();
  },

  /**
   * Uploads a file attachment.
   */
  async uploadAttachment(formData) {
    const res = await send("/api/attachments", {
      method: "POST",
      body: formData,
    });
    if (!res.ok) throw new Error(t("api.httpError", { status: res.status }));
    return res.json();
  },

  /**
   * Deletes an attachment.
   */
  async deleteAttachment(id) {
    const res = await send(`/api/attachments/${pathSegment(id)}`, { method: "DELETE" });
    if (!res.ok) throw new Error(t("api.httpError", { status: res.status }));
    return res.json();
  },
};

let activeApi = backendApi;

/** Selects the browser-side implementation before app components are loaded. */
export function useApiAdapter(adapter) {
  activeApi = adapter;
}

/** Dispatches API calls to the currently selected backend or demo adapter. */
export const api = new Proxy({}, {
  get(_target, property) {
    return activeApi[property];
  },
});
