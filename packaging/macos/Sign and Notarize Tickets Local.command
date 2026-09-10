#!/bin/bash
set -euo pipefail
IFS=$'\n\t'

app_name="Tickets Local"
version="0.5.8"
direwolf_version="1.8.1"
bundle_id="org.openises.tickets-local"
notary_profile="${NOTARY_PROFILE:-TicketsLocal-Notary}"
script_dir="$(cd "$(dirname "$0")" && pwd)"
output_dir="$script_dir/Signed Release"
binary_amd64="$script_dir/bin/tickets-local-amd64"
binary_arm64="$script_dir/bin/tickets-local-arm64"
info_plist="$script_dir/Info.plist"
license_file="$script_dir/Resources/LICENSE.txt"
notice_file="$script_dir/Resources/NOTICE.md"
third_party_notice_file="$script_dir/Resources/THIRD-PARTY-NOTICES.md"
third_party_source_dir="$script_dir/Resources/Third-Party-Source"
readme_file="$script_dir/Resources/README.txt"
icon_file="$script_dir/Resources/TicketsLocal.icns"
checksum_manifest="$script_dir/BINARY-SHA256SUMS.txt"
source_archive="$script_dir/Tickets-Local-${version}-source.zip"
direwolf_source_archive="$third_party_source_dir/DireWolf-${direwolf_version}-source.tar.gz"
work_dir=""

pause_before_close() {
  local exit_code=$?
  if [[ -n "$work_dir" && -d "$work_dir" ]]; then
    /bin/rm -rf "$work_dir"
  fi
  echo
  if [[ $exit_code -eq 0 ]]; then
    echo "Finished successfully."
  else
    echo "The release was not completed. Review the error above."
  fi
  read -r -p "Press Return to close." _
  exit "$exit_code"
}
trap pause_before_close EXIT

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

require_executable() {
  [[ -x "$1" ]] || fail "Required macOS tool is missing: $1"
}

resolve_macho_dependency() {
  local dependency="$1"
  local source_file="$2"
  local source_dir
  local suffix
  local base
  source_dir="$(cd "$(dirname "$source_file")" && pwd)"
  base="$(basename "$dependency")"
  case "$dependency" in
    /*)
      [[ -f "$dependency" ]] && { echo "$dependency"; return 0; }
      ;;
    @loader_path/*)
      suffix="${dependency#@loader_path/}"
      [[ -f "$source_dir/$suffix" ]] && { echo "$source_dir/$suffix"; return 0; }
      ;;
    @executable_path/*)
      suffix="${dependency#@executable_path/}"
      [[ -f "$(dirname "$direwolf_active_source")/$suffix" ]] && {
        echo "$(dirname "$direwolf_active_source")/$suffix"
        return 0
      }
      ;;
    @rpath/*)
      suffix="${dependency#@rpath/}"
      for candidate in \
        "$source_dir/$suffix" \
        "$source_dir/../lib/$suffix" \
        "/opt/homebrew/lib/$base" \
        "/usr/local/lib/$base"; do
        [[ -f "$candidate" ]] && { echo "$candidate"; return 0; }
      done
      ;;
  esac
  return 1
}

copy_macho_dependencies() {
  local source_file="$1"
  local bundled_file="$2"
  local bundled_lib_dir="$3"
  local bundled_kind="$4"
  local dependency
  local resolved
  local base
  local target
  local replacement

  while IFS= read -r dependency; do
    [[ -n "$dependency" ]] || continue
    case "$dependency" in
      /System/*|/usr/lib/*)
        continue
        ;;
    esac
    resolved="$(resolve_macho_dependency "$dependency" "$source_file" || true)"
    [[ -n "$resolved" ]] ||
      fail "Could not resolve Dire Wolf dependency '$dependency' from '$source_file'."
    base="$(basename "$resolved")"
    target="$bundled_lib_dir/$base"
    if [[ ! -f "$target" ]]; then
      /usr/bin/ditto "$resolved" "$target"
      /bin/chmod 755 "$target"
      copy_macho_dependencies "$resolved" "$target" "$bundled_lib_dir" "library"
      /usr/bin/install_name_tool -id "@loader_path/$base" "$target" 2>/dev/null || true
    fi
    if [[ "$bundled_kind" == "executable" ]]; then
      replacement="@loader_path/lib/$base"
    else
      replacement="@loader_path/$base"
    fi
    /usr/bin/install_name_tool -change "$dependency" "$replacement" "$bundled_file"
  done < <(
    /usr/bin/otool -L "$source_file" |
      /usr/bin/tail -n +2 |
      /usr/bin/awk '{print $1}'
  )
}

bundle_direwolf_binary() {
  local source="$1"
  local version_output
  local architecture
  local go_arch
  local target_dir
  local target_binary
  local target_lib_dir
  version_output="$("$source" -h 2>&1 || true)"
  echo "$version_output" | /usr/bin/grep -q "Dire Wolf Release ${direwolf_version}" ||
    fail "The decoder at '$source' is not Dire Wolf ${direwolf_version}; its bundled GPL source would not match."
  direwolf_active_source="$source"
  while IFS= read -r architecture; do
    case "$architecture" in
      x86_64) go_arch="amd64" ;;
      arm64) go_arch="arm64" ;;
      *) continue ;;
    esac
    target_dir="$resources_dir/direwolf/$go_arch"
    target_binary="$target_dir/direwolf"
    target_lib_dir="$target_dir/lib"
    if [[ -x "$target_binary" ]]; then
      continue
    fi
    /bin/mkdir -p "$target_lib_dir"
    /usr/bin/ditto "$source" "$target_binary"
    /bin/chmod 755 "$target_binary"
    copy_macho_dependencies "$source" "$target_binary" "$target_lib_dir" "executable"
    echo "Bundled Dire Wolf for $architecture from:"
    echo "  $source"
  done < <(/usr/bin/lipo -archs "$source" | /usr/bin/tr ' ' '\n')
}

echo "Tickets Local $version — signed and notarized Mac release"
echo "=========================================================="
echo
echo "This tool will:"
echo "  1. Build one Intel + Apple-silicon app."
echo "  2. Sign it with your Developer ID Application certificate."
echo "  3. Create and sign a DMG."
echo "  4. Submit the DMG to Apple for notarization."
echo "  5. Staple Apple's ticket and verify Gatekeeper acceptance."
echo

require_executable /usr/bin/codesign
require_executable /usr/bin/security
require_executable /usr/bin/xcrun
require_executable /usr/bin/lipo
require_executable /usr/bin/otool
require_executable /usr/bin/install_name_tool
require_executable /usr/bin/hdiutil
require_executable /usr/bin/ditto
require_executable /usr/bin/plutil
require_executable /usr/bin/shasum
require_executable /usr/sbin/spctl

/usr/bin/xcrun --find notarytool >/dev/null 2>&1 ||
  fail "notarytool is unavailable. Install or update Xcode, then try again."

for required_file in \
  "$binary_amd64" \
  "$binary_arm64" \
  "$info_plist" \
  "$license_file" \
  "$notice_file" \
  "$third_party_notice_file" \
  "$readme_file" \
  "$icon_file" \
  "$checksum_manifest" \
  "$source_archive" \
  "$direwolf_source_archive"; do
  [[ -f "$required_file" ]] || fail "Release-kit file is missing: $required_file"
done
[[ -d "$third_party_source_dir" ]] || fail "Third-party source directory is missing: $third_party_source_dir"
[[ "$(/usr/bin/find "$third_party_source_dir" -type f | /usr/bin/wc -l | /usr/bin/tr -d ' ')" -eq 6 ]] ||
  fail "The release kit must contain all six pinned third-party source archives."

echo "Verifying the release-kit binaries..."
(
  cd "$script_dir"
  /usr/bin/shasum -a 256 -c "BINARY-SHA256SUMS.txt"
)

amd64_archs="$(/usr/bin/lipo -archs "$binary_amd64")"
arm64_archs="$(/usr/bin/lipo -archs "$binary_arm64")"
[[ "$amd64_archs" == "x86_64" ]] ||
  fail "The Intel binary has the wrong architecture: $amd64_archs"
[[ "$arm64_archs" == "arm64" ]] ||
  fail "The Apple-silicon binary has the wrong architecture: $arm64_archs"

echo "Checking the saved Apple notarization profile..."
/usr/bin/xcrun notarytool history \
  --keychain-profile "$notary_profile" \
  >/dev/null ||
  fail "The '$notary_profile' Keychain profile could not be validated."

signing_identity="${SIGNING_IDENTITY:-}"
if [[ -z "$signing_identity" ]]; then
  identities=()
  while IFS= read -r identity; do
    [[ -n "$identity" ]] && identities[${#identities[@]}]="$identity"
  done < <(
    /usr/bin/security find-identity -v -p codesigning |
      /usr/bin/sed -n 's/.*"\(Developer ID Application:[^"]*\)".*/\1/p'
  )

  [[ ${#identities[@]} -gt 0 ]] ||
    fail "No valid Developer ID Application signing identity was found."

  if [[ ${#identities[@]} -eq 1 ]]; then
    signing_identity="${identities[0]}"
  else
    echo
    echo "More than one Developer ID Application identity is available:"
    for index in "${!identities[@]}"; do
      printf "  %d. %s\n" "$((index + 1))" "${identities[$index]}"
    done
    echo
    read -r -p "Choose the certificate number: " identity_choice
    [[ "$identity_choice" =~ ^[0-9]+$ ]] ||
      fail "The certificate selection was not a number."
    ((identity_choice >= 1 && identity_choice <= ${#identities[@]})) ||
      fail "The certificate selection was outside the available range."
    signing_identity="${identities[$((identity_choice - 1))]}"
  fi
fi

echo
echo "Signing identity:"
echo "  $signing_identity"
echo "Notarization profile:"
echo "  $notary_profile"
echo
echo "macOS may ask for permission to use the private key."

work_dir="$(/usr/bin/mktemp -d "${TMPDIR:-/tmp}/tickets-local-sign.XXXXXX")"
app_dir="$work_dir/$app_name.app"
contents_dir="$app_dir/Contents"
macos_dir="$contents_dir/MacOS"
resources_dir="$contents_dir/Resources"
dmg_stage="$work_dir/dmg"
dmg_name="Tickets-Local-${version}-macOS-universal-notarized.dmg"
dmg_path="$work_dir/$dmg_name"
notary_result="$work_dir/notary-result.json"

/bin/mkdir -p "$macos_dir" "$resources_dir" "$dmg_stage" "$output_dir"
/usr/bin/lipo -create \
  "$binary_amd64" \
  "$binary_arm64" \
  -output "$macos_dir/tickets-local"
/bin/chmod 755 "$macos_dir/tickets-local"

universal_archs="$(/usr/bin/lipo -archs "$macos_dir/tickets-local")"
[[ "$universal_archs" == *"x86_64"* && "$universal_archs" == *"arm64"* ]] ||
  fail "The universal binary was not assembled correctly: $universal_archs"

/usr/bin/ditto "$info_plist" "$contents_dir/Info.plist"
/usr/bin/ditto "$license_file" "$resources_dir/LICENSE.txt"
/usr/bin/ditto "$notice_file" "$resources_dir/NOTICE.md"
/usr/bin/ditto "$third_party_notice_file" "$resources_dir/THIRD-PARTY-NOTICES.md"
/usr/bin/ditto "$readme_file" "$resources_dir/README.txt"
/usr/bin/ditto "$icon_file" "$resources_dir/TicketsLocal.icns"
/usr/bin/ditto "$third_party_source_dir" "$resources_dir/Third-Party-Source"
/usr/bin/plutil -lint "$contents_dir/Info.plist"

plist_version="$(/usr/bin/plutil -extract CFBundleShortVersionString raw -o - "$contents_dir/Info.plist")"
plist_bundle_id="$(/usr/bin/plutil -extract CFBundleIdentifier raw -o - "$contents_dir/Info.plist")"
[[ "$plist_version" == "$version" ]] ||
  fail "Info.plist version '$plist_version' does not match '$version'."
[[ "$plist_bundle_id" == "$bundle_id" ]] ||
  fail "Info.plist bundle ID '$plist_bundle_id' does not match '$bundle_id'."

direwolf_sources=()
for brew_command in /opt/homebrew/bin/brew /usr/local/bin/brew; do
  if [[ -x "$brew_command" ]] && "$brew_command" list --versions direwolf >/dev/null 2>&1; then
    candidate="$("$brew_command" --prefix direwolf)/bin/direwolf"
    [[ -x "$candidate" ]] && direwolf_sources[${#direwolf_sources[@]}]="$candidate"
  fi
done
if [[ -n "${DIREWOLF_BINARY:-}" && -x "${DIREWOLF_BINARY}" ]]; then
  direwolf_sources[${#direwolf_sources[@]}]="${DIREWOLF_BINARY}"
fi

if [[ ${#direwolf_sources[@]} -eq 0 && "${SKIP_BUNDLED_DIREWOLF:-0}" != "1" ]]; then
  available_brew=""
  for brew_command in /opt/homebrew/bin/brew /usr/local/bin/brew; do
    if [[ -x "$brew_command" ]]; then
      available_brew="$brew_command"
      break
    fi
  done
  if [[ -n "$available_brew" ]]; then
    echo
    echo "Dire Wolf is needed for the built-in Local RF option."
    read -r -p "Install Dire Wolf with Homebrew now? [Y/n] " install_direwolf
    if [[ -z "$install_direwolf" || "$install_direwolf" =~ ^[Yy]$ ]]; then
      "$available_brew" install direwolf
      candidate="$("$available_brew" --prefix direwolf)/bin/direwolf"
      [[ -x "$candidate" ]] ||
        fail "Homebrew completed but the Dire Wolf executable was not found."
      direwolf_sources[${#direwolf_sources[@]}]="$candidate"
    fi
  fi
fi

if [[ ${#direwolf_sources[@]} -eq 0 ]]; then
  if [[ "${SKIP_BUNDLED_DIREWOLF:-0}" == "1" ]]; then
    echo
    echo "Building without bundled Dire Wolf."
    echo "Internet-only and Existing KISS TNC modes will remain available."
  else
    fail "Dire Wolf is not installed. Install Homebrew and run 'brew install direwolf', then run this signing tool again. Set SKIP_BUNDLED_DIREWOLF=1 only for an Internet/KISS-only release."
  fi
else
  echo
  echo "Bundling the optional Dire Wolf local radio decoder..."
  for direwolf_source in "${direwolf_sources[@]}"; do
    bundle_direwolf_binary "$direwolf_source"
  done
fi

echo
echo "Signing the universal app..."
if [[ -d "$resources_dir/direwolf" ]]; then
  while IFS= read -r nested_binary; do
    /usr/bin/codesign \
      --force \
      --options runtime \
      --timestamp \
      --sign "$signing_identity" \
      "$nested_binary"
  done < <(
    /usr/bin/find "$resources_dir/direwolf" -type f \
      \( -name 'direwolf' -o -name '*.dylib' \) |
      /usr/bin/sort
  )
fi
/usr/bin/codesign \
  --force \
  --options runtime \
  --timestamp \
  --sign "$signing_identity" \
  "$app_dir"
/usr/bin/codesign --verify --deep --strict --verbose=2 "$app_dir"

/usr/bin/ditto "$app_dir" "$dmg_stage/$app_name.app"
/bin/ln -s /Applications "$dmg_stage/Applications"

echo
echo "Creating and signing the DMG..."
/usr/bin/hdiutil create \
  -volname "$app_name" \
  -srcfolder "$dmg_stage" \
  -ov \
  -format UDZO \
  "$dmg_path"
/usr/bin/codesign \
  --force \
  --timestamp \
  --sign "$signing_identity" \
  "$dmg_path"
/usr/bin/codesign --verify --strict --verbose=2 "$dmg_path"
/usr/bin/hdiutil verify "$dmg_path"

echo
echo "Submitting to Apple. This can take several minutes..."
if ! /usr/bin/xcrun notarytool submit "$dmg_path" \
  --keychain-profile "$notary_profile" \
  --wait \
  --output-format json >"$notary_result"; then
  /bin/cat "$notary_result" 2>/dev/null || true
  fail "Apple's notarization submission command failed."
fi
/bin/cat "$notary_result"

notary_status="$(/usr/bin/plutil -extract status raw -o - "$notary_result" 2>/dev/null || true)"
submission_id="$(/usr/bin/plutil -extract id raw -o - "$notary_result" 2>/dev/null || true)"
if [[ "$notary_status" != "Accepted" ]]; then
  if [[ -n "$submission_id" ]]; then
    log_path="$output_dir/Tickets-Local-${version}-notarization-error.json"
    /usr/bin/xcrun notarytool log "$submission_id" \
      --keychain-profile "$notary_profile" \
      "$log_path" || true
    echo "Apple's diagnostic log was saved to:"
    echo "  $log_path"
  fi
  fail "Apple returned notarization status '$notary_status'."
fi

echo
echo "Stapling Apple's notarization ticket..."
/usr/bin/xcrun stapler staple "$dmg_path"
/usr/bin/xcrun stapler validate "$dmg_path"
/usr/sbin/spctl \
  --assess \
  --type open \
  --context context:primary-signature \
  --verbose=4 \
  "$dmg_path"

final_dmg="$output_dir/$dmg_name"
checksum_name="Tickets-Local-${version}-macOS-universal-notarized-SHA256.txt"
final_checksum="$output_dir/$checksum_name"
final_source="$output_dir/Tickets-Local-${version}-source.zip"
final_third_party_source="$output_dir/Third-Party-Source"

# A notarized DMG is reproducible from the committed signing kit. Replace the
# prior build instead of accumulating large timestamped local backups.
/usr/bin/find "$output_dir" -type f -name '*.dmg.previous-*' -delete
/bin/rm -f "$final_dmg"
/usr/bin/ditto "$dmg_path" "$final_dmg"
/usr/bin/ditto "$source_archive" "$final_source"
/bin/rm -rf "$final_third_party_source"
/usr/bin/ditto "$third_party_source_dir" "$final_third_party_source"
(
  cd "$output_dir"
  /usr/bin/shasum -a 256 "$dmg_name" >"$checksum_name"
)

echo
echo "The signed and notarized release is ready:"
echo "  $final_dmg"
echo
echo "Checksum:"
echo "  $final_checksum"
echo
echo "Matching application and third-party source archives:"
echo "  $final_source"
echo "  $final_third_party_source"
echo
echo "End users can open the DMG, drag Tickets Local to Applications, and"
echo "launch it normally without the old preparation or quarantine process."
