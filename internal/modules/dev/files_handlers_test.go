package dev

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestPreviewKind(t *testing.T) {
	tests := map[string]string{
		"image/png":                 "image",
		"image/jpeg; charset=utf-8": "image",
		"image/svg+xml":             "",
		"video/mp4":                 "video",
		"audio/mpeg":                "audio",
		"application/pdf":           "pdf",
		"text/plain":                "",
		"application/octet-stream":  "",
	}
	for mimeType, want := range tests {
		if got := previewKind(mimeType); got != want {
			t.Errorf("previewKind(%q) = %q, want %q", mimeType, got, want)
		}
	}
}

func TestIsInlinePreview(t *testing.T) {
	tests := []struct {
		query string
		mime  string
		want  bool
	}{
		{"inline=1", "image/png", true},
		{"inline=true", "application/pdf", true},
		{"inline=1", "image/svg+xml", false},
		{"inline=1", "application/octet-stream", false},
		{"", "image/png", false},
		{"inline=0", "image/png", false},
		{"inline=nope", "image/png", false},
	}
	for _, test := range tests {
		request := httptest.NewRequest("GET", "/dev/api/files/1/download?"+test.query, nil)
		if got := isInlinePreview(request, test.mime); got != test.want {
			t.Errorf("isInlinePreview(%q, %q) = %v, want %v", test.query, test.mime, got, test.want)
		}
	}
}

func TestFileFilterFromQuery(t *testing.T) {
	request := httptest.NewRequest("GET",
		"/dev/api/files?search=report&type=image&disk=s3&attached=true&owner_id=4&created_by=7&tenant_id=2&created_from=2026-01-02&created_to=2026-01-03&order_by=size&order_direction=asc",
		nil)
	filter, err := fileFilterFromQuery(request, 25, 50)
	if err != nil {
		t.Fatalf("fileFilterFromQuery() error = %v", err)
	}
	if filter.Search != "report" || filter.Type != "image" || filter.Disk != "s3" {
		t.Fatalf("filter text = %+v", filter)
	}
	if filter.Attached == nil || !*filter.Attached {
		t.Fatalf("filter attached = %v, want true", filter.Attached)
	}
	if filter.OwnerID != 4 || filter.CreatedBy != 7 || filter.TenantID != 2 {
		t.Fatalf("filter ids = %+v", filter)
	}
	if filter.CreatedFrom == nil || !filter.CreatedFrom.Equal(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("filter from = %v", filter.CreatedFrom)
	}
	if filter.CreatedTo == nil || filter.CreatedTo.Sub(time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)) != 24*time.Hour-time.Nanosecond {
		t.Fatalf("filter to = %v", filter.CreatedTo)
	}
	if filter.OrderBy != "size" || filter.OrderDir != "asc" || filter.Limit != 25 || filter.Offset != 50 {
		t.Fatalf("filter order/paging = %+v", filter)
	}

	for _, query := range []string{
		"type=archive",
		"attached=maybe",
		"tenant_id=abc",
		"created_from=02-01-2026",
		"order_by=disk",
		"order_direction=sideways",
	} {
		request := httptest.NewRequest("GET", "/dev/api/files?"+query, nil)
		if _, err := fileFilterFromQuery(request, 25, 0); err == nil {
			t.Errorf("fileFilterFromQuery(%q) = nil error", query)
		}
	}
}
