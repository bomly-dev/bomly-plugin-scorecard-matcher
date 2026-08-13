# bomly-plugin-scorecard-matcher

OpenSSF Scorecard matcher for [Bomly](https://github.com/bomly-dev/bomly-cli).

It attaches [OpenSSF Scorecard](https://scorecard.dev) project-posture data
(aggregate score plus per-check results) to every package whose upstream
source repository resolves to a `github.com` URL. Coverage is bounded by repo
resolvability, not by ecosystem.

> **Already inside the Bomly CLI.** This matcher ships embedded in the `bomly`
> binary as the built-in `scorecard` matcher — you do not need to install this
> plugin to use Scorecard enrichment. This repository is the matcher's home as
> a standalone module: the Bomly CLI consumes the same code in-process, and
> the plugin binary serves it to hosts that run matchers as managed
> subprocesses.

## Identity

- Plugin id / descriptor name: `scorecard`
- Kind: matcher
- Module path: `github.com/bomly-dev/bomly-plugin-scorecard-matcher`

## Network behavior

This matcher performs network calls **only during enrichment** (`bomly scan
--enrich`), never during audit-only runs:

- `https://api.scorecard.dev/projects/github.com/{owner}/{repo}` — one fetch
  per unique resolved repository.

Responses are cached on disk (default `~/.bomly/cache/scorecard`, 24h TTL);
repositories the service has not scored are cached as a not-scored sentinel so
they are not re-requested within the TTL. Transport failures degrade to
warnings — a bad network never aborts the scan. Cache failures are non-fatal.

## Configuration

Embedded execution is configured through the Bomly CLI's own `scorecard`
settings. Managed execution reads a JSON block under
`plugins.matchers.scorecard`:

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `api_base` | string | `https://api.scorecard.dev` | Scorecard API base URL |
| `cache_dir` | string | `~/.bomly/cache/scorecard` | Response cache |
| `cache_ttl` | duration string | `24h` | Cache TTL |
| `bypass_cache` | bool | `false` | Always fetch fresh results |

## Package-updates delta protocol

The matcher advertises `package-updates-v1`. When the host sets
`AcceptPackageUpdates`, `Match` does not enrich the request registry and
returns one delta per enriched package (PURL, `Matched`, and the scorecard).
One nuance, documented in the descriptor: `Package.MergeFrom` fills
`Scorecard` only when the target package has none, while the in-place path
overwrites. This matcher is the sole producer of `Package.Scorecard`, so
packages reach it un-scored and the two shapes agree in practice; the
equivalence is pinned by `TestMatchDeltaEquivalence`.

## Development

```sh
make test    # unit tests + SDK conformance suite
make build   # build bin/bomly-plugin-scorecard-matcher
```

## License

Apache-2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
