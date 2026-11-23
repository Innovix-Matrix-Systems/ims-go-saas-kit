package branches

import (
	"strings"
	"time"
)

// InsertUpdateRequest is the branch create/update payload.
type InsertUpdateRequest struct {
	Name        string  `json:"name" validate:"required,min=2,max=255"`
	Address     *string `json:"address" validate:"omitempty,max=255"`
	Description *string `json:"description" validate:"omitempty,max=1000"`
	Phone       *string `json:"phone" validate:"omitempty,max=50"`
	Email       *string `json:"email" validate:"omitempty,email,max=255"`
	ManagerID   *int64  `json:"manager_id" validate:"omitempty,gt=0"`
}

func (req InsertUpdateRequest) toInput() CreateParams {
	return CreateParams{
		Name:        strings.TrimSpace(req.Name),
		Address:     req.Address,
		Description: req.Description,
		Phone:       req.Phone,
		Email:       req.Email,
		ManagerID:   req.ManagerID,
	}
}

// Response is the public branch representation.
type Response struct {
	ID          int64     `json:"id"`
	TenantID    int64     `json:"tenant_id"`
	Name        string    `json:"name"`
	Address     *string   `json:"address"`
	Description *string   `json:"description"`
	Phone       *string   `json:"phone"`
	Email       *string   `json:"email"`
	ManagerID   *int64    `json:"manager_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// NewResponse maps a branch to its public representation.
func NewResponse(branch Branch) Response {
	return Response{
		ID:          branch.ID,
		TenantID:    branch.TenantID,
		Name:        branch.Name,
		Address:     branch.Address,
		Description: branch.Description,
		Phone:       branch.Phone,
		Email:       branch.Email,
		ManagerID:   branch.ManagerID,
		CreatedAt:   branch.CreatedAt,
		UpdatedAt:   branch.UpdatedAt,
	}
}
