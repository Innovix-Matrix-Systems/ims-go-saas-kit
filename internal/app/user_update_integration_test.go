package app_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/testkit"
)

type userFields struct {
	FirstName *string `json:"first_name"`
	LastName  *string `json:"last_name"`
	Phone     *string `json:"phone"`
	IsActive  bool    `json:"is_active"`
}

// PATCH /user/{id} keeps the optional fields it is not sent (like branch_id
// and is_active); an empty string clears one.
func TestUserUpdateKeepsOmittedFields(t *testing.T) {
	app := testkit.New(t)
	admin := app.User(t)
	member := app.User(t, testkit.Role(app.Role(t, "Member")))
	path := fmt.Sprintf("/api/v1/user/%d", member.ID)

	app.As(admin).Patch(path, map[string]any{
		"name": "Member One", "email": member.Email, "first_name": "Rahim", "last_name": "Uddin", "phone": "+8801700000001",
	}).Status(t, http.StatusOK)

	// Only deactivate: names and phone stay.
	var got userFields
	app.As(admin).Patch(path, map[string]any{"name": "Member One", "email": member.Email, "is_active": false}).
		Status(t, http.StatusOK).Data(t, &got)
	if got.IsActive || got.FirstName == nil || *got.FirstName != "Rahim" || got.LastName == nil || got.Phone == nil {
		t.Fatalf("after an is_active-only update = %+v, want names and phone kept", got)
	}

	// An empty string clears; null keeps.
	app.As(admin).Patch(path, map[string]any{"name": "Member One", "email": member.Email, "phone": "", "last_name": nil}).
		Status(t, http.StatusOK).Data(t, &got)
	if got.Phone != nil || got.LastName == nil || *got.LastName != "Uddin" {
		t.Fatalf("after clearing the phone = %+v, want no phone and the last name kept", got)
	}
}
