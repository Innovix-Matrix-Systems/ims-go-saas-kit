package app

import (
	"net/http"
	"slices"
	"sync"
	"testing"
)

// TestLoadSmokeBoundedConcurrency drives the router with bounded concurrency to
// catch data races and gross regressions under load. It is deliberately small
// enough to run in CI on every push.
func TestLoadSmokeBoundedConcurrency(t *testing.T) {
	router := newTestRouter(t)

	const workers = 16
	const perWorker = 50

	var wg sync.WaitGroup
	errs := make(chan string, workers*perWorker)

	// /api/health is readiness: without a database (CI's build-test job) it
	// fails closed with 503, which is correct, so it may answer either.
	paths := []struct {
		method string
		path   string
		ok     []int
	}{
		{http.MethodGet, "/up", []int{http.StatusOK}},
		{http.MethodGet, "/api/health", []int{http.StatusOK, http.StatusServiceUnavailable}},
		{http.MethodGet, "/api/v1/system/info", []int{http.StatusOK}},
	}

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				req := paths[(w+i)%len(paths)]
				rec := doRequest(t, router, req.method, req.path, "")
				if !slices.Contains(req.ok, rec.Code) {
					errs <- req.method + " " + req.path + ": " + http.StatusText(rec.Code)
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("unexpected status under load: %s", err)
	}
}
