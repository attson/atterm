#!/usr/bin/env bash
set -euo pipefail

version_no_v="${VERSION#v}"
app="desktop/build/bin/AT Term.app"
out="desktop/build/bin/${ARTIFACT_NAME}_${version_no_v}_${ARCH}.dmg"

test -d "$app"
rm -f "$out"
hdiutil create -volname "AT Term" -srcfolder "$app" -ov -format UDZO "$out"
ls -la "$out"
