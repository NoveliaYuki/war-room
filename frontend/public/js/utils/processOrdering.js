const SORT_MODE_STORAGE_PREFIX = "war-room.process.sort.";
const VALID_SORT_MODES = ["added-newest", "added-oldest", "status-newest", "status-oldest", "advanced", "early", "manual"];

/** Returns a validated saved card ordering, including the earlier key names. */
export function readSavedSortMode(storage, status) {
  try {
    const mode = storage.getItem(`${SORT_MODE_STORAGE_PREFIX}${status}`);
    const legacyModes = { newest: "added-newest", oldest: "added-oldest" };
    if (legacyModes[mode]) return legacyModes[mode];
    return VALID_SORT_MODES.includes(mode) ? mode : "added-newest";
  } catch {
    return "added-newest";
  }
}

/** Sorts status tabs by creation date, status-change date, or interview progress. */
export function sortJobsForDisplay(jobs, status, mode) {
  if (status === "all" || mode === "manual") return [...jobs].sort((a, b) => a.order_index - b.order_index);
  const progress = (job) => job.current_stage_index && job.total_stages_count
    ? job.current_stage_index / job.total_stages_count
    : 0;
  const compareNewest = (field) => (a, b) =>
    (b[field] - a[field]) || (b.created_at - a.created_at) || (b.order_index - a.order_index);
  const addedNewest = compareNewest("created_at");
  const statusNewest = compareNewest("status_changed_at");
  return [...jobs].sort((a, b) => {
    if (mode === "added-oldest") return addedNewest(b, a);
    if (mode === "status-newest") return statusNewest(a, b);
    if (mode === "status-oldest") return statusNewest(b, a);
    if (mode === "advanced") return (progress(b) - progress(a)) || addedNewest(a, b);
    if (mode === "early") return (progress(a) - progress(b)) || addedNewest(a, b);
    return addedNewest(a, b);
  });
}
