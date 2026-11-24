package realtime

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/httpx"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/pubsub"
)

// retryMillis is the reconnect delay the stream suggests to clients.
const retryMillis = 3000

// Handler serves GET /api/v1/realtime/stream.
type Handler struct {
	httpx.Kit
	hub    *pubsub.Hub
	cfg    config.Realtime
	logger *slog.Logger
}

// NewHandler creates the stream handler; a nil hub (or REALTIME_ENABLED off)
// answers 404.
func NewHandler(hub *pubsub.Hub, cfg config.Realtime, logger *slog.Logger, kit httpx.Kit) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{Kit: kit, hub: hub, cfg: cfg, logger: logger}
}

// stream holds a text/event-stream open until the client leaves, the server
// shuts down or REALTIME_MAX_STREAM passes. It sends:
//
//	event: ready          once, with the heartbeat interval
//	event: <kind>         when something of that kind changed for the caller
//	                      ("notifications", "announcements", "resync")
//	: ping                every REALTIME_HEARTBEAT while idle
//	event: reconnect      before the server closes the stream
func (h *Handler) stream(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.Enabled || h.hub == nil {
		h.Error(w, r, apperr.New(apperr.ResourceNotFound, "Live updates are not enabled."))
		return
	}
	identity, ok := authn.IdentityFromContext(r.Context())
	if !ok {
		h.Error(w, r, apperr.New(apperr.TokenMissing, "Authentication is required."))
		return
	}
	sub, err := h.hub.Subscribe(identity.UserID, identity.TenantID)
	switch {
	case errors.Is(err, pubsub.ErrTooManyStreams):
		h.Error(w, r, apperr.New(apperr.RateLimitExceeded, "Too many live-update streams are open for this account."))
		return
	case err != nil:
		h.Error(w, r, apperr.New(apperr.ServiceUnavailable, "Live updates are unavailable; try again shortly."))
		return
	}
	defer h.hub.Unsubscribe(sub)

	// The server's write timeout is for ordinary responses: lift it for this
	// one (every wrapper on the way unwraps to the real writer).
	controller := http.NewResponseController(w)
	if err := controller.SetWriteDeadline(time.Time{}); err != nil {
		h.logger.WarnContext(r.Context(), "realtime stream cannot lift the write deadline; it will end at HTTP_WRITE_TIMEOUT", "error", err)
	}

	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("X-Accel-Buffering", "no") // nginx: do not buffer the stream
	w.WriteHeader(http.StatusOK)

	send := func(format string, args ...any) bool {
		if _, err := fmt.Fprintf(w, format, args...); err != nil {
			return false
		}
		return controller.Flush() == nil
	}
	if !send("retry: %d\nevent: ready\ndata: {\"heartbeat\":%d}\n\n", retryMillis, int(h.cfg.Heartbeat.Seconds())) {
		return
	}

	heartbeat := time.NewTicker(h.cfg.Heartbeat)
	defer heartbeat.Stop()
	lifetime := time.NewTimer(h.cfg.MaxStream)
	defer lifetime.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case kind, open := <-sub.Events():
			if !open { // the server is shutting down
				send("event: reconnect\ndata: {}\n\n")
				return
			}
			if !validKind(kind) {
				continue
			}
			if !send("event: %s\ndata: {}\n\n", kind) {
				return
			}
		case <-heartbeat.C:
			if !send(": ping\n\n") {
				return
			}
		case <-lifetime.C:
			send("event: reconnect\ndata: {}\n\n")
			return
		}
	}
}

// validKind keeps the stream well formed whatever a NOTIFY payload carried:
// kinds are short lowercase names.
func validKind(kind string) bool {
	if kind == "" || len(kind) > 64 {
		return false
	}
	for _, c := range kind {
		if (c < 'a' || c > 'z') && c != '_' && c != '.' && c != '-' {
			return false
		}
	}
	return true
}
