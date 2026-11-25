package identity

import (
	"context"
	"mime/multipart"
	"net/http"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/httpx"
)

// avatarMaxBytes is the maximum avatar upload size (5 MB).
const avatarMaxBytes = 5 << 20

// PhotoManager resolves and updates user profile photos (implemented by the
// files module; nil disables avatar handling).
type PhotoManager interface {
	UpdateAvatar(ctx context.Context, actorID, userID int64, tenantID int64, header *multipart.FileHeader) error
	PhotoURL(ctx context.Context, userID int64) (string, error)
	PhotoURLs(ctx context.Context, userIDs []int64) (map[int64]string, error)
}

// Handler serves user HTTP endpoints.
type Handler struct {
	service  *Service
	photo    PhotoManager
	exchange *ExchangeService
	checker  *authz.Checker
	httpx.Kit
}

// NewHandler creates the user handler.
func NewHandler(service *Service, photo PhotoManager, exchange *ExchangeService, checker *authz.Checker, kit httpx.Kit) *Handler {
	return &Handler{service: service, photo: photo, exchange: exchange, checker: checker, Kit: kit}
}

func (h *Handler) responseFor(ctx context.Context, user User) Response {
	roles, err := h.checker.RoleRefs(ctx, user.ID)
	if err != nil {
		roles = nil
	}
	return NewResponse(user, roles, h.photoURL(ctx, user.ID))
}

// avatarMultipartSlack leaves room for multipart framing.
const avatarMultipartSlack = 64 << 10

func (h *Handler) photoURL(ctx context.Context, userID int64) *string {
	if h.photo == nil {
		return nil
	}
	url, err := h.photo.PhotoURL(ctx, userID)
	if err != nil {
		return nil
	}
	return stringPtr(url)
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// importMultipartSlack leaves room for multipart framing.
const importMultipartSlack = 64 << 10

// can is route middleware requiring a permission (Laravel's can:...).
func (h *Handler) can(permission string) func(http.Handler) http.Handler {
	return authz.Require(h.checker, permission, h.Responder)
}
