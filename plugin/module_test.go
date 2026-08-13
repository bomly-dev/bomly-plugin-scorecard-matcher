package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	sdk "github.com/bomly-dev/bomly-sdk"
	"github.com/bomly-dev/bomly-sdk/conformance"
	"go.uber.org/zap"
)

// testHost is a minimal HostContext for unit tests.
type testHost struct {
	config json.RawMessage
}

func (h testHost) Logger() *zap.Logger                 { return zap.NewNop() }
func (h testHost) HTTPClient() *sdk.HTTPClientProvider { return nil }
func (h testHost) Runtime() sdk.RuntimeInfo {
	return sdk.RuntimeInfo{Execution: sdk.ExecutionEmbedded}
}

func (h testHost) DecodeConfig(v any) error {
	payload := h.config
	if len(payload) == 0 {
		payload = json.RawMessage("{}")
	}
	return json.Unmarshal(payload, v)
}

// TestModuleConstructsMatcher checks the managed constructor honors the JSON
// config block.
func TestModuleConstructsMatcher(t *testing.T) {
	cacheDir := filepath.ToSlash(filepath.Join(t.TempDir(), "cache"))
	cfg := fmt.Sprintf(`{"api_base":"https://scorecard.example","cache_dir":%q,"cache_ttl":"12h","bypass_cache":true}`, cacheDir)
	component, err := Module().Matcher.New(context.Background(), testHost{config: json.RawMessage(cfg)})
	if err != nil {
		t.Fatalf("construct matcher: %v", err)
	}
	matcher, ok := component.(*Matcher)
	if !ok {
		t.Fatalf("unexpected component type %T", component)
	}
	if matcher.config.APIBase != "https://scorecard.example" {
		t.Fatalf("api base = %q", matcher.config.APIBase)
	}
	if !matcher.config.BypassCache {
		t.Fatal("expected bypass_cache to be honored")
	}
}

// newScorecardFixtureServer serves sampleResponse for the ossf/scorecard repo.
func newScorecardFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/projects/github.com/ossf/scorecard") {
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleResponse))
	}))
	t.Cleanup(server.Close)
	return server
}

func newDeltaGraphAndRegistry(t *testing.T) (*sdk.Graph, *sdk.PackageRegistry) {
	t.Helper()
	scored := sdk.NewDependency(sdk.Dependency{Coordinates: sdk.Coordinates{Name: "scorecard", Version: "v5.0.0", PURL: "pkg:github/ossf/scorecard@v5.0.0"}})
	unscored := sdk.NewDependency(sdk.Dependency{Coordinates: sdk.Coordinates{Name: "left-pad", Version: "1.3.0", PURL: "pkg:npm/left-pad@1.3.0", Ecosystem: sdk.EcosystemNPM}})
	graph := sdk.New()
	for _, dep := range []*sdk.Dependency{scored, unscored} {
		if err := graph.AddNode(dep); err != nil {
			t.Fatalf("AddNode: %v", err)
		}
	}
	return graph, sdk.NewPackageRegistry()
}

func newDeltaMatcher(t *testing.T, apiBase string) *Matcher {
	t.Helper()
	matcher, err := New(Config{APIBase: apiBase, CacheDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return matcher
}

// TestMatchDeltaEquivalence is the delta-protocol contract check: when the
// request sets AcceptPackageUpdates, Match must not enrich the request
// registry in place and must return deltas that — applied through the host's
// own merge, sdk.ApplyPackageUpdates — reproduce the registry the legacy
// full-registry path produces.
func TestMatchDeltaEquivalence(t *testing.T) {
	server := newScorecardFixtureServer(t)

	legacyGraph, legacyRegistry := newDeltaGraphAndRegistry(t)
	legacy, err := newDeltaMatcher(t, server.URL).Match(context.Background(), sdk.MatchRequest{
		Graph:    legacyGraph,
		Registry: legacyRegistry,
	})
	if err != nil {
		t.Fatalf("legacy Match() error = %v", err)
	}

	deltaGraph, deltaRegistry := newDeltaGraphAndRegistry(t)
	delta, err := newDeltaMatcher(t, server.URL).Match(context.Background(), sdk.MatchRequest{
		Graph:                deltaGraph,
		Registry:             deltaRegistry,
		AcceptPackageUpdates: true,
	})
	if err != nil {
		t.Fatalf("delta Match() error = %v", err)
	}

	if delta.Registry != nil {
		t.Fatal("delta path must not return a registry")
	}
	if len(delta.PackageUpdates) != 1 {
		t.Fatalf("expected 1 package update, got %d", len(delta.PackageUpdates))
	}
	update := delta.PackageUpdates[0]
	if update.PURL != "pkg:github/ossf/scorecard@v5.0.0" || !update.Matched {
		t.Fatalf("unexpected update %#v", update)
	}
	if len(update.Licenses) != 0 || len(update.Vulnerabilities) != 0 {
		t.Fatalf("update must carry only the mutated fields, got %#v", update)
	}
	if update.Scorecard == nil || update.Scorecard.AggregateScore != 8.7 {
		t.Fatalf("unexpected scorecard %#v", update.Scorecard)
	}

	// The matcher must not have enriched the request registry packages in
	// delta mode (RegistryPackagesForGraph seeds identity, never enrichment).
	for _, pkg := range deltaRegistry.All() {
		if pkg.Matched || pkg.Scorecard != nil {
			t.Fatalf("delta path mutated request registry package %#v", pkg)
		}
	}

	merged := sdk.ApplyPackageUpdates(deltaRegistry, delta.PackageUpdates)
	if diff := registryDiff(legacy.Registry, merged); diff != "" {
		t.Fatalf("merged delta registry differs from legacy registry: %s", diff)
	}
	if legacy.MatcherStats != delta.MatcherStats {
		t.Fatalf("matcher stats diverge: legacy %#v, delta %#v", legacy.MatcherStats, delta.MatcherStats)
	}
}

// registryDiff deep-compares two registries package by package.
func registryDiff(want, got *sdk.PackageRegistry) string {
	wantPkgs := want.All()
	gotPkgs := got.All()
	if len(wantPkgs) != len(gotPkgs) {
		return fmt.Sprintf("package count %d != %d", len(gotPkgs), len(wantPkgs))
	}
	for _, wantPkg := range wantPkgs {
		gotPkg, ok := got.Get(wantPkg.PURL)
		if !ok {
			return fmt.Sprintf("missing package %s", wantPkg.PURL)
		}
		if !reflect.DeepEqual(wantPkg, gotPkg) {
			return fmt.Sprintf("package %s differs: want %#v, got %#v", wantPkg.PURL, wantPkg, gotPkg)
		}
	}
	return ""
}

// TestConformance runs the SDK conformance suite against the module,
// including the bomly-plugin.json identity cross-check.
func TestConformance(t *testing.T) {
	conformance.Test(t, conformance.Config{
		Module:       Module(),
		ManifestPath: filepath.Join("..", "bomly-plugin.json"),
		SampleConfig: json.RawMessage(`{"api_base":"https://api.scorecard.dev","cache_ttl":"24h","bypass_cache":false}`),
	})
}

// TestProbeBinary builds the real plugin binary and probes it over the
// managed HashiCorp gRPC transport, asserting the served descriptor equals
// the in-process one.
func TestProbeBinary(t *testing.T) {
	goBinary, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not available; skipping managed-transport probe")
	}
	binaryPath := filepath.Join(t.TempDir(), "bomly-plugin-scorecard-matcher")
	build := exec.Command(goBinary, "build", "-o", binaryPath, "./cmd/bomly-plugin-scorecard-matcher")
	build.Dir = ".."
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build plugin binary: %v\n%s", err, output)
	}
	conformance.ProbeBinary(t, binaryPath, conformance.WithModule(Module()))
}
