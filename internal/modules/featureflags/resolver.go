package featureflags

import platformflags "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/featureflags"

// Service satisfies the platform `feature:<key>` gate middleware resolver.
var _ platformflags.Resolver = (*Service)(nil)
