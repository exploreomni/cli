#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: $0 <tag> <checksums-file> <output-file>" >&2
  exit 1
fi

tag="$1"
checksums_file="$2"
output_file="$3"
version="${tag#v}"
repo="exploreomni/cli"
artifact_version="${OMNI_RELEASE_ARTIFACT_VERSION:-$version}"
release_base_url="${OMNI_RELEASE_BASE_URL:-}"

if [[ -n "$release_base_url" ]]; then
  release_base_url="${release_base_url%/}"
else
  release_base_url="https://github.com/${repo}/releases/download/${tag}"
fi

infer_artifact_version() {
  awk '{ print $2 }' "$checksums_file" | while IFS= read -r artifact; do
    case "$artifact" in
      omni_*_darwin_amd64.tar.gz) printf '%s\n' "${artifact#omni_}" | sed 's/_darwin_amd64\.tar\.gz$//' ;;
      omni_*_darwin_arm64.tar.gz) printf '%s\n' "${artifact#omni_}" | sed 's/_darwin_arm64\.tar\.gz$//' ;;
      omni_*_linux_amd64.tar.gz) printf '%s\n' "${artifact#omni_}" | sed 's/_linux_amd64\.tar\.gz$//' ;;
      omni_*_linux_arm64.tar.gz) printf '%s\n' "${artifact#omni_}" | sed 's/_linux_arm64\.tar\.gz$//' ;;
    esac
  done | sort -u
}

artifact_name() {
  local os="$1"
  local arch="$2"
  printf 'omni_%s_%s_%s.tar.gz' "$artifact_version" "$os" "$arch"
}

artifact_sha() {
  local artifact="$1"
  local sha

  sha="$(awk -v file="$artifact" '$2 == file { print $1 }' "$checksums_file")"
  if [[ -z "$sha" ]]; then
    echo "missing checksum for $artifact in $checksums_file" >&2
    exit 1
  fi

  printf '%s' "$sha"
}

if [[ -n "${OMNI_RELEASE_BASE_URL:-}" ]]; then
  expected_local_artifact="$(artifact_name darwin amd64)"
  if ! awk -v file="$expected_local_artifact" '$2 == file { found = 1 } END { exit found ? 0 : 1 }' "$checksums_file"; then
    inferred_versions="$(infer_artifact_version)"
    if [[ -z "$inferred_versions" ]]; then
      echo "could not infer artifact version from $checksums_file" >&2
      exit 1
    fi

    if [[ "$(printf '%s\n' "$inferred_versions" | wc -l | tr -d ' ')" != "1" ]]; then
      echo "multiple artifact versions found in $checksums_file; set OMNI_RELEASE_ARTIFACT_VERSION explicitly" >&2
      printf '%s\n' "$inferred_versions" >&2
      exit 1
    fi

    artifact_version="$inferred_versions"
  fi
fi

darwin_amd64="$(artifact_name darwin amd64)"
darwin_arm64="$(artifact_name darwin arm64)"
linux_amd64="$(artifact_name linux amd64)"
linux_arm64="$(artifact_name linux arm64)"

darwin_amd64_sha="$(artifact_sha "$darwin_amd64")"
darwin_arm64_sha="$(artifact_sha "$darwin_arm64")"
linux_amd64_sha="$(artifact_sha "$linux_amd64")"
linux_arm64_sha="$(artifact_sha "$linux_arm64")"

mkdir -p "$(dirname "$output_file")"

cat >"$output_file" <<EOF
class Omni < Formula
  desc "Command-line tool for the Omni API"
  homepage "https://github.com/${repo}"
  version "${version}"
  license "MIT"

  on_macos do
    on_arm do
      url "${release_base_url}/${darwin_arm64}"
      sha256 "${darwin_arm64_sha}"
    end

    on_intel do
      url "${release_base_url}/${darwin_amd64}"
      sha256 "${darwin_amd64_sha}"
    end
  end

  on_linux do
    on_arm do
      if Hardware::CPU.is_64_bit?
        url "${release_base_url}/${linux_arm64}"
        sha256 "${linux_arm64_sha}"
      end
    end

    on_intel do
      url "${release_base_url}/${linux_amd64}"
      sha256 "${linux_amd64_sha}"
    end
  end

  def install
    bin.install "omni"
  end

  test do
    system bin/"omni", "--help"
  end
end
EOF
