package handlers

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
