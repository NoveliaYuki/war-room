/**
 * @fileoverview Clean async API fetch wrapper for client-server communication.
 */

/** Encodes an identifier as one safe API path segment. */
function pathSegment(value) {
  return encodeURIComponent(String(value));
}

/** Sends typed API requests for jobs, stages, questions, and attachments. */
const backendApi = {
  /**
   * Fetches job processes with optional status and search filters.
   */
  async getJobs(status = "all", search = "") {
    const params = new URLSearchParams();
    if (status && status !== "all") params.set("status", status);
    if (search && search.trim()) params.set("search", search.trim());

    const res = await fetch(`/api/jobs?${params.toString()}`);
    if (!res.ok) throw new Error(`HTTP error: ${res.status}`);
    return res.json();
  },

  /**
   * Retrieves aggregate counts for filter tabs.
   */
  async getJobCounts() {
    const res = await fetch("/api/jobs/counts");
    if (!res.ok) throw new Error(`HTTP error: ${res.status}`);
    return res.json();
  },

  /**
   * Fetches full composite job details by ID.
   */
  async getJob(id) {
    const res = await fetch(`/api/jobs/${pathSegment(id)}`);
    if (!res.ok) throw new Error(`HTTP error: ${res.status}`);
    return res.json();
  },

  /**
   * Creates a new job process.
   */
  async createJob(payload) {
    const res = await fetch("/api/jobs", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: "Failed to create" }));
      throw new Error(err.error || "Failed to create job process");
    }
    return res.json();
  },

  /**
   * Updates an existing job process.
   */
  async updateJob(id, payload) {
    const res = await fetch(`/api/jobs/${pathSegment(id)}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    if (!res.ok) throw new Error(`HTTP error: ${res.status}`);
    return res.json();
  },

  /**
   * Deletes a job process.
   */
  async deleteJob(id) {
    const res = await fetch(`/api/jobs/${pathSegment(id)}`, { method: "DELETE" });
    if (!res.ok) throw new Error(`HTTP error: ${res.status}`);
    return res.json();
  },

  /**
   * Reorders selection processes cards in the grid.
   */
  async reorderJobs(jobIds) {
    const res = await fetch("/api/jobs/reorder", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ job_ids: jobIds }),
    });
    if (!res.ok) throw new Error(`HTTP error: ${res.status}`);
    return res.json();
  },

  /**
   * Adds a new interview stage.
   */
  async createStage(payload) {
    const res = await fetch("/api/stages", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    if (!res.ok) throw new Error(`HTTP error: ${res.status}`);
    return res.json();
  },

  /**
   * Updates a stage.
   */
  async updateStage(id, payload) {
    const res = await fetch(`/api/stages/${pathSegment(id)}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    if (!res.ok) throw new Error(`HTTP error: ${res.status}`);
    return res.json();
  },

  /**
   * Sets an interview stage as the current step in the process with 1 single click.
   */
  async setCurrentStage(stageId) {
    const res = await fetch(`/api/stages/${pathSegment(stageId)}/set-current`, {
      method: "PUT",
    });
    if (!res.ok) throw new Error(`HTTP error: ${res.status}`);
    return res.json();
  },

  /**
   * Fetches all scheduled meetings or filters by date.
   */
  async getMeetings(date = "") {
    const params = date ? `?date=${encodeURIComponent(date)}` : "";
    const res = await fetch(`/api/meetings${params}`);
    if (!res.ok) throw new Error(`HTTP error: ${res.status}`);
    return res.json();
  },

  /**
   * Schedules or updates an interview meeting date and time.
   */
  async scheduleMeeting(stageId, payload) {
    const res = await fetch(`/api/stages/${pathSegment(stageId)}/schedule`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    if (!res.ok) throw new Error(`HTTP error: ${res.status}`);
    return res.json();
  },

  /**
   * Deletes a stage.
   */
  async deleteStage(id) {
    const res = await fetch(`/api/stages/${pathSegment(id)}`, { method: "DELETE" });
    if (!res.ok) throw new Error(`HTTP error: ${res.status}`);
    return res.json();
  },

  /**
   * Reorders stages for a job.
   */
  async reorderStages(jobId, stageIds) {
    const res = await fetch("/api/stages/reorder", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ job_id: jobId, stage_ids: stageIds }),
    });
    if (!res.ok) throw new Error(`HTTP error: ${res.status}`);
    return res.json();
  },

  /**
   * Adds an interview question to a stage.
   */
  async createQuestion(payload) {
    const res = await fetch("/api/questions", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    if (!res.ok) throw new Error(`HTTP error: ${res.status}`);
    return res.json();
  },

  /**
   * Reorders interview questions within a stage.
   */
  async reorderQuestions(stageId, questionIds) {
    const res = await fetch("/api/questions/reorder", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ stage_id: stageId, question_ids: questionIds }),
    });
    if (!res.ok) throw new Error(`HTTP error: ${res.status}`);
    return res.json();
  },

  /**
   * Updates an interview question.
   */
  async updateQuestion(id, payload) {
    const res = await fetch(`/api/questions/${pathSegment(id)}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    if (!res.ok) throw new Error(`HTTP error: ${res.status}`);
    return res.json();
  },

  /**
   * Deletes an interview question.
   */
  async deleteQuestion(id) {
    const res = await fetch(`/api/questions/${pathSegment(id)}`, { method: "DELETE" });
    if (!res.ok) throw new Error(`HTTP error: ${res.status}`);
    return res.json();
  },

  /**
   * Uploads a file attachment.
   */
  async uploadAttachment(formData) {
    const res = await fetch("/api/attachments", {
      method: "POST",
      body: formData,
    });
    if (!res.ok) throw new Error(`HTTP error: ${res.status}`);
    return res.json();
  },

  /**
   * Deletes an attachment.
   */
  async deleteAttachment(id) {
    const res = await fetch(`/api/attachments/${pathSegment(id)}`, { method: "DELETE" });
    if (!res.ok) throw new Error(`HTTP error: ${res.status}`);
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
