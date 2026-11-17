package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// maxGoogleErrorMessage bounds Google's human-readable message in log records.
const maxGoogleErrorMessage = 300

// GoogleAPIError describes a failed call to a Google API. Its text names the
// operation, HTTP status, and Google's machine-readable reason (for example
// storageQuotaExceeded, insufficientPermissions, accessNotConfigured, or
// invalid_grant). It is meant for server logs; handlers send clients a
// generic message instead.
type GoogleAPIError struct {
	Operation string
	Status    int
	Reason    string
	Message   string
}

// Error implements error.
func (e *GoogleAPIError) Error() string {
	detail := fmt.Sprintf("%d", e.Status)
	if e.Reason != "" {
		detail += " " + e.Reason
	}
	text := fmt.Sprintf("google %s failed (%s)", e.Operation, detail)
	if e.Message != "" {
		text += ": " + e.Message
	}
	return text
}

// googleErrorBody covers both error shapes Google returns: the API form
// {"error":{"code","message","status","errors":[{"reason"}]}} and the OAuth
// form {"error":"invalid_grant","error_description":"..."}.
type googleErrorBody struct {
	Error            json.RawMessage `json:"error"`
	ErrorDescription string          `json:"error_description"`
}

type googleAPIErrorDetail struct {
	Message string `json:"message"`
	Status  string `json:"status"`
	Errors  []struct {
		Reason  string `json:"reason"`
		Message string `json:"message"`
	} `json:"errors"`
}

// newGoogleAPIError builds a GoogleAPIError from a non-success response body.
func newGoogleAPIError(operation string, status int, body []byte) *GoogleAPIError {
	apiErr := &GoogleAPIError{Operation: operation, Status: status}
	var parsed googleErrorBody
	if json.Unmarshal(body, &parsed) != nil || len(parsed.Error) == 0 {
		return apiErr
	}
	var oauthCode string
	if json.Unmarshal(parsed.Error, &oauthCode) == nil {
		apiErr.Reason, apiErr.Message = oauthCode, boundedGoogleMessage(parsed.ErrorDescription)
		return apiErr
	}
	var detail googleAPIErrorDetail
	if json.Unmarshal(parsed.Error, &detail) != nil {
		return apiErr
	}
	apiErr.Reason, apiErr.Message = detail.Status, boundedGoogleMessage(detail.Message)
	if len(detail.Errors) > 0 && detail.Errors[0].Reason != "" {
		apiErr.Reason = detail.Errors[0].Reason
	}
	return apiErr
}

// googleResponseError reads res's bounded body and returns its GoogleAPIError.
func googleResponseError(operation string, res *http.Response) *GoogleAPIError {
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	return newGoogleAPIError(operation, res.StatusCode, body)
}

func boundedGoogleMessage(message string) string {
	message = strings.Join(strings.Fields(message), " ")
	if len(message) > maxGoogleErrorMessage {
		return message[:maxGoogleErrorMessage]
	}
	return message
}

// doRequest sends req and strips the query string from transport errors: a
// resumable upload URL carries its session capability there, and folder
// searches carry the user's search text.
func doRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	res, err := client.Do(req)
	return res, transportError(err)
}

// IsGoogleAccessExpired reports whether err means Google no longer accepts
// the stored refresh token (revoked, expired, or issued to another client),
// so only signing in again can restore Drive backups.
func IsGoogleAccessExpired(err error) bool {
	var apiErr *GoogleAPIError
	return errors.As(err, &apiErr) && (apiErr.Reason == "invalid_grant" || apiErr.Reason == "unauthorized_client")
}

func transportError(err error) error {
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		return err
	}
	target := "<unparseable url>"
	if parsed, parseErr := url.Parse(urlErr.URL); parseErr == nil {
		parsed.RawQuery, parsed.Fragment, parsed.User = "", "", nil
		target = parsed.String()
	}
	return fmt.Errorf("google request %s %s: %w", urlErr.Op, target, urlErr.Err)
}
