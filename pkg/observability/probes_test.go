package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLivenessHandler(t *testing.T) {
	req := httptest.NewRequest("GET", "/healthz", nil)
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(livenessHandler)

	handler.ServeHTTP(rr, req)

	if contentType := rr.Header().Get("Content-Type"); contentType != "application/json" {
		t.Errorf("handler returned wrong status content type: got %v want %v",
			contentType, "application/json")
	}

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

	if body := rr.Body.String(); body != `{"health":"OK"}` {
		t.Errorf("handler returned wrong body: got %v want %v",
			body, http.StatusText(http.StatusOK))
	}
}
