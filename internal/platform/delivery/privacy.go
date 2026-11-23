package delivery

import (
	"context"
	"time"

	dbgen "github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/privacy"
)

// PrivacyContributor covers the deliveries log: which messages were sent to
// the user. The log never stores bodies; erasure clears the (masked) address
// and the link to the user, keeping the row for delivery statistics.
type PrivacyContributor struct{}

var _ privacy.Contributor = PrivacyContributor{}

// Name implements privacy.Contributor.
func (PrivacyContributor) Name() string { return "delivery" }

// Columns implements privacy.Contributor.
func (PrivacyContributor) Columns() []string { return []string{"deliveries.user_id"} }

type privacyDelivery struct {
	Channel   string     `json:"channel"`
	Template  string     `json:"template"`
	Status    string     `json:"status"`
	CreatedAt *time.Time `json:"created_at"`
	SentAt    *time.Time `json:"sent_at"`
}

// Export implements privacy.Contributor.
func (PrivacyContributor) Export(ctx context.Context, q *dbgen.Queries, s privacy.Subject, w privacy.Writer) error {
	rows, err := q.ListDeliveriesForPrivacy(ctx, s.UserIDPtr())
	if err != nil {
		return database.MapError("delivery.privacy_export", err)
	}
	messages := make([]privacyDelivery, 0, len(rows))
	for _, d := range rows {
		messages = append(messages, privacyDelivery{
			Channel: d.Channel, Template: d.Template, Status: d.Status,
			CreatedAt: privacy.Time(d.CreatedAt), SentAt: privacy.Time(d.SentAt),
		})
	}
	return w.JSON("messages", messages)
}

// Erase implements privacy.Contributor.
func (PrivacyContributor) Erase(ctx context.Context, q *dbgen.Queries, s privacy.Subject) (privacy.Report, error) {
	anonymized, err := q.AnonymizeDeliveriesForUser(ctx, s.UserIDPtr())
	if err != nil {
		return privacy.Report{}, database.MapError("delivery.privacy_erase", err)
	}
	return privacy.Report{Anonymized: anonymized}, nil
}
