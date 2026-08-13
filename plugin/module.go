package plugin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bomly-dev/bomly-sdk"
)

// Name is the plugin's identity. It MUST equal the "id" field in
// bomly-plugin.json — Bomly refuses to load a plugin whose manifest id and
// runtime descriptor name disagree. It is also the descriptor name the Bomly
// CLI composition keys on when it embeds this matcher.
const Name = "scorecard"

// displayName is the human-readable matcher name.
const displayName = "OpenSSF Scorecard"

// moduleConfig is the JSON configuration accepted in managed execution under
// plugins.matchers.scorecard. Durations are Go duration strings ("24h").
type moduleConfig struct {
	APIBase     string `json:"api_base"`
	CacheDir    string `json:"cache_dir"`
	CacheTTL    string `json:"cache_ttl"`
	BypassCache bool   `json:"bypass_cache"`
}

// moduleDescriptor is the matcher's static registration data, shared by the
// embedded Descriptor method and the managed Module constructor.
func moduleDescriptor() sdk.MatcherDescriptor {
	descriptor := (&Matcher{}).Descriptor()
	descriptor.ConfigSchema = sdk.MustConfigSchemaFor(moduleConfig{})
	return descriptor
}

// configFromHost builds the matcher Config from the host-provided JSON block.
func configFromHost(host sdk.HostContext) (Config, error) {
	var raw moduleConfig
	if err := host.DecodeConfig(&raw); err != nil {
		return Config{}, fmt.Errorf("decode scorecard matcher configuration: %w", err)
	}
	cfg := DefaultConfig()
	cfg.Logger = host.Logger()
	if strings.TrimSpace(raw.APIBase) != "" {
		cfg.APIBase = raw.APIBase
	}
	if strings.TrimSpace(raw.CacheDir) != "" {
		cfg.CacheDir = raw.CacheDir
	}
	if trimmed := strings.TrimSpace(raw.CacheTTL); trimmed != "" {
		parsed, err := time.ParseDuration(trimmed)
		if err != nil {
			return Config{}, fmt.Errorf("invalid cache_ttl: %w", err)
		}
		if parsed > 0 {
			cfg.CacheTTL = parsed
		}
	}
	cfg.BypassCache = raw.BypassCache
	cfg.ClientConfig = &ClientConfig{
		APIBase:            cfg.APIBase,
		Timeout:            15 * time.Second,
		HTTPClientProvider: host.HTTPClient(),
	}
	return cfg, nil
}

// Module packages the matcher for both execution modes: Bomly can embed it
// in-process or serve it as a managed plugin subprocess (see
// cmd/bomly-plugin-scorecard-matcher).
func Module() sdk.Module {
	return sdk.Module{
		Kind: sdk.PluginKindMatcher,
		Matcher: &sdk.MatcherModule{
			Descriptor: moduleDescriptor(),
			New: func(_ context.Context, host sdk.HostContext) (sdk.Matcher, error) {
				cfg, err := configFromHost(host)
				if err != nil {
					return nil, err
				}
				return New(cfg)
			},
		},
	}
}
