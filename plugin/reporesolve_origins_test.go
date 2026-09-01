package plugin

import (
	"testing"

	sdk "github.com/bomly-dev/bomly-sdk"
)

// TestResolveRepoReadsDetectedOrigins pins the fix for a silent regression the
// SDK v0.7.0 bump would otherwise have introduced.
//
// This matcher used to read the repository out of the package PURL's
// repository_url / vcs_url / download_url qualifiers. ADR-0041 relocates those
// out of a package PURL and into origins at node construction, so that path
// now finds nothing for any package that came through the graph — and the
// failure is silent. Resolution returns "", the package is never scored, and
// the result is indistinguishable from a package that genuinely has no GitHub
// source.
func TestResolveRepoReadsDetectedOrigins(t *testing.T) {
	cases := []struct {
		name string
		pkg  *sdk.Package
		want string
	}{
		{
			name: "repository origin",
			pkg: &sdk.Package{
				PURL:            "pkg:npm/left-pad@1.3.0",
				DetectedOrigins: []sdk.DependencyOrigin{{Repository: "https://github.com/ossf/scorecard", Revision: "abc123"}},
			},
			want: "github.com/ossf/scorecard",
		},
		{
			name: "artifact origin hosted on github",
			pkg: &sdk.Package{
				PURL:            "pkg:npm/left-pad@1.3.0",
				DetectedOrigins: []sdk.DependencyOrigin{{ArtifactURL: "https://github.com/ossf/scorecard/archive/v5.0.0.tar.gz"}},
			},
			want: "github.com/ossf/scorecard",
		},
		{
			name: "a repository claim outranks an artifact URL",
			pkg: &sdk.Package{
				PURL: "pkg:npm/left-pad@1.3.0",
				DetectedOrigins: []sdk.DependencyOrigin{
					{ArtifactURL: "https://github.com/mirror/copy/archive/v1.tar.gz"},
					{Repository: "https://github.com/ossf/scorecard"},
				},
			},
			want: "github.com/ossf/scorecard",
		},
		{
			name: "no github source anywhere",
			pkg: &sdk.Package{
				PURL:            "pkg:npm/left-pad@1.3.0",
				DetectedOrigins: []sdk.DependencyOrigin{{Repository: "https://gitlab.com/owner/repo"}},
			},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveRepo(tc.pkg); got != tc.want {
				t.Errorf("resolveRepo = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestResolveRepoStillReadsQualifiers pins that the qualifier path is kept as
// a fallback. A PURL that arrived from outside the graph — an ingested SBOM
// that was never through a node constructor — can still carry them, and
// dropping the step would lose a source the document did state.
func TestResolveRepoStillReadsQualifiers(t *testing.T) {
	pkg := &sdk.Package{PURL: "pkg:npm/left-pad@1.3.0?repository_url=https://github.com/ossf/scorecard"}
	if got := resolveRepo(pkg); got != "github.com/ossf/scorecard" {
		t.Errorf("resolveRepo = %q, want the qualifier fallback to still work", got)
	}
}
