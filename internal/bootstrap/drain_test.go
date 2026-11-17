package bootstrap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDrainFlipsOnlyReadiness(t *testing.T) {
	d := &drainState{}
	h := d.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	probe := func(path string) int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec.Code
	}
	if probe("/readyz") != http.StatusOK {
		t.Fatal("ready before drain")
	}
	d.begin(context.Background(), 0)
	if probe("/readyz") != http.StatusServiceUnavailable {
		t.Fatal("readyz must be 503 while draining")
	}
	if probe("/healthz") != http.StatusOK || probe("/api/x") != http.StatusOK {
		t.Fatal("other paths must keep serving while draining")
	}
}
