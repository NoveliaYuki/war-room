const INACTIVITY_PERIOD_SECONDS = 30 * 24 * 60 * 60;

/** Moves inactive waiting jobs to rejected and reports whether any job changed.
 * @param {Array<Record<string, any>>} jobs - Demo jobs to evaluate.
 * @param {number} [nowMs=Date.now()] - Current time in milliseconds.
 * @returns {boolean} Whether a job was rejected.
 */
export function applyInactiveWaitingPolicy(jobs, nowMs = Date.now()) {
  const nowSeconds = Math.floor(nowMs / 1000);
  const cutoff = nowSeconds - INACTIVITY_PERIOD_SECONDS;
  let changed = false;
  jobs.forEach((job) => {
    if (job.status !== "waiting" || Number(job.last_activity_at) > cutoff) return;
    job.status = "rejected";
    job.status_changed_at = nowSeconds;
    job.history ||= [];
    job.history.push({ occurred_at: nowSeconds, action: "moved to rejected" });
    job.automatic_rejection_reason = "Automatically rejected after 30 days without saved activity.";
    changed = true;
  });
  return changed;
}

/** Records a saved change to a demo job.
 * @param {Record<string, any>} job - Demo job to update.
 * @param {number} [nowMs=Date.now()] - Current time in milliseconds.
 */
export function touchDemoJob(job, nowMs = Date.now()) {
  job.last_activity_at = Math.floor(nowMs / 1000);
  job.automatic_rejection_reason = "";
}
