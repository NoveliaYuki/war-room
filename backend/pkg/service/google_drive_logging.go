package service

import (
	"errors"
	"log/slog"
	"time"
)

// publicBackupFailure is the only failure text stored in the user-visible
// backup status; the cause is in the server log.
const publicBackupFailure = "Google Drive backup failed. The server log has the details."

// googleReasonHints explains Google's machine-readable error reasons in the
// server log, so an operator can act without looking them up.
var googleReasonHints = map[string]string{
	"storageQuotaExceeded":            "the Google account's Drive storage is full",
	"insufficientPermissions":         "the grant lacks Drive access; sign in again and allow Google Drive",
	"insufficientFilePermissions":     "the account cannot write to the chosen backup folder",
	"ACCESS_TOKEN_SCOPE_INSUFFICIENT": "the grant lacks the drive.file scope; sign in again and allow Google Drive",
	"appNotAuthorizedToFile":          "the drive.file scope cannot reach this folder; choose it again with the folder picker",
	"accessNotConfigured":             "the Google Drive API is disabled for this OAuth client's Cloud project",
	"SERVICE_DISABLED":                "the Google Drive API is disabled for this OAuth client's Cloud project",
	"notFound":                        "the backup folder or file no longer exists in Drive",
	"invalid_grant":                   "the refresh token was revoked or expired (Testing-mode OAuth apps expire them after 7 days); sign in again",
	"invalid_client":                  "GOOGLE_CLIENT_ID or GOOGLE_CLIENT_SECRET does not match a valid OAuth client",
	"unauthorized_client":             "the stored refresh token was issued to a different OAuth client; sign in again",
	"userRateLimitExceeded":           "Google rate limit for this user; retry later",
	"rateLimitExceeded":               "Google rate limit; retry later",
	"dailyLimitExceeded":              "the Cloud project's daily Drive quota is exhausted",
	"domainPolicy":                    "the Google Workspace domain policy blocks this app",
	"UNAUTHENTICATED":                 "the access token was rejected; sign in again",
	"PERMISSION_DENIED":               "Google denied the request; check Drive access and the backup folder",
	"backendError":                    "temporary Google error; retry later",
	"access_denied":                   "the user declined the Google consent screen",
	"admin_policy_enforced":           "the Google Workspace administrator blocks this app",
	"redirect_uri_mismatch":           "GOOGLE_REDIRECT_URI is not registered on the OAuth client",
	"deleted_client":                  "the OAuth client was deleted in Google Cloud Console",
}

// googleErrorAttrs returns log attributes describing err, including Google's
// status, reason, and an operator hint when err wraps a GoogleAPIError.
func googleErrorAttrs(err error) []any {
	attrs := []any{"error", err.Error()}
	var apiErr *GoogleAPIError
	if !errors.As(err, &apiErr) {
		return attrs
	}
	attrs = append(attrs, "google_operation", apiErr.Operation, "google_status", apiErr.Status)
	if apiErr.Reason != "" {
		attrs = append(attrs, "google_reason", apiErr.Reason)
	}
	if hint, ok := googleReasonHints[apiErr.Reason]; ok {
		attrs = append(attrs, "hint", hint)
	}
	return attrs
}

// logBackupResult writes one record for a Drive backup attempt, whether it
// came from a request or from the background trigger.
func logBackupResult(userID string, size int64, duration time.Duration, err error) {
	durationMS := float64(duration.Microseconds()) / 1000
	switch {
	case err == nil:
		slog.Info("google drive backup completed", "user_id", userID, "bytes", size, "duration_ms", durationMS)
	case errors.Is(err, ErrGoogleNotConnected), errors.Is(err, ErrGoogleNotConfigured):
		slog.Debug("google drive backup skipped", "user_id", userID, "reason", err.Error())
	default:
		attrs := append([]any{"user_id", userID, "duration_ms", durationMS}, googleErrorAttrs(err)...)
		slog.Error("google drive backup failed", attrs...)
	}
}

// moveBackupToFolder moves the current backup into folderID and reports
// whether it succeeded. A failure is logged and the next backup is written
// to the new folder instead.
func (d *DriveBackupService) moveBackupToFolder(userID string, account *driveAccount, folderID string) bool {
	token, err := d.accessToken(userID, account)
	if err == nil {
		err = d.moveFile(account.FileID, folderID, token)
	}
	if err != nil {
		attrs := append([]any{"user_id", userID, "drive_folder_id", folderID}, googleErrorAttrs(err)...)
		slog.Warn("could not move existing drive backup to the new folder; the next backup will be created there", attrs...)
		return false
	}
	return true
}
