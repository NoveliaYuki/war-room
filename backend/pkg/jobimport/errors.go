package jobimport

import "errors"

// Stable error codes shared with the frontend.
const (
	CodeInvalidURL    = "invalid_url"
	CodePasteRequired = "paste_required"
	CodeNotFound      = "not_found"
	CodeBlocked       = "blocked"
	CodeUnsupported   = "unsupported"
	CodeTimeout       = "timeout"
	CodeRateLimited   = "rate_limited"
	CodeBusy          = "busy"
)

// Error is an import failure whose message is safe to show to the user.
type Error struct {
	Code    string
	Message string
}

// Error returns the user-facing message.
func (e *Error) Error() string { return e.Message }

func newError(code, message string) *Error { return &Error{Code: code, Message: message} }

// ErrBlockedTarget marks a URL refused by the outbound safety checks.
var ErrBlockedTarget = newError(CodeInvalidURL, "Enter a public https link to the job posting.")

var errTooManyRedirects = errors.New("too many redirects")
