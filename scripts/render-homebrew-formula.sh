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

artifact_name() {
  local os="$1"
  local arch="$2"
  printf 'omni_%s_%s_%s.tar.gz' "$version" "$os" "$arch"
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
      url "https://github.com/${repo}/releases/download/${tag}/${darwin_arm64}"
      sha256 "${darwin_arm64_sha}"
    end

    on_intel do
      url "https://github.com/${repo}/releases/download/${tag}/${darwin_amd64}"
      sha256 "${darwin_amd64_sha}"
    end
  end

  on_linux do
    on_arm do
      if Hardware::CPU.is_64_bit?
        url "https://github.com/${repo}/releases/download/${tag}/${linux_arm64}"
        sha256 "${linux_arm64_sha}"
      end
    end

    on_intel do
      url "https://github.com/${repo}/releases/download/${tag}/${linux_amd64}"
      sha256 "${linux_amd64_sha}"
    end
  end

  def install
    bin.install "omni"
  end

  test do
    system "#{bin}/omni", "--help"
  end
end
EOF
