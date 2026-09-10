#!/bin/zsh
set -euo pipefail

script_dir="${0:A:h}"
app_path="$script_dir/Tickets Local.app"

if [[ ! -d "$app_path" ]]; then
  echo "Tickets Local.app was not found next to this preparation tool."
  echo "Keep both items in the same folder and try again."
  read -r "?Press Return to close."
  exit 1
fi

echo "Preparing Tickets Local for this Mac..."
/usr/bin/xattr -dr com.apple.quarantine "$app_path"
/usr/bin/codesign --force --deep --sign - --timestamp=none "$app_path"
/usr/bin/codesign --verify --deep --strict --verbose=2 "$app_path"

echo
echo "The app now has a valid local signature."
echo "Opening Tickets Local..."
/usr/bin/open "$app_path"

echo
echo "If macOS still blocks the first launch, Control-click Tickets Local.app"
echo "and choose Open, or use System Settings > Privacy & Security > Open Anyway."
read -r "?Press Return to close."
