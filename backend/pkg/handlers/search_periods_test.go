package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchPeriodHandlersLifecycle(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	invalidJSON := httptest.NewRecorder()
	fixture.handler.handleCreateSearchPeriod(invalidJSON, withUserID(httptest.NewRequest(http.MethodPost, "/api/search-periods", strings.NewReader("{")), testUserID))
	requireSearchPeriodStatus(t, invalidJSON, http.StatusBadRequest)
	invalidInput := httptest.NewRecorder()
	fixture.handler.handleCreateSearchPeriod(invalidInput, withUserID(httptest.NewRequest(http.MethodPost, "/api/search-periods", strings.NewReader(`{"name":"","start_date":"2026-03-01","end_date":"2026-03-31"}`)), testUserID))
	requireSearchPeriodStatus(t, invalidInput, http.StatusBadRequest)
	create := httptest.NewRecorder()
	fixture.handler.handleCreateSearchPeriod(create, withUserID(httptest.NewRequest(http.MethodPost, "/api/search-periods", strings.NewReader(`{"name":"March 2026","start_date":"2026-03-01","end_date":"2026-03-31"}`)), testUserID))
	requireSearchPeriodStatus(t, create, http.StatusCreated)
	periods := httptest.NewRecorder()
	fixture.handler.handleListSearchPeriods(periods, withUserID(httptest.NewRequest(http.MethodGet, "/api/search-periods", nil), testUserID))
	if periods.Code != http.StatusOK || !strings.Contains(periods.Body.String(), "March 2026") {
		t.Fatalf("list status=%d body=%s", periods.Code, periods.Body.String())
	}
	overlap := httptest.NewRecorder()
	fixture.handler.handleCreateSearchPeriod(overlap, withUserID(httptest.NewRequest(http.MethodPost, "/api/search-periods", strings.NewReader(`{"name":"Overlap","start_date":"2026-03-31","end_date":"2026-04-30"}`)), testUserID))
	requireSearchPeriodStatus(t, overlap, http.StatusBadRequest)
	var created struct {
		ID string `json:"id"`
	}
	if err := decodeJSONBody(strings.NewReader(create.Body.String()), &created); err != nil || created.ID == "" {
		t.Fatalf("created response=%+v err=%v", created, err)
	}
	update := httptest.NewRecorder()
	updateRequest := withUserID(httptest.NewRequest(http.MethodPut, "/api/search-periods/"+created.ID, strings.NewReader(`{"name":"Updated","start_date":"2026-05-01","end_date":"2026-05-31"}`)), testUserID)
	updateRequest.SetPathValue("id", created.ID)
	fixture.handler.handleUpdateSearchPeriod(update, updateRequest)
	requireSearchPeriodStatus(t, update, http.StatusOK)
	missingUpdate := httptest.NewRecorder()
	missingUpdateRequest := withUserID(httptest.NewRequest(http.MethodPut, "/api/search-periods/missing", strings.NewReader(`{"name":"Missing","start_date":"2026-07-01","end_date":"2026-07-31"}`)), testUserID)
	missingUpdateRequest.SetPathValue("id", "missing")
	fixture.handler.handleUpdateSearchPeriod(missingUpdate, missingUpdateRequest)
	requireSearchPeriodStatus(t, missingUpdate, http.StatusNotFound)
	badUpdate := httptest.NewRecorder()
	badUpdateRequest := withUserID(httptest.NewRequest(http.MethodPut, "/api/search-periods/"+created.ID, strings.NewReader(`{"name":"","start_date":"2026-05-01","end_date":"2026-05-31"}`)), testUserID)
	badUpdateRequest.SetPathValue("id", created.ID)
	fixture.handler.handleUpdateSearchPeriod(badUpdate, badUpdateRequest)
	requireSearchPeriodStatus(t, badUpdate, http.StatusBadRequest)
	invalidUpdateJSON := httptest.NewRecorder()
	fixture.handler.handleUpdateSearchPeriod(invalidUpdateJSON, withUserID(httptest.NewRequest(http.MethodPut, "/api/search-periods/"+created.ID, strings.NewReader("{")), testUserID))
	requireSearchPeriodStatus(t, invalidUpdateJSON, http.StatusBadRequest)
	remove := httptest.NewRecorder()
	removeRequest := withUserID(httptest.NewRequest(http.MethodDelete, "/api/search-periods/"+created.ID, nil), testUserID)
	removeRequest.SetPathValue("id", created.ID)
	fixture.handler.handleDeleteSearchPeriod(remove, removeRequest)
	requireSearchPeriodStatus(t, remove, http.StatusOK)
	missingDelete := httptest.NewRecorder()
	missingDeleteRequest := withUserID(httptest.NewRequest(http.MethodDelete, "/api/search-periods/missing", nil), testUserID)
	missingDeleteRequest.SetPathValue("id", "missing")
	fixture.handler.handleDeleteSearchPeriod(missingDelete, missingDeleteRequest)
	requireSearchPeriodStatus(t, missingDelete, http.StatusNotFound)
	if err := fixture.db.Close(); err != nil {
		t.Fatal(err)
	}
	failedList := httptest.NewRecorder()
	fixture.handler.handleListSearchPeriods(failedList, withUserID(httptest.NewRequest(http.MethodGet, "/api/search-periods", nil), testUserID))
	requireSearchPeriodStatus(t, failedList, http.StatusInternalServerError)
}

func requireSearchPeriodStatus(t *testing.T, response *httptest.ResponseRecorder, expected int) {
	t.Helper()
	if response.Code != expected {
		t.Fatalf("status=%d body=%s, want %d", response.Code, response.Body.String(), expected)
	}
}

func TestSearchPeriodHandlersDatabaseErrors(t *testing.T) {
	fixture := newCVHandlerFixture(t)
	if err := fixture.db.Close(); err != nil {
		t.Fatal(err)
	}
	payload := `{"name":"March","start_date":"2026-03-01","end_date":"2026-03-31"}`
	create := httptest.NewRecorder()
	fixture.handler.handleCreateSearchPeriod(create, withUserID(httptest.NewRequest(http.MethodPost, "/api/search-periods", strings.NewReader(payload)), testUserID))
	requireSearchPeriodStatus(t, create, http.StatusConflict)
	update := httptest.NewRecorder()
	updateRequest := withUserID(httptest.NewRequest(http.MethodPut, "/api/search-periods/period", strings.NewReader(payload)), testUserID)
	updateRequest.SetPathValue("id", "period")
	fixture.handler.handleUpdateSearchPeriod(update, updateRequest)
	requireSearchPeriodStatus(t, update, http.StatusConflict)
	remove := httptest.NewRecorder()
	removeRequest := withUserID(httptest.NewRequest(http.MethodDelete, "/api/search-periods/period", nil), testUserID)
	removeRequest.SetPathValue("id", "period")
	fixture.handler.handleDeleteSearchPeriod(remove, removeRequest)
	requireSearchPeriodStatus(t, remove, http.StatusInternalServerError)
}
