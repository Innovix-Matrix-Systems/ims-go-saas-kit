package oauth

import (
	"context"
	"slices"
	"strings"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/mcp"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/activity"
)

// Client types a registered client can have.
const (
	TypePublic       = "public"
	TypeConfidential = "confidential"
)

// ClientInput is a registered client's editable fields.
type ClientInput struct {
	Name         string
	Type         string // create only
	RedirectURIs []string
	Scopes       []string
	TenantID     int64
	IsActive     bool
}

// RegisteredClient is a registered client with its secret, which is only
// ever present right after it was created or rotated.
type RegisteredClient struct {
	ClientRecord
	Secret string
}

func (in *ClientInput) normalise(create bool) error {
	in.Name = strings.TrimSpace(in.Name)
	fields := map[string][]string{}
	if in.Name == "" {
		fields["name"] = append(fields["name"], "The name is required.")
	}
	if create && in.Type != TypePublic && in.Type != TypeConfidential {
		fields["type"] = append(fields["type"], "The type must be public or confidential.")
	}
	if len(in.RedirectURIs) == 0 {
		fields["redirect_uris"] = append(fields["redirect_uris"], "At least one redirect URI is required.")
	}
	for i, uri := range in.RedirectURIs {
		in.RedirectURIs[i] = strings.TrimSpace(uri)
		if err := validRedirect(in.RedirectURIs[i]); err != nil {
			fields["redirect_uris"] = append(fields["redirect_uris"], in.RedirectURIs[i]+": "+err.Error()+".")
		}
	}
	if len(in.Scopes) == 0 {
		in.Scopes = []string{mcp.ScopeRead, mcp.ScopeWrite}
	}
	for _, scope := range in.Scopes {
		if scope != mcp.ScopeRead && scope != mcp.ScopeWrite {
			fields["scopes"] = append(fields["scopes"], "Scopes must be mcp:read or mcp:write.")
			break
		}
	}
	if !slices.Contains(in.Scopes, mcp.ScopeRead) {
		in.Scopes = append([]string{mcp.ScopeRead}, in.Scopes...)
	}
	in.Scopes = slices.Compact(slices.Sorted(slices.Values(in.Scopes)))
	if len(fields) > 0 {
		return apperr.Validation(fields)
	}
	return nil
}

// CreateClient registers a client. A confidential client's secret is in the
// result, once.
func (s *Service) CreateClient(ctx context.Context, actorID int64, in ClientInput) (*RegisteredClient, error) {
	if err := in.normalise(true); err != nil {
		return nil, err
	}
	id, err := randomToken()
	if err != nil {
		return nil, apperr.Wrap(apperr.InternalServerError, "generate client id", err)
	}

	var secret, secretHash string
	if in.Type == TypeConfidential {
		if secret, err = randomToken(); err != nil {
			return nil, apperr.Wrap(apperr.InternalServerError, "generate client secret", err)
		}
		secretHash = hashSecret(secret)
	}
	client, err := s.repo.CreateClient(ctx, NewClient{
		ClientID: RegisteredPrefix + id[:32], Name: in.Name, Type: in.Type, SecretHash: secretHash,
		RedirectURIs: in.RedirectURIs, Scopes: in.Scopes, TenantID: in.TenantID, CreatedBy: actorID,
	})
	if err != nil {
		return nil, err
	}
	s.recordClient(ctx, actorID, ActionClientCreated, *client)
	return &RegisteredClient{ClientRecord: *client, Secret: secret}, nil
}

// ListClients pages through registered clients.
func (s *Service) ListClients(ctx context.Context, search string, limit, offset int32) ([]ClientRecord, int64, error) {
	clients, err := s.repo.ListClients(ctx, search, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.repo.CountClients(ctx, search)
	if err != nil {
		return nil, 0, err
	}
	return clients, total, nil
}

// GetClient returns one registered client.
func (s *Service) GetClient(ctx context.Context, id int64) (*ClientRecord, error) {
	return s.repo.GetClient(ctx, id)
}

// UpdateClient changes a registered client. Deactivating it revokes every
// connection made with it.
func (s *Service) UpdateClient(ctx context.Context, actorID, id int64, in ClientInput) (*ClientRecord, error) {
	current, err := s.repo.GetClient(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := in.normalise(false); err != nil {
		return nil, err
	}
	client, err := s.repo.UpdateClient(ctx, ClientUpdate{
		ID: id, Name: in.Name, RedirectURIs: in.RedirectURIs, Scopes: in.Scopes,
		TenantID: in.TenantID, IsActive: in.IsActive,
	})
	if err != nil {
		return nil, err
	}
	if current.IsActive && !client.IsActive {
		s.revokeClientGrants(ctx, client.ClientID)
	}
	s.recordClient(ctx, actorID, ActionClientUpdated, *client)
	return client, nil
}

// RotateSecret gives a confidential client a new secret; the old one keeps
// working for the grace period.
func (s *Service) RotateSecret(ctx context.Context, actorID, id int64) (*RegisteredClient, error) {
	current, err := s.repo.GetClient(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.Type != TypeConfidential {
		return nil, apperr.New(apperr.Conflict, "A public client has no secret to rotate.")
	}
	secret, err := randomToken()
	if err != nil {
		return nil, apperr.Wrap(apperr.InternalServerError, "generate client secret", err)
	}
	client, err := s.repo.RotateClientSecret(ctx, id, hashSecret(secret), s.now().Add(s.settings.SecretGrace))
	if err != nil {
		return nil, err
	}
	s.recordClient(ctx, actorID, ActionClientSecretRotated, *client)
	return &RegisteredClient{ClientRecord: *client, Secret: secret}, nil
}

// DeleteClient removes a registered client and revokes its connections.
func (s *Service) DeleteClient(ctx context.Context, actorID, id int64) error {
	current, err := s.repo.GetClient(ctx, id)
	if err != nil {
		return err
	}
	s.revokeClientGrants(ctx, current.ClientID)
	if err := s.repo.DeleteClient(ctx, id); err != nil {
		return err
	}
	s.recordClient(ctx, actorID, ActionClientDeleted, *current)
	return nil
}

func (s *Service) revokeClientGrants(ctx context.Context, clientID string) {
	if ids, err := s.repo.RevokeClientGrants(ctx, clientID); err == nil {
		_ = s.repo.DropGrantTokens(ctx, ids)
	}
}

// recordClient writes a system audit entry; secrets never go in it.
func (s *Service) recordClient(ctx context.Context, actorID int64, action string, client ClientRecord) {
	changes := map[string]any{
		"client_id": client.ClientID, "name": client.Name, "type": client.Type,
		"redirect_uris": client.RedirectURIs, "scopes": client.Scopes, "is_active": client.IsActive,
	}
	if client.TenantID != 0 {
		changes["tenant_id"] = client.TenantID
	}
	s.activity.Record(ctx, activity.Entry{
		ActorID: actorID, Action: action, Subject: "oauth_client", SubjectID: client.ID, Changes: changes,
	})
}
