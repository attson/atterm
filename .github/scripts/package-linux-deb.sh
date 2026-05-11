#!/usr/bin/env bash
set -euo pipefail

version_no_v="${VERSION#v}"
bin="desktop/build/bin/AT Term"
out="desktop/build/bin/${ARTIFACT_NAME}_${version_no_v}_${ARCH}.deb"
root="$(mktemp -d)"
trap 'rm -rf "$root"' EXIT

test -f "$bin"

install -Dm755 "$bin" "$root/usr/bin/AT-Term"
install -Dm644 desktop/build/appicon.png "$root/usr/share/pixmaps/AT-Term.png"
install -Dm644 desktop/build/appicon.png "$root/usr/share/icons/hicolor/1024x1024/apps/AT-Term.png"
install -Dm644 /dev/stdin "$root/usr/share/applications/AT-Term.desktop" <<DESKTOP
[Desktop Entry]
Type=Application
Name=AT Term
Exec=AT-Term
Icon=AT-Term
Terminal=false
Categories=System;TerminalEmulator;
DESKTOP

installed_size="$(du -sk "$root/usr" | awk '{print $1}')"
mkdir -p "$root/DEBIAN"
cat > "$root/DEBIAN/control" <<CONTROL
Package: at-term
Version: ${version_no_v}
Section: utils
Priority: optional
Architecture: ${ARCH}
Maintainer: liuzaisen <liuzaisen@wanxinbuzhi.com>
Installed-Size: ${installed_size}
Depends: libgtk-3-0, libwebkit2gtk-4.1-0
Description: AT Term desktop app
 Cross-platform terminal emulator with attachable synced sessions.
CONTROL

dpkg-deb --build --root-owner-group "$root" "$out"
ls -la "$out"
