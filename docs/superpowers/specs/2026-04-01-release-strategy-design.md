# Release Strategy Design

## Context

omni-cli is a Go CLI that auto-generates commands from an embedded OpenAPI spec. Users currently must clone the repo and run `make build` to get a binary. We need a release pipeline so users can install pre-built binaries without building from source.

## Distribution Channels

1. **Homebrew tap (phase 1)** — formula published to `exploreomni/homebrew-tap`
2. **GitHub Releases** — pre-built binaries with SHA256 checksums for macOS (arm64/amd64), Linux (arm64/amd64), Windows (amd64)
3. **Install script** — `curl -fsSL https://raw.githubusercontent.com/exploreomni/cli/main/install.sh | sh`
4. **`go install`** — `go install github.com/exploreomni/omni-cli/cmd/omni@latest`

## Homebrew Rollout Plan

1. **Phase 1: tap formula.** Publish `omni` to `exploreomni/homebrew-tap` so users can run `brew install exploreomni/tap/omni`, or `brew tap exploreomni/tap && brew install omni`.
2. **Phase 2: `homebrew/core`.** Submit a separate curated formula to `homebrew/core` so fresh installs can use `brew install omni` with no tap setup.
3. **Migration window:** keep the tap formula working after the core formula lands so both install paths remain valid while docs and downstream skills migrate.

Important constraint: GoReleaser still handles GitHub release artifacts and checksums, but the phase-1 tap formula is rendered by our release workflow from those tagged release artifacts. The eventual `homebrew/core` formula is intentionally a separate artifact that will be maintained independently.

## Release Trigger

Push a semver git tag (e.g., `v0.1.0`) to trigger the pipeline. No manual workflow dispatch.

## Version Strategy

- Start at `v0.1.0` (pre-stability)
- GoReleaser injects version from the git tag via ldflags (`-X main.version={{.Version}}`)
- Pre-release tags (e.g., `v0.1.0-rc.1`) are auto-marked as pre-releases on GitHub
- Add `debug.ReadBuildInfo()` fallback in `main.go` so `go install` users see the correct version instead of "dev"

## New Files

### `.goreleaser.yml`

GoReleaser v2 configuration:

- **Before hooks:** `cp api/openapi.json cmd/omni/openapi.json` (replicates the Makefile `spec` target so the embedded spec is fresh)
- **Build:** `./cmd/omni` with `CGO_ENABLED=0`, ldflags `-s -w -X main.version={{.Version}}`
- **Targets:** darwin/amd64, darwin/arm64, linux/amd64, linux/arm64, windows/amd64
- **Archives:** `omni_{Version}_{Os}_{Arch}` — tar.gz for unix, zip for windows
- **Checksums:** SHA256, file named `checksums.txt`
- **Homebrew tap:** render `Formula/omni.rb` from release artifacts and publish it to `exploreomni/homebrew-tap` for stable releases
- **Changelog:** auto-generated from commits, filtered to exclude docs/test/ci/chore prefixes
- **Release:** to `exploreomni/cli`, not draft, prerelease auto-detected from tag

### `.github/workflows/release.yml`

GitHub Actions workflow:

- **Trigger:** push tags matching `v*`
- **Permissions:** `contents: write`
- **Steps:** checkout (full history for changelog), setup-go (version from go.mod), goreleaser-action v6 with `release --clean`
- **Secrets:** `GITHUB_TOKEN` plus `HOMEBREW_TAP_GITHUB_TOKEN` with `contents:write` access to `exploreomni/homebrew-tap`

### `install.sh`

POSIX shell install script:

1. Detect OS via `uname -s` (Darwin/Linux) and arch via `uname -m` (x86_64 -> amd64, aarch64/arm64 -> arm64)
2. Fetch latest release tag from `https://api.github.com/repos/exploreomni/cli/releases/latest`
3. Download the matching archive from GitHub Releases
4. Download `checksums.txt` and verify SHA256
5. Extract binary and install to `/usr/local/bin` (fallback to `~/.local/bin`)
6. Print success with version

No Windows support in the script — Windows users download from Releases or use `go install`.

## Modified Files

### `cmd/omni/main.go`

Add a `debug.ReadBuildInfo()` fallback after the `version` var declaration:

```go
import "runtime/debug"

func init() {
    if version == "dev" {
        if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
            version = info.Main.Version
        }
    }
}
```

This gives `go install` users the correct version (Go stamps module version into the binary). When built via GoReleaser or `make build` with `VERSION=`, the ldflags value takes precedence since it's set before `init()` runs.

## Verification

1. Run `goreleaser check` to validate the config
2. Run `goreleaser release --snapshot --clean` locally to test a dry-run build (creates binaries but doesn't publish)
3. Verify all 5 platform archives are created with correct names
4. Verify `checksums.txt` is generated
5. Tag `v0.1.0` and push to trigger a real release
6. Test each install method:
   - Run `brew install exploreomni/tap/omni` and check version output
   - Run `brew tap exploreomni/tap && brew install omni` and check version output
   - Download binary from GitHub Releases, verify checksum, run `omni --version`
   - Run the install script on macOS and Linux
   - Run `go install github.com/exploreomni/omni-cli/cmd/omni@v0.1.0` and check version output
