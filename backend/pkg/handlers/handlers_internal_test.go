package handlers

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"war-room/backend/pkg/service"
)

func TestUpdateHandlersReturnBadRequestForInvalidEnums(t *testing.T) {
	handler := &Handler{jobs: service.NewJobService(nil, nil)}
	tests := []struct {
		name, method, path, body string
		handle                   func(http.ResponseWriter, *http.Request)
	}{
		{"job", http.MethodPut, "/api/jobs/job", `{"status":"invalid"}`, handler.handleUpdateJob},
		{"stage", http.MethodPut, "/api/stages/stage", `{"status":"invalid"}`, handler.handleUpdateStage},
		{"meeting", http.MethodPut, "/api/stages/stage/schedule", `{"meeting_date":"2030-01-01","meeting_type":"invalid"}`, handler.handleScheduleStage},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			request.SetPathValue("id", "fixture")
			recorder := httptest.NewRecorder()
			test.handle(recorder, request)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status=%d, want %d: %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
		})
	}
}

func TestParseAttachmentUploadValidation(t *testing.T) {
	tests := []struct {
		name       string
		jobID      string
		stageID    string
		withFile   bool
		content    string
		wantStatus int
	}{
		{name: "invalid multipart", content: "not multipart", wantStatus: http.StatusBadRequest},
		{name: "missing job", withFile: true, wantStatus: http.StatusBadRequest},
		{name: "missing file", jobID: "job", wantStatus: http.StatusBadRequest},
		{name: "stage null", jobID: "job", stageID: "null", withFile: true, wantStatus: http.StatusCreated},
		{name: "stage id", jobID: "job", stageID: "stage", withFile: true, wantStatus: http.StatusCreated},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, cleanup := uploadRequest(t, test.jobID, test.stageID, test.withFile, test.content)
			defer cleanup()
			assertParsedUpload(t, request, test)
		})
	}
}

func assertParsedUpload(t *testing.T, request *http.Request, test struct {
	name       string
	jobID      string
	stageID    string
	withFile   bool
	content    string
	wantStatus int
}) {
	t.Helper()
	recorder := httptest.NewRecorder()
	_, stageID, file, _, removeForm, ok := parseAttachmentUpload(recorder, request)
	defer func() {
		if file != nil {
			_ = file.Close()
		}
		if removeForm != nil {
			removeForm()
		}
	}()
	if test.wantStatus != http.StatusCreated {
		if ok || recorder.Code != test.wantStatus {
			t.Fatalf("invalid upload ok=%t status=%d", ok, recorder.Code)
		}
		return
	}
	if validStageUpload(ok, stageID, test.stageID) {
		return
	}
	t.Fatalf("valid upload parsed ok=%t stageID=%v", ok, stageID)
}

func validStageUpload(ok bool, stageID *string, expected string) bool {
	if !ok {
		return false
	}
	if expected == "stage" {
		return stageID != nil && *stageID == expected
	}
	return expected != "null" || stageID == nil
}

func TestParseAttachmentUploadRejectsRequestOverLimit(t *testing.T) {
	const boundary = "upload-limit"
	body := "--" + boundary + "\r\nContent-Disposition: form-data; name=\"file\"; filename=\"large.bin\"\r\n\r\n" +
		strings.Repeat("a", (50<<20)+1) + "\r\n--" + boundary + "--\r\n"
	request := httptest.NewRequest(http.MethodPost, "/api/attachments", strings.NewReader(body))
	request.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	recorder := httptest.NewRecorder()
	_, _, _, _, _, ok := parseAttachmentUpload(recorder, request)
	if ok || recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized upload ok=%t status=%d", ok, recorder.Code)
	}
}

func TestStoreUploadedAttachmentReturnsReadErrors(t *testing.T) {
	readErr := errors.New("upload stream failed")
	file := failingMultipartFile{err: readErr}
	_, err := (&Handler{}).storeUploadedAttachment("job", nil, file, &multipart.FileHeader{Filename: "resume.txt"})
	if !errors.Is(err, readErr) {
		t.Fatalf("upload read error=%v", err)
	}
}

func TestBoundedWriterStopsAtConfiguredLimit(t *testing.T) {
	var output bytes.Buffer
	writer := &boundedWriter{writer: &output, limit: 5}
	if n, err := writer.Write([]byte("1234")); n != 4 || err != nil {
		t.Fatalf("write before limit=(%d, %v), want (4, nil)", n, err)
	}
	if n, err := writer.Write([]byte("5678")); n != 1 || !errors.Is(err, errBackupExportTooLarge) {
		t.Fatalf("write beyond limit=(%d, %v), want (1, limit error)", n, err)
	}
	if output.String() != "12345" {
		t.Fatalf("bounded output=%q, want exactly five bytes", output.String())
	}
	if n, err := writer.Write([]byte("9")); n != 0 || !errors.Is(err, errBackupExportTooLarge) {
		t.Fatalf("write after limit=(%d, %v), want (0, limit error)", n, err)
	}
}

func TestBoundedWriterLimitErrorSurvivesZIPFinalization(t *testing.T) {
	writer := &boundedWriter{writer: io.Discard, limit: 1}
	archive := zip.NewWriter(writer)
	entry, err := archive.Create("payload.txt")
	if err != nil {
		t.Fatalf("create ZIP entry: %v", err)
	}
	if _, err := entry.Write([]byte("payload")); err != nil {
		t.Fatalf("buffer ZIP entry: %v", err)
	}
	if err := archive.Close(); !errors.Is(err, errBackupExportTooLarge) {
		t.Fatalf("ZIP close error=%v, want bounded-writer error", err)
	}
}

func TestBoundedWriterPropagatesUnderlyingWriteFailures(t *testing.T) {
	writeErr := errors.New("disk full")
	tests := []struct {
		name   string
		writer io.Writer
		data   string
		limit  int64
		want   error
	}{
		{name: "underlying error before limit", writer: alwaysFailWriter{err: writeErr}, data: "x", limit: 5, want: writeErr},
		{name: "underlying error while enforcing limit", writer: alwaysFailWriter{err: writeErr}, data: "123456", limit: 5, want: writeErr},
		{name: "short write before limit", writer: shortWriter{}, data: "xy", limit: 5, want: io.ErrShortWrite},
		{name: "short write while enforcing limit", writer: shortWriter{}, data: "123456", limit: 5, want: io.ErrShortWrite},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			writer := &boundedWriter{writer: test.writer, limit: test.limit}
			if _, err := writer.Write([]byte(test.data)); !errors.Is(err, test.want) {
				t.Fatalf("write error=%v, want %v", err, test.want)
			}
		})
	}
}

type alwaysFailWriter struct{ err error }

func (writer alwaysFailWriter) Write([]byte) (int, error) { return 0, writer.err }

type shortWriter struct{}

func (shortWriter) Write(data []byte) (int, error) { return len(data) - 1, nil }

func uploadRequest(t *testing.T, jobID, stageID string, withFile bool, invalid string) (*http.Request, func()) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if invalid != "" {
		_, _ = io.WriteString(&body, invalid)
	} else {
		if jobID != "" {
			_ = writer.WriteField("job_id", jobID)
		}
		if stageID != "" {
			_ = writer.WriteField("stage_id", stageID)
		}
		if withFile {
			part, err := writer.CreateFormFile("file", "resume.txt")
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.WriteString(part, "resume")
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/api/attachments", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request, func() {
		if request.MultipartForm != nil {
			_ = request.MultipartForm.RemoveAll()
		}
	}
}

type failingMultipartFile struct{ err error }

func (file failingMultipartFile) Read([]byte) (int, error)     { return 0, file.err }
func (failingMultipartFile) ReadAt([]byte, int64) (int, error) { return 0, io.EOF }
func (failingMultipartFile) Seek(int64, int) (int64, error)    { return 0, nil }
func (failingMultipartFile) Close() error                      { return nil }
