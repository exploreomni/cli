#!/bin/sh
set -e

REPO="exploreomni/cli"
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

    tmpdir=$(mktemp -d)
    trap 'rm -rf "$tmpdir"' EXIT

    echo "Downloading ${BINARY} ${version} for ${os}/${arch}..."
    download "$url" "${tmpdir}/${archive}"
    download "$checksums_url" "${tmpdir}/checksums.txt"

    echo "Verifying checksum..."
    verify_checksum "${tmpdir}" "${archive}"

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
        echo "Warning: No sha256 tool found, skipping checksum verification" >&2
        return
    fi

    if [ "$expected" != "$actual" ]; then
        echo "Error: Checksum mismatch for ${file}" >&2
        echo "  Expected: ${expected}" >&2
        echo "  Actual:   ${actual}" >&2
        exit 1
    fi
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
