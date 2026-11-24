package activity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/tenancy"
)

// Item is one activity row.
type Item struct {
	ID               int64
	TenantID         *int64
	ActorID          *int64
	ActorName        *string
	ImpersonatorID   *int64
	ImpersonatorName *string
	Action           string
	SubjectType      string
	SubjectID        *int64
	Changes          json.RawMessage
	IP               *string
	RequestID        *string
	CreatedAt        time.Time
}

// TenantSystem is the Filter.Tenant value selecting the system audit log.
const TenantSystem = "system"

// Filter narrows the feed. Cursor is the opaque value from a previous page.
// Tenant, ImpersonatorID and Impersonated apply to system readers only.
type Filter struct {
	SubjectType    string
	SubjectID      int64
	ActorID        int64
	Tenant         string // "" = every tenant and the system log; "system"; or a tenant id
	ImpersonatorID int64
	Impersonated   bool
	// Action is an exact action key (user.created).
	Action string
	// From and To bound created_at (inclusive); zero means open.
	From time.Time
	To   time.Time
	// Search matches the action, subject type, IP, actor name and changes.
	Search  string
	Cursor  string
	PerPage int
}

// Reader is who reads the feed. A system reader holds activity.view.all and
// is not being acted as: they read every tenant and the system audit log.
// Anyone else reads one tenant only and never sees who acted as a user.
type Reader struct {
	System bool
}

// Page is one page of the feed, newest first.
type Page struct {
	Items      []Item
	PerPage    int
	NextCursor *string
	// Total is how many rows match the filter (every page).
	Total int64
}

// Facets summarise the reader's whole feed: which actions, subjects and
// people appear and how often, and how much happened in the last 24 hours.
type Facets struct {
	Actions  []Count
	Subjects []Count
	Actors   []ActorCount
	LastDay  int64
}

// Count is a value and how many rows have it.
type Count struct {
	Value string
	Total int64
}

// ActorCount is a person and how many rows they acted in.
type ActorCount struct {
	ID    int64
	Name  string
	Total int64
}

// Service reads the activity feed.
type Service struct {
	repo Repository
}

// NewService creates the activity feed service.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// Feed lists the activity the reader may see.
//
//   - A system reader sees every tenant and the system log (rows with no
//     tenant), narrowed by Filter.Tenant. With X-Acting-Tenant they see that
//     tenant only.
//   - Anyone else sees the scope's effective tenant only. An internal account
//     without activity.view.all has no tenant to read and is refused.
func (s *Service) Feed(ctx context.Context, scope tenancy.Scope, reader Reader, filter Filter) (*Page, error) {
	perPage := filter.PerPage
	if perPage < 1 || perPage > 100 {
		perPage = 25
	}
	where, err := resolveScope(scope, reader, filter.Tenant)
	if err != nil {
		return nil, err
	}
	query := FeedQuery{
		scopePredicate: where,
		SubjectType:    filter.SubjectType,
		SubjectID:      filter.SubjectID,
		ActorID:        filter.ActorID,
		Action:         filter.Action,
		From:           filter.From,
		To:             filter.To,
		Search:         strings.TrimSpace(filter.Search),
		Limit:          perPage + 1,
	}
	if reader.System {
		query.ImpersonatorID = filter.ImpersonatorID
		query.ImpersonatedOnly = filter.Impersonated
	}
	total, err := s.repo.Count(ctx, query)
	if err != nil {
		return nil, err
	}
	if filter.Cursor != "" {
		if query.BeforeAt, query.BeforeID, err = decodeCursor(filter.Cursor); err != nil {
			return nil, err
		}
	}
	rows, err := s.repo.List(ctx, query)
	if err != nil {
		return nil, err
	}
	page := &Page{PerPage: perPage, Total: total, Items: make([]Item, 0, min(len(rows), perPage))}
	for i, row := range rows {
		if i == perPage {
			last := page.Items[len(page.Items)-1]
			next := encodeCursor(last.CreatedAt, last.ID)
			page.NextCursor = &next
			break
		}
		page.Items = append(page.Items, row)
	}
	return page, nil
}

// resolveScope picks the rows the reader may see:
//
//   - A system reader sees every tenant and the system log (rows with no
//     tenant), narrowed by tenant ("", "system" or an id). With
//     X-Acting-Tenant they see that tenant only.
//   - Anyone else sees the scope's effective tenant only. An internal account
//     without activity.view.all has no tenant to read and is refused.
func resolveScope(scope tenancy.Scope, reader Reader, tenant string) (scopePredicate, error) {
	tenantID := scope.EffectiveTenantID()
	switch {
	case !reader.System:
		if tenantID == 0 {
			return scopePredicate{}, apperr.New(apperr.InsufficientPermissions,
				"Reading every tenant's activity and the system audit log needs activity.view.all.")
		}
		return scopePredicate{TenantID: &tenantID}, nil
	case tenantID != 0:
		return scopePredicate{TenantID: &tenantID}, nil
	}
	switch tenant {
	case "":
		return scopePredicate{AllTenants: true}, nil
	case TenantSystem:
		return scopePredicate{SystemOnly: true}, nil
	}
	id, err := strconv.ParseInt(tenant, 10, 64)
	if err != nil || id < 1 {
		return scopePredicate{}, apperr.Validation(map[string][]string{"tenant_id": {`The tenant_id must be a tenant id or "system".`}})
	}
	return scopePredicate{TenantID: &id}, nil
}

// Facets summarises the feed the reader may see (tenant as in Feed).
func (s *Service) Facets(ctx context.Context, scope tenancy.Scope, reader Reader, tenant string) (*Facets, error) {
	where, err := resolveScope(scope, reader, tenant)
	if err != nil {
		return nil, err
	}
	return s.repo.Facets(ctx, where)
}

// Purge deletes activity older than the retention window.
func (s *Service) Purge(ctx context.Context, days int) (int64, error) {
	return s.repo.Purge(ctx, days)
}

func encodeCursor(at time.Time, id int64) string {
	raw := strconv.FormatInt(at.UnixNano(), 10) + ":" + strconv.FormatInt(id, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(raw string) (time.Time, int64, error) {
	invalid := apperr.Validation(map[string][]string{"cursor": {"The cursor is invalid."}})
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return time.Time{}, 0, invalid
	}
	nanos, id, ok := strings.Cut(string(decoded), ":")
	if !ok {
		return time.Time{}, 0, invalid
	}
	at, err1 := strconv.ParseInt(nanos, 10, 64)
	rowID, err2 := strconv.ParseInt(id, 10, 64)
	if err1 != nil || err2 != nil || rowID < 1 {
		return time.Time{}, 0, invalid
	}
	return time.Unix(0, at), rowID, nil
}
