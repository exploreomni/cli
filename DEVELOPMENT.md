# Development Guide

## Prerequisites

- Go (version specified in `go.mod`)
- Make

## AI-Assisted Development

Using Claude, Copilot, or other AI coding tools is expected and encouraged. However, all AI-generated code must be thoroughly reviewed and validated before requesting review for a PR. You are responsible for understanding and standing behind every line of code you submit.

## Building

```bash
make build       # Build to ./bin/omni
make test        # Run tests
make clean       # Remove built binary
```

## Updating the OpenAPI Spec

The CLI auto-generates commands from the embedded OpenAPI spec. To update it:

```bash
OMNI_OPENAPI_SPEC=/path/to/openapi.json make sync-spec
```

This copies the spec into both `api/openapi.json` (source of truth) and `cmd/omni/openapi.json` (embedded copy). Rebuild after syncing.

### Claude Code: `/update-from-api-spec`

If you use Claude Code, the project ships a `/update-from-api-spec` slash command that drives the whole spec-bump flow end to end: previewing the diff, syncing, building, validating that the new commands and flags appear in `--help`, then committing and opening a PR. Pass an issue reference and/or a spec path:

```
/update-from-api-spec #47
/update-from-api-spec /path/to/openapi.json
/update-from-api-spec #47 add fullyResolved param
```

It resolves the source spec from the argument or `$OMNI_OPENAPI_SPEC`, and asks if neither is set. The skill is defined at [.claude/commands/update-from-api-spec.md](.claude/commands/update-from-api-spec.md) — edit it there.

## Publishing a New Version

Releases are fully automated via GitHub Actions and [GoReleaser](https://goreleaser.com/).

### Steps

1. **Ensure `main` is ready.** All changes should be merged and CI should be green.

2. **Create and push a version tag:**

   ```bash
   git tag v1.2.3
   git push origin v1.2.3
   ```

   Use [semantic versioning](https://semver.org/). Pre-release versions (e.g., `v1.2.3-beta.1`) are automatically marked as pre-releases on GitHub.

3. **The release pipeline runs automatically.** Pushing a `v*` tag triggers the [Release workflow](../.github/workflows/release.yml), which:
   - Checks out the repo with full history
   - Runs GoReleaser, which:
      - Builds cross-platform binaries (macOS amd64/arm64, Linux amd64/arm64, Windows amd64)
      - Creates tar.gz archives (zip for Windows)
      - Generates SHA-256 checksums
    - Renders and publishes `Formula/omni.rb` to `exploreomni/homebrew-tap` for stable releases
    - Creates a GitHub Release with a changelog (auto-generated from commits, excluding docs/test/ci/chore prefixes)

4. **Verify the release** on the [GitHub Releases page](https://github.com/exploreomni/cli/releases) and confirm Homebrew can install it:

   ```bash
   brew tap exploreomni/tap
   brew install omni
   omni --version
   ```

## Homebrew Setup

This section documents how the Homebrew distribution is structured, for reference when making changes to the release pipeline.

### Rollout Plan

1. **Phase 1: ExploreOmni tap.** Publish a tap-only formula to `exploreomni/homebrew-tap` from the release workflow. GoReleaser is still used for GitHub release artifacts and checksums, but not for the Homebrew formula itself. This supports `brew install exploreomni/tap/omni`, and `brew tap exploreomni/tap && brew install omni` until `homebrew/core` exists.
2. **Phase 2: `homebrew/core`.** Submit a separate curated formula to `homebrew/core` so fresh installs can use `brew install omni` with no tap step.
3. **Migration window:** keep the tap formula working even after the core formula lands so `brew install omni` resolves to `homebrew/core`, while `brew install exploreomni/tap/omni` remains valid while docs and downstream skills catch up.

The phase-1 tap formula is intentionally separate from the eventual `homebrew/core` formula. GoReleaser handles release packaging only. The release workflow renders the tap formula directly from the published release checksums instead of using GoReleaser's deprecated `brews` integration, and the core formula will still be maintained separately.

### Prerequisites

For the release workflow to publish the tap formula, the following must be in place:

- The public tap repo `exploreomni/homebrew-tap` exists with `main` as its default branch.
- The `HOMEBREW_TAP_GITHUB_TOKEN` secret is set on the `exploreomni/cli` repo. This token needs `contents:write` access to `exploreomni/homebrew-tap` so GitHub Actions can update the tap from the release workflow.

### What Gets Built

| Platform | Architecture | Archive Format |
|----------|-------------|----------------|
| macOS    | amd64, arm64 | tar.gz        |
| Linux    | amd64, arm64 | tar.gz        |
| Windows  | amd64        | zip            |

### Testing a Release Locally

To dry-run GoReleaser without publishing:

```bash
goreleaser release --snapshot --clean
```

This builds all artifacts into `dist/` without creating a GitHub release.


### Versioning

The version is baked into the binary via `-ldflags "-X main.version=..."`. GoReleaser sets this from the git tag automatically. For local builds, it defaults to `dev`.
