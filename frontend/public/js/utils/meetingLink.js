/**
 * @fileoverview Shared rendering for links attached to scheduled meetings.
 */

import { icon } from "../icons.js";
import { t } from "../i18n.js";
import { escapeHtml } from "./sanitize.js";

/**
 * Renders a meeting link's icon and label for its format.
 *
 * @param {string} meetingType - Meeting format from the backend.
 * @param {string} joinLabel - Label shown for links that join a call.
 * @param {number} size - Leading icon size in pixels.
 * @returns {string} HTML for the link content; on-site meetings link to Maps.
 */
export function renderMeetingLinkContent(meetingType, joinLabel, size) {
  const [iconName, label] = meetingType === "onsite" ? ["pin", t("meeting.openInMaps")] : ["video", joinLabel];
  return `${icon(iconName, size)} ${escapeHtml(label)} ${icon("arrowUpRight", 11)}`;
}
