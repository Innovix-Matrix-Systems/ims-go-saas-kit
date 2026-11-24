package dev

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/delivery"
)

// Deliveries panel: the delivery log (what was sent, on which
// channel, with what outcome — never a body) and template previews.

// deliveryTemplate is one registered template and the channels it renders.
type deliveryTemplate struct {
	Name     string             `json:"name"`
	Channels []delivery.Channel `json:"channels"`
}

// deliveryTemplates is the template list plus the channels that can send.
type deliveryTemplates struct {
	Templates []deliveryTemplate `json:"templates"`
	Channels  []deliveryChannel  `json:"channels"`
}

// deliveryChannel is a configured channel and the driver behind it.
type deliveryChannel struct {
	Channel delivery.Channel `json:"channel"`
	Driver  string           `json:"driver"`
}

func (h *Handler) listDeliveries(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit, err := parseLimit(query.Get("limit"))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	before, err := parseCursor(query.Get("cursor"))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	userID, err := parseOptionalID(query.Get("user_id"), "user_id")
	if err != nil {
		h.Error(w, r, err)
		return
	}
	records, err := h.deliveries.List(r.Context(), delivery.ListFilter{
		Channel:  strings.TrimSpace(query.Get("channel")),
		Status:   strings.TrimSpace(query.Get("status")),
		Template: strings.TrimSpace(query.Get("template")),
		UserID:   userID,
		BeforeID: before,
		Limit:    int32(limit),
	})
	if err != nil {
		h.Error(w, r, err)
		return
	}
	nextCursor := ""
	if len(records) == limit {
		nextCursor = strconv.FormatInt(records[len(records)-1].ID, 10)
	}
	h.Cursor(w, r, http.StatusOK, records, map[string]any{"next_cursor": nextCursor}, nil)
}

func (h *Handler) getDelivery(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	record, err := h.deliveries.Get(r.Context(), id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, record)
}

func (h *Handler) listDeliveryTemplates(w http.ResponseWriter, r *http.Request) {
	registry := h.deliveries.Templates()
	result := deliveryTemplates{}
	for _, name := range registry.Names() {
		result.Templates = append(result.Templates, deliveryTemplate{Name: name, Channels: registry.Channels(name)})
	}
	for _, channel := range h.deliveries.Channels() {
		result.Channels = append(result.Channels, deliveryChannel{Channel: channel, Driver: h.deliveries.Driver(channel)})
	}
	h.Data(w, r, http.StatusOK, result)
}

func (h *Handler) previewDeliveryTemplate(w http.ResponseWriter, r *http.Request) {
	channel := delivery.Channel(r.URL.Query().Get("channel"))
	if channel == "" {
		channel = delivery.Mail
	}
	if channel != delivery.Mail && channel != delivery.SMS && channel != delivery.Push {
		h.Error(w, r, apperr.Validation(map[string][]string{"channel": {"The channel must be mail, sms or push."}}))
		return
	}
	content, err := h.deliveries.Preview(r.Context(), chi.URLParam(r, "name"), channel, r.URL.Query().Get("locale"), nil)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, content)
}
