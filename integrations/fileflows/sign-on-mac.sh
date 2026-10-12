#!/bin/bash
set -euo pipefail
cd -- "$(dirname -- "$0")"
if [[ "$(uname -s)" != Darwin ]]; then
  echo 'Run this script on your Mac; Apple Shortcuts signing requires macOS.' >&2
  exit 1
fi
mkdir -p Signed
for shortcut in 'Process and Organize Videos - Stash' 'Upload Jess Videos - Stash'; do
  shortcuts sign --mode anyone --input "$shortcut.shortcut" --output "Signed/$shortcut.shortcut"
done
printf '%s\n' 'Signing complete. Open Signed/Process and Organize Videos - Stash.shortcut first,' \
  'then Signed/Upload Jess Videos - Stash.shortcut. Let iCloud sync them to your iPhone.'
open Signed
