package oauth

import "time"

// ConnectionResponse is one connection as the user sees it.
type ConnectionResponse struct {
	ID         int64      `json:"id"`
	ClientID   string     `json:"client_id"`
	ClientName string     `json:"client_name"`
	Scopes     []string   `json:"scopes"`
	TenantID   *int64     `json:"tenant_id"`
	BranchID   *int64     `json:"branch_id"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

// NewConnectionResponse maps a grant.
func NewConnectionResponse(g Grant) ConnectionResponse {
	return ConnectionResponse{
		ID: g.ID, ClientID: g.ClientID, ClientName: g.ClientName, Scopes: g.Scopes,
		TenantID: optional(g.TenantID), BranchID: optional(g.BranchID),
		CreatedAt: g.CreatedAt, LastUsedAt: g.LastUsedAt,
	}
}

// ClientResponse is a registered client; ClientSecret is set only in the
// response that created or rotated it.
type ClientResponse struct {
	ID           int64     `json:"id"`
	ClientID     string    `json:"client_id"`
	ClientSecret string    `json:"client_secret,omitempty"`
	Name         string    `json:"name"`
	Type         string    `json:"type"`
	RedirectURIs []string  `json:"redirect_uris"`
	Scopes       []string  `json:"scopes"`
	TenantID     *int64    `json:"tenant_id"`
	IsActive     bool      `json:"is_active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// NewClientResponse maps a registered client.
func NewClientResponse(c ClientRecord, secret string) ClientResponse {
	return ClientResponse{
		ID: c.ID, ClientID: c.ClientID, ClientSecret: secret, Name: c.Name, Type: c.Type,
		RedirectURIs: c.RedirectURIs, Scopes: c.Scopes, TenantID: optional(c.TenantID), IsActive: c.IsActive,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

// ClientRequest creates or updates a registered client.
type ClientRequest struct {
	Name         string   `json:"name" validate:"required,max=255"`
	Type         string   `json:"type" validate:"omitempty,oneof=public confidential"`
	RedirectURIs []string `json:"redirect_uris" validate:"required,min=1,max=20,dive,required,max=2048"`
	Scopes       []string `json:"scopes" validate:"omitempty,max=2,dive,oneof=mcp:read mcp:write"`
	TenantID     *int64   `json:"tenant_id" validate:"omitempty,min=1"`
	IsActive     *bool    `json:"is_active"`
}

func (req ClientRequest) toInput() ClientInput {
	in := ClientInput{Name: req.Name, Type: req.Type, RedirectURIs: req.RedirectURIs, Scopes: req.Scopes, IsActive: true}
	if in.Type == "" {
		in.Type = TypePublic
	}
	if req.TenantID != nil {
		in.TenantID = *req.TenantID
	}
	if req.IsActive != nil {
		in.IsActive = *req.IsActive
	}
	return in
}
