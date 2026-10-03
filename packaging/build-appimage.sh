#!/bin/sh
# Builds digwire-<tag>-<arch>.AppImage from a built digwire binary, on a
# machine of that architecture.
#   packaging/build-appimage.sh <binary> <tag> [outdir]
set -eu
bin=$1
tag=$2
out=${3:-release_out}
arch=$(uname -m)
root=$(cd "$(dirname "$0")/.." && pwd)

appdir=$(mktemp -d)/Digwire.AppDir
mkdir -p "$appdir/usr/bin" "$appdir/usr/share/applications" "$appdir/usr/share/icons/hicolor/512x512/apps"
install -m755 "$bin" "$appdir/usr/bin/digwire"
install -m644 "$root/assets/digwire.desktop" "$appdir/usr/share/applications/digwire.desktop"
install -m644 "$root/assets/digwire.png" "$appdir/usr/share/icons/hicolor/512x512/apps/digwire.png"
cp "$root/assets/digwire.desktop" "$appdir/digwire.desktop"
cp "$root/assets/digwire.png" "$appdir/digwire.png"
cat > "$appdir/AppRun" <<'RUN'
#!/bin/sh
here=$(dirname "$(readlink -f "$0")")
exec "$here/usr/bin/digwire" "$@"
RUN
chmod +x "$appdir/AppRun"

tool=${APPIMAGETOOL:-}
if [ -z "$tool" ]; then
	tool=$(mktemp -d)/appimagetool
	curl -fsSL -o "$tool" "https://github.com/AppImage/appimagetool/releases/download/continuous/appimagetool-$arch.AppImage"
	chmod +x "$tool"
fi
mkdir -p "$out"
ARCH=$arch "$tool" --appimage-extract-and-run "$appdir" "$out/digwire-$tag-$arch.AppImage"
