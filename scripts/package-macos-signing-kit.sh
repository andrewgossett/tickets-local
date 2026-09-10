#!/usr/bin/env bash
set -euo pipefail

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dist_dir="$project_dir/dist"
version="0.5.11"
direwolf_version="1.8.1"
direwolf_source_sha256="89d5f7992ae1e74d8cf26ec6479dde74d1f480bde950043756e875a689d065d7"
kit_name="Tickets-Local-${version}-macOS-Signing-Kit"
kit_dir="$dist_dir/$kit_name"
archive_path="$dist_dir/$kit_name.zip"
source_cache_dir="${TICKETS_LOCAL_SOURCE_CACHE:-$project_dir/.tools/source-cache}"
amd64_binary=""
arm64_binary=""
temp_dir=""

usage() {
  cat <<'EOF'
Usage:
  package-macos-signing-kit.sh [--amd64 PATH --arm64 PATH]

When both binary paths are provided, the script packages those prebuilt macOS
binaries. Without paths, it cross-compiles both binaries using Go.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --amd64)
      [[ $# -ge 2 ]] || { usage >&2; exit 2; }
      amd64_binary="$2"
      shift 2
      ;;
    --arm64)
      [[ $# -ge 2 ]] || { usage >&2; exit 2; }
      arm64_binary="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -n "$amd64_binary" || -n "$arm64_binary" ]]; then
  [[ -n "$amd64_binary" && -n "$arm64_binary" ]] || {
    echo "Provide both --amd64 and --arm64 paths." >&2
    exit 2
  }
else
  command -v go >/dev/null 2>&1 || {
    echo "Go is required when prebuilt binary paths are not supplied." >&2
    exit 1
  }
  temp_dir="$(mktemp -d "${TMPDIR:-/tmp}/tickets-local-kit.XXXXXX")"
  trap '[[ -n "$temp_dir" && -d "$temp_dir" ]] && rm -rf "$temp_dir"' EXIT
  amd64_binary="$temp_dir/tickets-local-amd64"
  arm64_binary="$temp_dir/tickets-local-arm64"
  (
    cd "$project_dir"
    GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 \
      go build -buildvcs=false -trimpath -ldflags="-s -w" \
      -o "$amd64_binary" .
    GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 \
      go build -buildvcs=false -trimpath -ldflags="-s -w" \
      -o "$arm64_binary" .
  )
fi

[[ -f "$amd64_binary" ]] || {
  echo "Intel binary not found: $amd64_binary" >&2
  exit 1
}
[[ -f "$arm64_binary" ]] || {
  echo "Apple-silicon binary not found: $arm64_binary" >&2
  exit 1
}

rm -rf "$kit_dir"
mkdir -p "$kit_dir/bin" "$kit_dir/Resources"
mkdir -p "$kit_dir/Resources/Third-Party-Source"

cp "$amd64_binary" "$kit_dir/bin/tickets-local-amd64"
cp "$arm64_binary" "$kit_dir/bin/tickets-local-arm64"
chmod 755 \
  "$kit_dir/bin/tickets-local-amd64" \
  "$kit_dir/bin/tickets-local-arm64"

cp "$project_dir/packaging/macos/Info.plist" "$kit_dir/Info.plist"
cp "$project_dir/packaging/macos/Sign and Notarize Tickets Local.command" \
  "$kit_dir/Sign and Notarize Tickets Local.command"
chmod 755 "$kit_dir/Sign and Notarize Tickets Local.command"
cp "$project_dir/packaging/macos/SIGNED-RELEASE-README.txt" "$kit_dir/README.txt"
cp "$project_dir/LICENSE" "$kit_dir/Resources/LICENSE.txt"
cp "$project_dir/NOTICE.md" "$kit_dir/Resources/NOTICE.md"
cp "$project_dir/THIRD-PARTY-NOTICES.md" "$kit_dir/Resources/THIRD-PARTY-NOTICES.md"
cp "$project_dir/packaging/macos/SIGNED-APP-README.txt" "$kit_dir/Resources/README.txt"
cp "$project_dir/packaging/macos/TicketsLocal.icns" "$kit_dir/Resources/TicketsLocal.icns"

download_source() {
  local name="$1"
  local url="$2"
  local expected_sha256="$3"
  local destination="$kit_dir/Resources/Third-Party-Source/$name"
  local cached="$source_cache_dir/$name"
  local actual_sha256
  mkdir -p "$source_cache_dir"
  if [[ -f "$cached" ]]; then
    actual_sha256="$({ command -v sha256sum >/dev/null 2>&1 && sha256sum "$cached" || shasum -a 256 "$cached"; } | awk '{print $1}')"
  fi
  if [[ "${actual_sha256:-}" != "$expected_sha256" ]]; then
    rm -f "$cached"
    curl -fL --retry 3 --retry-delay 2 "$url" -o "$cached"
  fi
  cp "$cached" "$destination"
  actual_sha256="$(
  if command -v sha256sum >/dev/null 2>&1; then
      sha256sum "$destination" | awk '{print $1}'
  else
      shasum -a 256 "$destination" | awk '{print $1}'
  fi
  )"
  [[ "$actual_sha256" == "$expected_sha256" ]] || {
    echo "$name source checksum did not match the pinned release." >&2
    exit 1
  }
}

download_source "DireWolf-${direwolf_version}-source.tar.gz" \
  "https://github.com/wb2osz/direwolf/archive/refs/tags/${direwolf_version}.tar.gz" \
  "$direwolf_source_sha256"
download_source "gpsd-3.27.5-source.tar.xz" \
  "https://download.savannah.gnu.org/releases/gpsd/gpsd-3.27.5.tar.xz" \
  "dc4a62bad835282bae788772bc7cc8f8bec4c7a48e8dceeb37477a89091c4656"
download_source "hamlib-4.7.2-source.tar.gz" \
  "https://github.com/Hamlib/Hamlib/releases/download/4.7.2/hamlib-4.7.2.tar.gz" \
  "ae1fcf2dbc80ea0786ea8f047b09399c3f7737d1930442f61a031708ed33e88f"
download_source "hidapi-0.15.0-source.tar.gz" \
  "https://github.com/libusb/hidapi/archive/refs/tags/hidapi-0.15.0.tar.gz" \
  "5d84dec684c27b97b921d2f3b73218cb773cf4ea915caee317ac8fc73cef8136"
download_source "portaudio-19.7.0-source.tgz" \
  "https://files.portaudio.com/archives/pa_stable_v190700_20210406.tgz" \
  "47efbf42c77c19a05d22e627d42873e991ec0c1357219c0d74ce6a2948cb2def"
download_source "libusb-1.0.30-source.tar.bz2" \
  "https://github.com/libusb/libusb/releases/download/v1.0.30/libusb-1.0.30.tar.bz2" \
  "fea36f34f9156400209595e300840767ab1a385ede1dc7ee893015aea9c6dbaf"

(
  cd "$project_dir"
  zip -qry "$kit_dir/Tickets-Local-${version}-source.zip" . \
    -x "dist/*" "./dist/*" \
       ".git/*" "./.git/*" \
       ".tools/*" "./.tools/*" \
       ".pnpm-home/*" "./.pnpm-home/*" \
       "*.log" "*/events.ndjson" "*/network.json" \
       "*/aprs-credentials.json" "*/integration-cache.json" \
       "*/snapshots/*" "*/attachments/*"
)

if command -v sha256sum >/dev/null 2>&1; then
  (
    cd "$kit_dir"
    sha256sum \
      bin/tickets-local-amd64 \
      bin/tickets-local-arm64 >BINARY-SHA256SUMS.txt
  )
else
  (
    cd "$kit_dir"
    shasum -a 256 \
      bin/tickets-local-amd64 \
      bin/tickets-local-arm64 >BINARY-SHA256SUMS.txt
  )
fi

rm -f "$archive_path"
(
  cd "$dist_dir"
  zip -qry "$archive_path" "$kit_name"
)

echo "Mac signing kit written to $archive_path"
