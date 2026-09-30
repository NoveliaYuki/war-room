/** Formats a date using the viewer's local calendar day. */
export function formatLocalDate(date) {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

/** Returns a stable, random-looking day offset within the month around today. */
export function getDemoMeetingDayOffset(stageId, date, direction) {
  const seed = `${stageId}:${formatLocalDate(new Date(date))}`;
  let hash = 2166136261;
  for (const character of seed) {
    hash ^= character.charCodeAt(0);
    hash = Math.imul(hash, 16777619);
  }
  const days = (hash >>> 0) % 30 + 1;
  return direction === "past" ? -days : days;
}

/** Updates a demo meeting date and keeps its timestamp aligned to its local time. */
export function setDemoMeetingDate(stage, today, dayOffset) {
  const date = new Date(`${today}T12:00:00`);
  date.setDate(date.getDate() + dayOffset);
  stage.meeting_date = formatLocalDate(date);
  stage.meeting_time ||= "10:00–10:45";
  const startTime = stage.meeting_time.split(/[–-]/)[0].trim();
  stage.scheduled_at = new Date(`${stage.meeting_date}T${startTime}:00`).getTime();
}
