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
     - Creates a GitHub Release with a changelog (auto-generated from commits, excluding docs/test/ci/chore prefixes)

4. **Verify the release** on the [GitHub Releases page](https://github.com/exploreomni/cli/releases).

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
