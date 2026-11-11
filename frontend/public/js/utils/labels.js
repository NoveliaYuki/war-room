/**
 * @fileoverview Translated display labels for stored enum values.
 * The stored values stay in English; only what the user sees is translated.
 */

import { t } from "../i18n.js";

const ARRANGEMENT_KEYS = { remote: "arrangement.remote", hybrid: "arrangement.hybrid", on_site: "arrangement.onSite" };
const EMPLOYMENT_TYPE_KEYS = { permanent: "employment.permanent", b2b: "employment.b2b", permanent_b2b: "employment.permanentB2b" };
const STATUS_KEYS = { waiting: "status.waiting", ongoing: "status.ongoing", accepted: "status.accepted", rejected: "status.rejected" };
const STAGE_TYPE_KEYS = { HR: "stageType.hr", Technical: "stageType.technical", Cultural: "stageType.cultural", "Offer & Decision": "stageType.offerDecision" };
// The API and the demo store record these English history entries.
const HISTORY_ACTION_KEYS = {
  created: "history.created",
  "changed company": "history.changedCompany",
  "changed role": "history.changedRole",
  "changed role highlights": "history.changedRoleHighlights",
  "changed job description": "history.changedJobDescription",
  "changed company and role overview": "history.changedOverview",
  "changed tech stack": "history.changedTechStack",
};
const HISTORY_STATUS_KEYS = { waiting: "history.movedToWaiting", ongoing: "history.movedToOngoing", accepted: "history.movedToApproved", rejected: "history.movedToRejected" };
const MEETING_FORMAT_KEYS = { phone: "meeting.phoneCall", onsite: "meeting.onSite", video: "meeting.videoCall" };
// The API stores these English titles and descriptions on the default stages it creates.
const DEFAULT_STAGE_TITLE_KEYS = {
  "Recruiter Screen": "stageDefault.hrTitle",
  "Technical Evaluation": "stageDefault.technicalTitle",
  "Team & Leadership Alignment": "stageDefault.culturalTitle",
  "Offer & Decision": "stageType.offerDecision",
};
const DEFAULT_STAGE_DESCRIPTION_KEYS = {
  "Initial culture fit, role overview, and expectations alignment.": "stageDefault.hrDescription",
  "System architecture, coding, or take-home review.": "stageDefault.technicalDescription",
  "Discussion with cross-functional peers and leadership.": "stageDefault.culturalDescription",
  "Compensation proposal, equity review, and final agreement.": "stageDefault.offerDecisionDescription",
};

function lookup(keys, value, fallback) {
  return Object.hasOwn(keys, value) ? t(keys[value]) : fallback;
}

/** Returns the label for a work arrangement, or "" when it is not set. */
export function arrangementLabel(value) {
  return lookup(ARRANGEMENT_KEYS, value, "");
}

/** Returns the label for an employment type, or "" when it is not set. */
export function employmentTypeLabel(value) {
  return lookup(EMPLOYMENT_TYPE_KEYS, value, "");
}

/** Returns the label for a process status, falling back to the stored value. */
export function statusLabel(status) {
  return lookup(STATUS_KEYS, status, String(status ?? ""));
}

/** Returns the label for a stage type, falling back to the stored value. */
export function stageTypeLabel(type) {
  return lookup(STAGE_TYPE_KEYS, type, String(type ?? ""));
}

/** Returns a stage's custom title, or its translated type when it has none. */
export function stageName(stage) {
  return stage.custom_title ? stageTitleLabel(stage.custom_title) : stageTypeLabel(stage.stage_type);
}

/** Returns a stage title, translating a default stage title and keeping any other title as written. */
export function stageTitleLabel(title) {
  return lookup(DEFAULT_STAGE_TITLE_KEYS, title, title);
}

/** Returns a stage description, translating the description of a default stage and keeping any other as written. */
export function stageDescriptionText(description) {
  return lookup(DEFAULT_STAGE_DESCRIPTION_KEYS, description, description);
}

/** Returns a card history entry in the selected language; unrecognized entries are shown as saved. */
export function historyActionLabel(action) {
  const text = String(action);
  if (!text.startsWith("moved to ")) return lookup(HISTORY_ACTION_KEYS, text, text);
  const status = text.slice("moved to ".length);
  return lookup(HISTORY_STATUS_KEYS, status, t("history.movedTo", { status }));
}

/** Returns the label for a meeting format; unknown formats read as video calls. */
export function meetingFormatLabel(type) {
  return lookup(MEETING_FORMAT_KEYS, type, t(MEETING_FORMAT_KEYS.video));
}

// The API stores an undisclosed company as this English placeholder.
const UNDISCLOSED_COMPANY = "unknown";

/** Returns whether a stored company name is the placeholder for an undisclosed company. */
export function isUndisclosedCompany(name) {
  return String(name ?? "").trim().toLowerCase() === UNDISCLOSED_COMPANY;
}

/** Returns a company name to show in text, translating the undisclosed placeholder. */
export function companyText(name) {
  return isUndisclosedCompany(name) ? t("company.unknown") : name;
}

/** Returns the value an edit field starts with; the undisclosed placeholder starts empty. */
export function editableCompanyName(name) {
  return isUndisclosedCompany(name) ? "" : name;
}
