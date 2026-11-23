package dev

import (
	"net/http"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/httpx"
)

// Gate makes the developer surface indistinguishable from an unregistered
// route while DEV_DASHBOARD_ENABLED is false. It is mounted before
// authentication so disabled deployments answer 404 (not 401/403) to every
// /dev request.
func Gate(enabled bool, responder *httpx.Responder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !enabled {
				responder.Error(w, r, apperr.New(apperr.ResourceNotFound, "The requested resource was not found."))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
