package system

import "github.com/go-chi/chi/v5"

// RegisterRoutes mounts the module routes on the API router.
func (m *Module) RegisterRoutes(r chi.Router) {
	r.Get("/system/info", m.info)
	r.Get("/languages", m.languages)
	r.Post("/system/echo", m.echo)
}
