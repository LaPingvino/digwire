#!/bin/bash
set -e

VERSION="0.3.1"
OUTPUT_DIR="dist/windows"
mkdir -p "$OUTPUT_DIR"

echo "🔨 Compiling Digwire v$VERSION for Windows (amd64)..."
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w -H=windowsgui -X main.Version=$VERSION" -o "$OUTPUT_DIR/digwire.exe" ./cmd/digwire

# Ensure .ico is generated
python3 -c "
from PIL import Image
img = Image.open('internal/web/embedded/digwire.png')
img.save('internal/web/embedded/digwire.ico', format='ICO', sizes=[(16,16), (32,32), (48,48), (64,64), (128,128), (256,256)])
" 2>/dev/null || true

cp internal/web/embedded/digwire.ico "$OUTPUT_DIR/"
cp scripts/install-windows.bat "$OUTPUT_DIR/install.bat"
cp scripts/uninstall-windows.bat "$OUTPUT_DIR/uninstall.bat"

echo "📦 Packaging Portable Windows Zip..."
ZIP_NAME="Digwire-v${VERSION}-Windows-x64.zip"
(cd "$OUTPUT_DIR" && python3 -c "
import zipfile, os
with zipfile.ZipFile('$ZIP_NAME', 'w', zipfile.ZIP_DEFLATED) as z:
    for f in ['digwire.exe', 'digwire.ico', 'install.bat', 'uninstall.bat']:
        if os.path.exists(f):
            z.write(f)
print('✓ Successfully packaged:', '$ZIP_NAME')
")

# If makensis is installed, compile native installer
if command -v makensis >/dev/null 2>&1; then
    echo "📦 Building NSIS Windows Setup executable..."
    makensis scripts/installer.nsi
    echo "✓ Built dist/windows/Digwire-v${VERSION}-Setup.exe"
fi

echo "✨ Windows distribution ready in $OUTPUT_DIR/"
ls -lh "$OUTPUT_DIR"
