package version

import (
	"regexp"
	"strings"
	"testing"

	project "github.com/Innovix-Matrix-Systems/ims-go-saas-kit"
)

var semver = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-[0-9A-Za-z.-]+)?$`)

func TestVersionFileIsSemver(t *testing.T) {
	if !semver.MatchString(project.Version) {
		t.Fatalf("VERSION = %q, want semver like 0.1.0 (no leading v)", project.Version)
	}
}

func TestLocalBuildsAreMarkedDev(t *testing.T) {
	// Tests are never stamped with -ldflags, so they see the local form.
	if !strings.HasSuffix(Version, "-dev") || !strings.HasPrefix(Version, project.Version) {
		t.Fatalf("Version = %q, want %q", Version, project.Version+"-dev")
	}
}
