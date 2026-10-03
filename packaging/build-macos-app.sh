#!/bin/sh
# Wraps a built darwin digwire binary in Digwire.app and zips it (macOS).
#   packaging/build-macos-app.sh <binary> <version> <zip>
set -eu
bin=$1
version=$2
zip=$3
root=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
app="$work/Digwire.app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
install -m755 "$bin" "$app/Contents/MacOS/digwire"

# icon: PNG -> iconset -> icns
set=$work/digwire.iconset
mkdir -p "$set"
for s in 16 32 128 256 512; do
	sips -z $s $s "$root/assets/digwire.png" --out "$set/icon_${s}x${s}.png" >/dev/null
	sips -z $((s * 2)) $((s * 2)) "$root/assets/digwire.png" --out "$set/icon_${s}x${s}@2x.png" >/dev/null
done
iconutil -c icns "$set" -o "$app/Contents/Resources/digwire.icns"

cat > "$app/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key><string>Digwire</string>
	<key>CFBundleDisplayName</key><string>Digwire</string>
	<key>CFBundleIdentifier</key><string>io.github.lapingvino.digwire</string>
	<key>CFBundleVersion</key><string>$version</string>
	<key>CFBundleShortVersionString</key><string>$version</string>
	<key>CFBundleExecutable</key><string>digwire</string>
	<key>CFBundleIconFile</key><string>digwire</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>LSMinimumSystemVersion</key><string>11.0</string>
	<key>NSHighResolutionCapable</key><true/>
	<key>CFBundleURLTypes</key>
	<array><dict>
		<key>CFBundleURLName</key><string>Magnet link</string>
		<key>CFBundleURLSchemes</key><array><string>magnet</string></array>
	</dict></array>
</dict>
</plist>
PLIST
mkdir -p "$(dirname "$zip")"
ditto -c -k --keepParent "$app" "$zip"
