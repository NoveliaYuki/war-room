/**
 * @fileoverview Shared rendering for links attached to scheduled meetings.
 */

import { icon } from "../icons.js";

/**
 * Renders a meeting link's icon and label for its format.
 *
 * @param {string} meetingType - Meeting format from the backend.
 * @param {string} joinLabel - Label shown for links that join a call.
 * @param {number} size - Leading icon size in pixels.
 * @returns {string} HTML for the link content; on-site meetings link to Maps.
 */
export function renderMeetingLinkContent(meetingType, joinLabel, size) {
  const [iconName, label] = meetingType === "onsite" ? ["pin", "Open in Maps"] : ["video", joinLabel];
  return `${icon(iconName, size)} ${label} ${icon("arrowUpRight", 11)}`;
}
