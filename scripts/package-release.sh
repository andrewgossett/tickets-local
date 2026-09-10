#!/usr/bin/env bash
set -euo pipefail

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dist_dir="$project_dir/dist"
version="0.5.8"
checksum_file="Tickets-Local-${version}-SHA256SUMS.txt"

mkdir -p "$dist_dir"

# Keep only the active release's DMG. Repeated signing must not accumulate
# timestamped backups, and a version bump removes notarized DMGs from older
# signing kits while leaving source files and non-DMG packages untouched.
find "$dist_dir" -type f -name '*.dmg.previous-*' -delete
find "$dist_dir" -type f -name 'Tickets-Local-*-macOS-universal-notarized.dmg' \
  ! -path "$dist_dir/Tickets-Local-${version}-macOS-Signing-Kit/Signed Release/Tickets-Local-${version}-macOS-universal-notarized.dmg" \
  -delete

build_binary() {
  local target_os="$1"
  local target_arch="$2"
  local output="$3"
  local flags="-s -w"
  if [[ "$target_os" == "windows" ]]; then
    flags="$flags -H=windowsgui"
  fi
  GOOS="$target_os" GOARCH="$target_arch" CGO_ENABLED=0 \
    go build -buildvcs=false -trimpath -ldflags="$flags" -o "$output" .
}

package_windows() {
  local package_dir="$dist_dir/Tickets-Local-Windows-x64"
  rm -rf "$package_dir"
  mkdir -p "$package_dir"
  build_binary windows amd64 "$package_dir/Tickets Local.exe"
  cp "$project_dir/packaging/PORTABLE-README.txt" "$package_dir/README.txt"
  cp "$project_dir/LICENSE" "$package_dir/LICENSE.txt"
  cp "$project_dir/NOTICE.md" "$package_dir/NOTICE.md"
  cp "$project_dir/THIRD-PARTY-NOTICES.md" "$package_dir/THIRD-PARTY-NOTICES.md"
  rm -f "$dist_dir/Tickets-Local-${version}-Windows-x64.zip"
  (
    cd "$dist_dir"
    zip -qry "Tickets-Local-${version}-Windows-x64.zip" "Tickets-Local-Windows-x64"
  )
}

package_macos() {
  local arch="$1"
  local package_dir="$dist_dir/Tickets-Local-macOS-$arch"
  local app_dir="$package_dir/Tickets Local.app"
  rm -rf "$package_dir"
  mkdir -p "$app_dir/Contents/MacOS" "$app_dir/Contents/Resources"
  build_binary darwin "$arch" "$app_dir/Contents/MacOS/tickets-local"
  cp "$project_dir/packaging/macos/Info.plist" "$app_dir/Contents/Info.plist"
  cp "$project_dir/packaging/macos/TicketsLocal.icns" "$app_dir/Contents/Resources/TicketsLocal.icns"
  cp "$project_dir/LICENSE" "$app_dir/Contents/Resources/LICENSE.txt"
  cp "$project_dir/NOTICE.md" "$app_dir/Contents/Resources/NOTICE.md"
  cp "$project_dir/THIRD-PARTY-NOTICES.md" "$app_dir/Contents/Resources/THIRD-PARTY-NOTICES.md"
  cp "$project_dir/packaging/PORTABLE-README.txt" "$package_dir/README.txt"
  cp "$project_dir/packaging/macos/Prepare Tickets Local.command" "$package_dir/Prepare Tickets Local.command"
  chmod +x "$package_dir/Prepare Tickets Local.command"
  if command -v codesign >/dev/null 2>&1; then
    codesign --force --deep --sign - --timestamp=none "$app_dir"
    codesign --verify --deep --strict --verbose=2 "$app_dir"
  fi
  rm -f "$dist_dir/Tickets-Local-${version}-macOS-${arch}.zip"
  (
    cd "$dist_dir"
    zip -qry "Tickets-Local-${version}-macOS-${arch}.zip" "Tickets-Local-macOS-$arch"
  )
}

package_linux() {
  local package_dir="$dist_dir/Tickets-Local-Linux-x64"
  rm -rf "$package_dir"
  mkdir -p "$package_dir"
  build_binary linux amd64 "$package_dir/tickets-local"
  cp "$project_dir/packaging/PORTABLE-README.txt" "$package_dir/README.txt"
  cp "$project_dir/LICENSE" "$package_dir/LICENSE.txt"
  cp "$project_dir/NOTICE.md" "$package_dir/NOTICE.md"
  cp "$project_dir/THIRD-PARTY-NOTICES.md" "$package_dir/THIRD-PARTY-NOTICES.md"
  rm -f "$dist_dir/Tickets-Local-${version}-Linux-x64.tar.gz"
  tar -C "$dist_dir" -czf "$dist_dir/Tickets-Local-${version}-Linux-x64.tar.gz" "Tickets-Local-Linux-x64"
}

package_source() {
  # Archive the reviewed commit, never untracked runtime data or credentials.
  # .gitattributes also excludes sensitive paths if accidentally force-added.
  git archive --format=zip --output="$dist_dir/Tickets-Local-${version}-source.zip" HEAD
}

cd "$project_dir"
if [[ -n "$(git status --porcelain --untracked-files=normal)" ]]; then
  echo "Commit reviewed source changes before packaging so binaries and source match." >&2
  exit 1
fi
go test ./...
package_windows
package_macos amd64
package_macos arm64
"$project_dir/scripts/package-macos-signing-kit.sh" \
  --amd64 "$dist_dir/Tickets-Local-macOS-amd64/Tickets Local.app/Contents/MacOS/tickets-local" \
  --arm64 "$dist_dir/Tickets-Local-macOS-arm64/Tickets Local.app/Contents/MacOS/tickets-local"
package_linux
package_source

if command -v sha256sum >/dev/null 2>&1; then
  (
    cd "$dist_dir"
    sha256sum \
      "./Tickets-Local-${version}-Windows-x64.zip" \
      "./Tickets-Local-${version}-macOS-amd64.zip" \
      "./Tickets-Local-${version}-macOS-arm64.zip" \
      "./Tickets-Local-${version}-Linux-x64.tar.gz" \
      "./Tickets-Local-${version}-source.zip" > "$checksum_file"
  )
else
  (
    cd "$dist_dir"
    shasum -a 256 \
      "./Tickets-Local-${version}-Windows-x64.zip" \
      "./Tickets-Local-${version}-macOS-amd64.zip" \
      "./Tickets-Local-${version}-macOS-arm64.zip" \
      "./Tickets-Local-${version}-Linux-x64.tar.gz" \
      "./Tickets-Local-${version}-source.zip" > "$checksum_file"
  )
fi

echo "Release packages written to $dist_dir"
