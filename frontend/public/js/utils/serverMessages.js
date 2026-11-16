/**
 * @fileoverview Shows server error messages in the selected language.
 * The API answers in English, so each message it can send maps to a translation key here.
 */

import { getLanguage, t } from "../i18n.js";

const MESSAGE_KEYS = new Map(Object.entries({
  "sign in required": "server.signInRequired",
  "sign-in is unavailable": "server.signInUnavailable",
  "Could not start Google sign-in": "server.googleSignInFailed",
  "Could not sign out": "server.signOutFailed",
  "Internal server error": "server.internal",
  "internal server error": "server.internal",
  "Invalid JSON payload": "server.invalidRequest",
  "Invalid payload: job_ids array required": "server.invalidRequest",
  "Invalid payload: job_id and stage_ids array required": "server.invalidRequest",
  "Invalid payload: stage_id and question_ids array required": "server.invalidRequest",
  "Invalid ordering: every ID must exist once in this list": "server.invalidRequest",
  "Invalid CV version assignment": "server.invalidRequest",
  "job_id is required": "server.invalidRequest",
  "job_id and stage_id must reference an owned selection process": "server.invalidRequest",
  "Request origin is not allowed": "server.originNotAllowed",
  "cross-site requests are not allowed": "server.originNotAllowed",
  "origin is not allowed": "server.originNotAllowed",
  "Not found": "server.notFound",
  "Job process not found": "server.jobNotFound",
  "Job not found": "server.jobNotFound",
  "Failed to load job": "server.jobLoadFailed",
  "Stage not found": "server.stageNotFound",
  "Question not found": "server.questionNotFound",
  "Attachment not found": "server.attachmentNotFound",
  "Technology not found": "server.technologyNotFound",
  "Search period not found": "server.searchPeriodNotFound",
  "CV version not found": "server.cvNotFound",
  "Job or CV version not found": "server.jobOrCvNotFound",
  "position_title is required": "server.positionTitleRequired",
  "question text is required": "server.questionTextRequired",
  "meeting_date must be a valid YYYY-MM-DD date": "server.invalidDate",
  "request contains an invalid field value": "server.invalidField",
  "file is required": "server.fileRequired",
  "Failed to parse multipart form or file too large": "server.uploadUnreadable",
  "Failed to save attachment": "server.attachmentSaveFailed",
  "Choose an image to upload": "server.photoRequired",
  "Choose a PNG or JPEG image": "server.photoType",
  "Choose an image no larger than 5 MiB": "server.photoTooLarge",
  "Image cannot be empty": "server.photoEmpty",
  "Failed to save profile picture": "server.photoSaveFailed",
  "Failed to remove profile picture": "server.photoRemoveFailed",
  "No custom profile picture set": "server.photoNotCustom",
  "Choose a CV file to upload": "server.cvRequired",
  "Choose a CV file no larger than 50 MiB": "server.cvTooLarge",
  "CV file cannot be empty": "server.cvEmpty",
  "CV version is assigned to a job": "server.cvAssigned",
  "Failed to save CV version": "server.cvSaveFailed",
  "Failed to delete CV version": "server.cvDeleteFailed",
  "Failed to load CV versions": "server.cvLoadFailed",
  "Failed to assign CV version": "server.cvAssignFailed",
  "Technology is assigned to one or more processes; remove its assignments first": "server.technologyInUse",
  "Another backup transfer is in progress; try again shortly": "server.backupBusy",
  "Backup exceeds the 500 MiB limit": "server.backupTooLarge",
  "Backup archive exceeds the 500 MiB limit": "server.backupTooLarge",
  "Upload an encrypted War Room backup no larger than 500 MiB": "server.backupUploadLimit",
  "Choose an encrypted War Room backup to import": "server.backupRequired",
  "Choose a readable War Room backup to import": "server.backupUnreadable",
  "Only encrypted War Room backups can be imported": "server.backupEncryptedOnly",
  "Encrypted backup could not be unlocked with this installation's key": "server.backupUnlockFailed",
  "Encrypted backups are not configured on this server": "server.backupNotConfigured",
  "Encrypted backup restore is not configured on this server": "server.backupNotConfigured",
  "Encrypted backup is unavailable": "server.backupUnavailable",
  "Encrypted backup restore is unavailable": "server.backupUnavailable",
  "Failed to create backup archive": "server.backupCreateFailed",
  "Failed to prepare backup import": "server.backupPrepareFailed",
  "This backup could not be imported": "server.backupImportFailed",
  "This backup contains no saved processes.": "server.backupEmpty",
  "Google Drive request failed. Try again later.": "server.driveRequestFailed",
  "Google Drive backup is unavailable": "server.driveUnavailable",
  "Could not revoke Google Drive access": "server.driveRevokeFailed",
  "google sign-in is not configured": "server.googleNotConfigured",
  "no google drive account connected": "server.driveNotConnected",
  "google access may still be authorized; revoke War Room access in your Google Account settings": "server.googleRevocationFailed",
  "Job import is disabled.": "server.importDisabled",
  "Enter a public https link to the job posting.": "server.importInvalidUrl",
  "No job details were found on that page.": "server.importNoDetails",
  "This site does not allow automatic imports.": "server.importNotAllowed",
  "That site does not allow automatic imports.": "server.importNotAllowed",
  "Too many imports. Try again in a few minutes.": "server.importRateLimited",
  "The import was cancelled.": "server.importCancelled",
  "Imports are busy right now. Try again shortly.": "server.importBusy",
  "That page is too large to import.": "server.importTooLarge",
  "That link does not point to a job page.": "server.importNotJobPage",
  "That posting is no longer available.": "server.importGone",
  "That link redirects too many times.": "server.importRedirects",
  "That site took too long to respond.": "server.importTimeout",
  "That site could not be reached.": "server.importUnreachable",
  "That job board returned an unexpected response.": "server.importUnexpected",
  "This posting is no longer accepting applications.": "server.importClosed",
  "This posting's closing date has passed.": "server.importExpired",
  "The posted salary is not annual, so it was left out.": "server.importSalaryNotAnnual",
  "The posted salary uses a currency War Room does not support, so it was left out.": "server.importSalaryCurrency",
}));

// Invalid-field errors arrive as "request contains an invalid field value: <detail>".
const INVALID_FIELD_PREFIX = "request contains an invalid field value: ";
const INVALID_FIELD_KEYS = new Map(Object.entries({
  "period name must contain 1 to 100 characters": "server.periodNameLength",
  "end date must be on or after start date": "server.periodEndBeforeStart",
  "dates must be valid YYYY-MM-DD calendar dates": "server.invalidDate",
  "search period dates overlap an existing period": "server.periodOverlap",
  "search period does not exist": "server.searchPeriodNotFound",
  "technology name is required and limited to 80 characters; at most 20 aliases are allowed": "server.technologyNameLimits",
  "technology aliases must be unique and different from the canonical name": "server.technologyAliasesUnique",
  "duplicate technology alias": "server.technologyAliasesUnique",
  "technology name or alias already exists": "server.technologyExists",
  "invalid or duplicate technology ID": "server.invalidField",
}));

const IMPORT_STATUS_PATTERN = /^That site answered with status (\d{3})\.$/;

/** Returns the translation key for an invalid-field message, or "" for any other message. */
function invalidFieldKey(message) {
  if (!message.startsWith(INVALID_FIELD_PREFIX)) return "";
  const detail = message.slice(INVALID_FIELD_PREFIX.length);
  if (/ must use YYYY-MM-DD$/.test(detail)) return "server.invalidDate";
  return INVALID_FIELD_KEYS.get(detail) || "server.invalidField";
}

/**
 * Translates a message from an API error body.
 * Unknown messages are shown as sent only in English; other languages get the fallback.
 *
 * @param {unknown} message - The `error` field of a response body, or an import warning.
 * @param {string} fallback - Translated text to show when the message cannot be translated.
 * @returns {string} Text in the selected language.
 */
export function serverMessage(message, fallback) {
  if (typeof message !== "string" || !message.trim()) return fallback;
  const key = MESSAGE_KEYS.get(message) || invalidFieldKey(message);
  if (key) return t(key);
  const status = message.match(IMPORT_STATUS_PATTERN);
  if (status) return t("server.importStatus", { status: status[1] });
  return getLanguage() === "en" ? message : fallback;
}
