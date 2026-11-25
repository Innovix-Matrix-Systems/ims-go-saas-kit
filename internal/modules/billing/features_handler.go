package billing

import (
	"net/http"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/billing/domain"
)

// FeatureCatalogItem is one feature a package can grant.
type FeatureCatalogItem struct {
	Key         string `json:"key"`
	Description string `json:"description"`
	// IsLimit marks numeric limits (limit_value counts; -1 is unlimited).
	IsLimit bool `json:"is_limit"`
}

// features lists the feature catalog, so package editors need not hard-code it.
func (h *Handler) features(w http.ResponseWriter, r *http.Request) {
	out := make([]FeatureCatalogItem, 0, len(domain.Features))
	for _, feature := range domain.Features {
		out = append(out, FeatureCatalogItem{Key: string(feature), Description: feature.Description(), IsLimit: feature.IsLimit()})
	}
	h.Data(w, r, http.StatusOK, out)
}
