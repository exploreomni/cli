#!/bin/sh
set -e

REPO="exploreomni/cli"
# GitHub's numeric IDs for the repo and its owner, checked against the
# attestation's signing certificate.
REPO_ID="1176524410"
REPO_OWNER_ID="100505341"
BINARY="omni"

main() {
    os=$(detect_os)
    arch=$(detect_arch)

    if [ "$os" = "windows" ]; then
        echo "Error: This install script does not support Windows." >&2
        echo "Please download the binary from https://github.com/${REPO}/releases" >&2
        exit 1
    fi

    version=$(fetch_latest_version)
    if [ -z "$version" ]; then
        echo "Error: Could not determine latest version." >&2
        exit 1
    fi

    # Strip leading 'v' for archive name
    version_num="${version#v}"
    archive="${BINARY}_${version_num}_${os}_${arch}.tar.gz"
    url="https://github.com/${REPO}/releases/download/${version}/${archive}"
    checksums_url="https://github.com/${REPO}/releases/download/${version}/checksums.txt"
    bundle_url="https://github.com/${REPO}/releases/download/${version}/attestation.sigstore.json"

    tmpdir=$(mktemp -d)
    trap 'rm -rf "$tmpdir"' EXIT

    echo "Downloading ${BINARY} ${version} for ${os}/${arch}..."
    download "$url" "${tmpdir}/${archive}"
    download "$checksums_url" "${tmpdir}/checksums.txt"

    echo "Verifying checksum..."
    verify_checksum "${tmpdir}" "${archive}"
    verify_attestation "${tmpdir}" "${archive}" "${version}" "${bundle_url}"

    echo "Extracting..."
    tar -xzf "${tmpdir}/${archive}" -C "${tmpdir}"

    install_dir=$(select_install_dir)
    echo "Installing to ${install_dir}/${BINARY}..."
    mkdir -p "$install_dir"
    mv "${tmpdir}/${BINARY}" "${install_dir}/${BINARY}"
    chmod +x "${install_dir}/${BINARY}"

    echo "Successfully installed ${BINARY} ${version} to ${install_dir}/${BINARY}"
}

detect_os() {
    case "$(uname -s)" in
        Darwin) echo "darwin" ;;
        Linux)  echo "linux" ;;
        MINGW*|MSYS*|CYGWIN*) echo "windows" ;;
        *) echo "Error: Unsupported OS: $(uname -s)" >&2; exit 1 ;;
    esac
}

detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64)   echo "amd64" ;;
        arm64|aarch64)  echo "arm64" ;;
        *) echo "Error: Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
    esac
}

fetch_latest_version() {
    url="https://api.github.com/repos/${REPO}/releases/latest"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$url" | grep '"tag_name"' | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/'
    elif command -v wget >/dev/null 2>&1; then
        wget -qO- "$url" | grep '"tag_name"' | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/'
    else
        echo "Error: curl or wget is required" >&2
        exit 1
    fi
}

download() {
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL -o "$2" "$1"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "$2" "$1"
    fi
}

verify_checksum() {
    dir="$1"
    file="$2"
    expected=$(grep "$file" "${dir}/checksums.txt" | awk '{print $1}')
    if [ -z "$expected" ]; then
        echo "Error: Checksum not found for ${file}" >&2
        exit 1
    fi

    if command -v sha256sum >/dev/null 2>&1; then
        actual=$(sha256sum "${dir}/${file}" | awk '{print $1}')
    elif command -v shasum >/dev/null 2>&1; then
        actual=$(shasum -a 256 "${dir}/${file}" | awk '{print $1}')
    else
        echo "Error: No sha256 tool found (need sha256sum or shasum); refusing to install without checksum verification" >&2
        exit 1
    fi

    if [ "$expected" != "$actual" ]; then
        echo "Error: Checksum mismatch for ${file}" >&2
        echo "  Expected: ${expected}" >&2
        echo "  Actual:   ${actual}" >&2
        exit 1
    fi
}

# Verifies the archive's GitHub Artifact Attestation: proof it was built by
# this repo's release workflow from the tag being installed. The checksum
# alone can't show that, since checksums.txt ships in the same release.
#
# OMNI_VERIFY_ATTESTATION=auto (default) verifies when it can and skips with
#   a note when it can't (no suitable gh, no bundle, Sigstore unreachable);
#   =require fails instead of skipping; =skip turns it off. A verification
#   that runs and fails always aborts.
verify_attestation() {
    dir="$1"
    file="$2"
    version="$3"
    bundle_url="$4"
    mode="${OMNI_VERIFY_ATTESTATION:-auto}"

    case "$mode" in
        skip) return 0 ;;
        auto|require) ;;
        *) echo "Error: OMNI_VERIFY_ATTESTATION must be auto, require or skip (got '${mode}')" >&2; exit 1 ;;
    esac

    # --source-ref arrived in gh 2.68, the newest flag used below. With
    # --bundle, gh verifies without `gh auth login` or any GitHub API call.
    if ! command -v gh >/dev/null 2>&1 || ! gh attestation verify --help 2>/dev/null | grep -q -- '--source-ref'; then
        attestation_unavailable "$mode" "GitHub CLI 2.68+ not found"
        return 0
    fi

    if ! download "$bundle_url" "${dir}/attestation.sigstore.json" 2>/dev/null || [ ! -s "${dir}/attestation.sigstore.json" ]; then
        attestation_unavailable "$mode" "release ${version} has no attestation bundle"
        return 0
    fi

    echo "Verifying build provenance..."
    # Prints the signing repo's numeric IDs: names can be renamed or
    # re-registered, IDs can't.
    if ids=$(gh attestation verify "${dir}/${file}" \
        --bundle "${dir}/attestation.sigstore.json" \
        --repo "$REPO" \
        --signer-workflow "${REPO}/.github/workflows/release.yml" \
        --source-ref "refs/tags/${version}" \
        --deny-self-hosted-runners \
        --format json \
        --jq '.[0].verificationResult.signature.certificate | "\(.sourceRepositoryIdentifier) \(.sourceRepositoryOwnerIdentifier)"' \
        2>"${dir}/attestation.err"); then
        if [ "$ids" = "${REPO_ID} ${REPO_OWNER_ID}" ]; then
            return 0
        fi
        echo "Error: Build provenance for ${file} was signed by repository ID '${ids}', expected '${REPO_ID} ${REPO_OWNER_ID}'" >&2
        exit 1
    fi

    # gh fetches Sigstore's trust root over the network; behind a proxy that
    # blocks it, nothing was checked, so that's "unavailable", not a failure.
    if grep -q 'error creating Sigstore verifier' "${dir}/attestation.err"; then
        attestation_unavailable "$mode" "could not reach Sigstore to fetch its trust root"
        return 0
    fi
    echo "Error: Build provenance verification failed for ${file}" >&2
    sed 's/^/  /' "${dir}/attestation.err" >&2
    exit 1
}

attestation_unavailable() {
    if [ "$1" = "require" ]; then
        echo "Error: Cannot verify build provenance: $2 (OMNI_VERIFY_ATTESTATION=require)" >&2
        exit 1
    fi
    echo "Note: Skipping build provenance check: $2. The checksum was verified." >&2
}

select_install_dir() {
    if [ -w /usr/local/bin ]; then
        echo "/usr/local/bin"
    else
        dir="${HOME}/.local/bin"
        mkdir -p "$dir"
        echo "$dir"
        case ":${PATH}:" in
            *":${dir}:"*) ;;
            *) echo "Warning: ${dir} is not in your PATH. Add it with: export PATH=\"${dir}:\$PATH\"" >&2 ;;
        esac
    fi
}

main
