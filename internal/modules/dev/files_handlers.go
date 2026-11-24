package dev

import (
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authn"
	filesmod "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/files"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/httpx"
)

var fileTypes = map[string]bool{
	"image":    true,
	"video":    true,
	"document": true,
	"other":    true,
}

var fileSortColumns = map[string]bool{
	"created_at": true,
	"name":       true,
	"size":       true,
}

type FileResponse struct {
	FileItem
	URL         string `json:"url"`
	PreviewKind string `json:"preview_kind,omitempty"`
}

func (h *Handler) listFiles(w http.ResponseWriter, r *http.Request) {
	page, perPage, offset := httpx.PageParams(r)
	filter, err := fileFilterFromQuery(r, int32(perPage), int32(offset))
	if err != nil {
		h.Error(w, r, err)
		return
	}
	items, err := h.files.List(r.Context(), filter)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	total, err := h.files.Count(r.Context(), filter)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	data := make([]FileResponse, 0, len(items))
	for _, item := range items {
		data = append(data, FileResponse{
			FileItem:    item,
			URL:         h.files.URL(r.Context(), item),
			PreviewKind: previewKind(item.MimeType),
		})
	}
	meta, links := httpx.NewPagination(r, page, perPage, total)
	h.Paginated(w, r, http.StatusOK, data, meta, links)
}

func (h *Handler) fileStats(w http.ResponseWriter, r *http.Request) {
	tenantID, err := parseOptionalID(r.URL.Query().Get("tenant_id"), "tenant_id")
	if err != nil {
		h.Error(w, r, err)
		return
	}
	stats, err := h.files.Stats(r.Context(), tenantID)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	h.Data(w, r, http.StatusOK, stats)
}

func (h *Handler) downloadFile(w http.ResponseWriter, r *http.Request) {
	// Downloading tenant documents is an action, not a view (H14): the route
	// needs dev.manage.
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	reader, file, err := h.files.Open(r.Context(), id)
	if err != nil {
		h.Error(w, r, err)
		return
	}
	defer reader.Close()

	disposition := "attachment"
	if isInlinePreview(r, file.MimeType) {
		disposition = "inline"
	}
	contentType := strings.TrimSpace(file.MimeType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	if value := mime.FormatMediaType(disposition, map[string]string{"filename": file.FileName}); value != "" {
		w.Header().Set("Content-Disposition", value)
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	if disposition == "inline" {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	}
	if file.Size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(file.Size, 10))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, reader)
}

func (h *Handler) deleteFile(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id")
	if !ok {
		return
	}
	if err := h.files.Delete(r.Context(), authn.UserID(r), id); err != nil {
		h.Error(w, r, err)
		return
	}
	h.NoContent(w)
}

func fileFilterFromQuery(r *http.Request, limit, offset int32) (FileFilter, error) {
	query := r.URL.Query()
	filter := FileFilter{
		Search:   strings.TrimSpace(query.Get("search")),
		Type:     strings.ToLower(strings.TrimSpace(query.Get("type"))),
		Disk:     strings.TrimSpace(query.Get("disk")),
		Limit:    limit,
		Offset:   offset,
		OrderBy:  "created_at",
		OrderDir: "desc",
	}
	if filter.Type != "" && !fileTypes[filter.Type] {
		return FileFilter{}, apperr.Validation(map[string][]string{
			"type": {"The type must be one of: image, video, document, other."},
		})
	}
	if ownerType := strings.TrimSpace(query.Get("owner_type")); ownerType != "" {
		filter.OwnerType = ownerType
	}
	attached, err := parseOptionalBool(query.Get("attached"), "attached")
	if err != nil {
		return FileFilter{}, err
	}
	filter.Attached = attached
	for raw, target := range map[string]*int64{
		"owner_id":   &filter.OwnerID,
		"created_by": &filter.CreatedBy,
		"tenant_id":  &filter.TenantID,
	} {
		value, err := parseOptionalID(query.Get(raw), raw)
		if err != nil {
			return FileFilter{}, err
		}
		*target = value
	}
	from, err := parseOptionalTime(query.Get("created_from"), "created_from", false)
	if err != nil {
		return FileFilter{}, err
	}
	to, err := parseOptionalTime(query.Get("created_to"), "created_to", true)
	if err != nil {
		return FileFilter{}, err
	}
	filter.CreatedFrom = from
	filter.CreatedTo = to

	if raw := strings.TrimSpace(query.Get("order_by")); raw != "" {
		if !fileSortColumns[raw] {
			return FileFilter{}, apperr.Validation(map[string][]string{
				"order_by": {"The order_by must be one of: created_at, name, size."},
			})
		}
		filter.OrderBy = raw
	}
	if raw := strings.ToLower(strings.TrimSpace(query.Get("order_direction"))); raw != "" {
		if raw != "asc" && raw != "desc" {
			return FileFilter{}, apperr.Validation(map[string][]string{
				"order_direction": {"The order_direction must be asc or desc."},
			})
		}
		filter.OrderDir = raw
	}
	return filter, nil
}

func parseOptionalBool(raw, field string) (*bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "all") {
		return nil, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, apperr.Validation(map[string][]string{
			field: {"The value must be true or false."},
		})
	}
	return &value, nil
}

// parseOptionalTime reads an RFC 3339 timestamp, or a bare YYYY-MM-DD date
// taken as that whole day in UTC (endOfDay picks its last instant). Clients
// filtering by a person's calendar day send RFC 3339 bounds with their offset,
// as the dashboard does.
func parseOptionalTime(raw, field string, endOfDay bool) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return &parsed, nil
	}
	parsed, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return nil, apperr.Validation(map[string][]string{
			field: {"The date must be YYYY-MM-DD (a UTC day) or RFC 3339."},
		})
	}
	if endOfDay {
		parsed = parsed.Add(24*time.Hour - time.Nanosecond)
	}
	return &parsed, nil
}

func filesDomain(item FileItem) filesmod.File {
	return filesmod.File{Disk: item.Disk, Path: item.Path}
}

func previewKind(mimeType string) string {
	value := strings.ToLower(strings.TrimSpace(mimeType))
	if semi := strings.IndexByte(value, ';'); semi >= 0 {
		value = strings.TrimSpace(value[:semi])
	}
	switch {
	case value == "image/svg+xml":
		return ""
	case strings.HasPrefix(value, "image/"):
		return "image"
	case strings.HasPrefix(value, "video/"):
		return "video"
	case strings.HasPrefix(value, "audio/"):
		return "audio"
	case value == "application/pdf":
		return "pdf"
	default:
		return ""
	}
}

func isInlinePreview(r *http.Request, mimeType string) bool {
	raw := strings.TrimSpace(r.URL.Query().Get("inline"))
	if raw == "" {
		return false
	}
	inline, err := strconv.ParseBool(raw)
	if err != nil || !inline {
		return false
	}
	return previewKind(mimeType) != ""
}
