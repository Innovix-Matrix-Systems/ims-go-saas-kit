package httpx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Responder writes success and error responses using the following conventions:
// {"data": ...} envelopes, pagination shapes, and RFC 9457
// problems for every error.
type Responder struct {
	builder ProblemBuilder
}

// NewResponder creates a responder for the given deployment.
func NewResponder(baseURL, env string, debug bool) *Responder {
	return &Responder{builder: NewProblemBuilder(baseURL, env, debug)}
}

// Data writes {"data": payload} with the given status.
func (rsp *Responder) Data(w http.ResponseWriter, r *http.Request, status int, payload any) {
	writeJSON(w, status, map[string]any{"data": payload})
}

// Message writes {"message": message} with the given status.
func (rsp *Responder) Message(w http.ResponseWriter, r *http.Request, status int, message string) {
	writeJSON(w, status, map[string]any{"message": message})
}

// NoContent writes an empty 204 response.
func (rsp *Responder) NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// Paginated writes {"data": [...], "links": {...}, "meta": {...}} using the
// pagination shape below.
func (rsp *Responder) Paginated(w http.ResponseWriter, r *http.Request, status int, data any, meta LengthAwareMeta, links PaginationLinks) {
	writeJSON(w, status, map[string]any{
		"data":  data,
		"links": links,
		"meta":  meta,
	})
}

// Cursor writes a cursor-paginated envelope: {data, links, meta}. The shapes
// are tuned for a notification feed (meta carries per_page/next_cursor/
// prev_cursor/unread_count).
func (rsp *Responder) Cursor(w http.ResponseWriter, r *http.Request, status int, data, meta, links any) {
	writeJSON(w, status, map[string]any{
		"data":  data,
		"links": links,
		"meta":  meta,
	})
}

// CursorURL returns the current request URL with the cursor query parameter
// set, or nil when the cursor is absent.
func CursorURL(r *http.Request, cursor *string) *string {
	if cursor == nil || *cursor == "" {
		return nil
	}
	query := r.URL.Query()
	query.Set("cursor", *cursor)
	scheme, host := linkOrigin(r)
	u := url.URL{
		Scheme:   scheme,
		Host:     host,
		Path:     r.URL.Path,
		RawQuery: query.Encode(),
	}
	value := u.String()
	return &value
}

// Error renders any error as an RFC 9457 problem response, including the
// Retry-After header for rate-limited responses.
func (rsp *Responder) Error(w http.ResponseWriter, r *http.Request, err error) {
	p := rsp.builder.FromError(r, err)
	if p.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(p.RetryAfter))
	}
	rsp.builder.WriteJSON(w, p)
}

// Panic renders a recovered panic as an internal-error problem.
func (rsp *Responder) Panic(w http.ResponseWriter, r *http.Request, panicValue any, stack []byte) {
	rsp.builder.WriteJSON(w, rsp.builder.FromPanic(r, panicValue, stack))
}

// PageLink is one entry of the pagination "links" array.
type PageLink struct {
	URL    *string `json:"url"`
	Label  string  `json:"label"`
	Active bool    `json:"active"`
}

// PaginationLinks is the envelope-level links object.
type PaginationLinks struct {
	First *string `json:"first"`
	Last  *string `json:"last"`
	Prev  *string `json:"prev"`
	Next  *string `json:"next"`
}

// LengthAwareMeta is the pagination meta object.
type LengthAwareMeta struct {
	CurrentPage int        `json:"current_page"`
	From        *int       `json:"from"`
	LastPage    int        `json:"last_page"`
	Links       []PageLink `json:"links"`
	Path        string     `json:"path"`
	PerPage     int        `json:"per_page"`
	To          *int       `json:"to"`
	Total       int64      `json:"total"`
}

// NewPagination builds pagination meta and links for the
// current request.
func NewPagination(r *http.Request, page, perPage int, total int64) (LengthAwareMeta, PaginationLinks) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 1
	}
	lastPage := int((total + int64(perPage) - 1) / int64(perPage))
	if lastPage < 1 {
		lastPage = 1
	}

	meta := LengthAwareMeta{
		CurrentPage: page,
		LastPage:    lastPage,
		Path:        baseURL(r),
		PerPage:     perPage,
		Total:       total,
	}
	if total > 0 {
		from := (page-1)*perPage + 1
		to := page * perPage
		if to > int(total) {
			to = int(total)
		}
		meta.From = &from
		meta.To = &to
	}

	first := pageURL(r, 1)
	last := pageURL(r, lastPage)
	links := PaginationLinks{First: &first, Last: &last}
	if page > 1 {
		prev := pageURL(r, page-1)
		links.Prev = &prev
	}
	if page < lastPage {
		next := pageURL(r, page+1)
		links.Next = &next
	}

	meta.Links = pageElements(r, page, lastPage, links)
	return meta, links
}

func pageElements(r *http.Request, page, lastPage int, links PaginationLinks) []PageLink {
	elements := make([]PageLink, 0, lastPage+2)
	elements = append(elements, PageLink{URL: links.Prev, Label: "&laquo; Previous"})

	appendPage := func(p int) {
		u := pageURL(r, p)
		elements = append(elements, PageLink{URL: &u, Label: strconv.Itoa(p), Active: p == page})
	}

	if lastPage <= 7 {
		for p := 1; p <= lastPage; p++ {
			appendPage(p)
		}
	} else {
		appendPage(1)
		start := page - 2
		if start < 2 {
			start = 2
		}
		end := page + 2
		if end > lastPage-1 {
			end = lastPage - 1
		}
		if start > 2 {
			elements = append(elements, PageLink{Label: "..."})
		}
		for p := start; p <= end; p++ {
			appendPage(p)
		}
		if end < lastPage-1 {
			elements = append(elements, PageLink{Label: "..."})
		}
		appendPage(lastPage)
	}

	elements = append(elements, PageLink{URL: links.Next, Label: "Next &raquo;"})
	return elements
}

func baseURL(r *http.Request) string {
	scheme, host := linkOrigin(r)
	if host == "" {
		return r.URL.Path
	}
	return scheme + "://" + host + r.URL.Path
}

func pageURL(r *http.Request, page int) string {
	query := r.URL.Query()
	query.Set("page", strconv.Itoa(page))
	scheme, host := linkOrigin(r)
	u := url.URL{
		Scheme:   scheme,
		Host:     host,
		Path:     r.URL.Path,
		RawQuery: query.Encode(),
	}
	return u.String()
}

type publicOriginKey struct{}

// PublicOrigin makes pagination/cursor links use the configured APP_URL
// origin instead of echoing the client-supplied Host header (M28). An empty
// or unparsable appURL keeps the request's own host.
func PublicOrigin(appURL string) func(http.Handler) http.Handler {
	parsed, err := url.Parse(strings.TrimSpace(appURL))
	valid := err == nil && parsed.Scheme != "" && parsed.Host != ""
	return func(next http.Handler) http.Handler {
		if !valid {
			return next
		}
		origin := [2]string{parsed.Scheme, parsed.Host}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), publicOriginKey{}, origin)))
		})
	}
}

func linkOrigin(r *http.Request) (scheme, host string) {
	if origin, ok := r.Context().Value(publicOriginKey{}).([2]string); ok {
		return origin[0], origin[1]
	}
	return requestScheme(r), r.Host
}

func requestScheme(r *http.Request) string {
	if trustedProxy(r.Context()) {
		if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
			if strings.EqualFold(proto, "https") {
				return "https"
			}
			return "http"
		}
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// AbsoluteURL builds an absolute URL for path on this API (APP_URL origin in
// deployed environments, the request host locally).
func AbsoluteURL(r *http.Request, path string) string {
	scheme, host := linkOrigin(r)
	if host == "" {
		return path
	}
	return scheme + "://" + host + path
}
