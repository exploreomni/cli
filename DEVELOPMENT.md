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

## Publishing a New Version

Releases are fully automated via GitHub Actions and [GoReleaser](https://goreleaser.com/).

### Homebrew Rollout Plan

1. **Phase 1: ExploreOmni tap.** Publish a tap-only formula to `exploreomni/homebrew-tap` from the release workflow. GoReleaser is still used for GitHub release artifacts and checksums, but not for the Homebrew formula itself. This supports `brew install exploreomni/tap/omni`, and `brew tap exploreomni/tap && brew install omni` until `homebrew/core` exists.
2. **Phase 2: `homebrew/core`.** Submit a separate curated formula to `homebrew/core` so fresh installs can use `brew install omni` with no tap step.
3. **Migration window:** keep the tap formula working even after the core formula lands so `brew install omni` resolves to `homebrew/core`, while `brew install exploreomni/tap/omni` remains valid while docs and downstream skills catch up.

The phase-1 tap formula is intentionally separate from the eventual `homebrew/core` formula. GoReleaser handles release packaging only. The release workflow then renders the tap formula directly from the published release checksums instead of using GoReleaser's deprecated `brews` integration, and the core formula will still be maintained separately.

### Steps

1. **Ensure `main` is ready.** All changes should be merged and CI should be green.

2. **Ensure the Homebrew tap prerequisites are in place.**
   - Create the public tap repo `exploreomni/homebrew-tap` with `main` as its default branch.
   - Add the `HOMEBREW_TAP_GITHUB_TOKEN` secret to the `exploreomni/cli` repo. This token needs `contents:write` access to `exploreomni/homebrew-tap` so GitHub Actions can update the tap from the release workflow.

3. **Create and push a version tag:**

   ```bash
   git tag v1.2.3
   git push origin v1.2.3
   ```

   Use [semantic versioning](https://semver.org/). Pre-release versions (e.g., `v1.2.3-beta.1`) are automatically marked as pre-releases on GitHub.

4. **The release pipeline runs automatically.** Pushing a `v*` tag triggers the [Release workflow](../.github/workflows/release.yml), which:
   - Checks out the repo with full history
   - Runs GoReleaser, which:
      - Builds cross-platform binaries (macOS amd64/arm64, Linux amd64/arm64, Windows amd64)
      - Creates tar.gz archives (zip for Windows)
      - Generates SHA-256 checksums
    - Renders and publishes `Formula/omni.rb` to `exploreomni/homebrew-tap` for stable releases
    - Creates a GitHub Release with a changelog (auto-generated from commits, excluding docs/test/ci/chore prefixes)

5. **Verify the release** on the [GitHub Releases page](https://github.com/exploreomni/cli/releases) and confirm Homebrew can install it:

   ```bash
   brew install exploreomni/tap/omni
   brew tap exploreomni/tap
   brew install omni
   omni --version
   ```

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

### Testing the Homebrew Tap Locally

To validate the tap formula against locally built artifacts instead of a real GitHub release:

```bash
goreleaser release --snapshot --clean
python3 -m http.server 8000 --directory dist
OMNI_RELEASE_BASE_URL=http://127.0.0.1:8000 \
  bash bin/render-homebrew-formula.sh v0.0.0-test dist/checksums.txt /tmp/omni.rb
brew tap-new --no-git local/omni-test
cp /tmp/omni.rb "$(brew --repository local/omni-test)/Formula/omni.rb"
brew audit --strict --new local/omni-test/omni
brew install local/omni-test/omni
brew test local/omni-test/omni
```

When `OMNI_RELEASE_BASE_URL` is set, the formula renderer serves artifacts from that base URL and can infer the snapshot artifact version from `dist/checksums.txt`.

Current Homebrew releases reject `brew audit /tmp/omni.rb` and `brew install --formula /tmp/omni.rb` for local formula files, so the supported local validation path is to copy the rendered formula into a temporary tap first.

Clean up the local test tap afterward if desired:

```bash
brew uninstall omni
brew untap local/omni-test
```

### Versioning

The version is baked into the binary via `-ldflags "-X main.version=..."`. GoReleaser sets this from the git tag automatically. For local builds, it defaults to `dev`.
