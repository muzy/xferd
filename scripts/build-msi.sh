#!/bin/bash
# Script to build Windows MSI packages using WiX
# This should be run after goreleaser has built the Windows binaries

set -e

if ! command -v wixl &> /dev/null; then
    echo "Error: wixl (WiX toolset for Linux) is not installed"
    echo "Install with: sudo apt-get install wixl"
    echo "Or on macOS: brew install wixl"
    exit 1
fi

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}"
DIST_DIR="dist"
WIX_DIR="packaging/wix"
WINSW_DIR="packaging/winsw/bin"
BUILD_DIR="$DIST_DIR/msi-build"

echo "Building MSI for version: $VERSION"

# Clean and create build directory
rm -rf "$BUILD_DIR"
mkdir -p "$BUILD_DIR"

# Build for amd64
echo "Building MSI for windows-amd64..."
AMD64_DIR="$BUILD_DIR/amd64"
mkdir -p "$AMD64_DIR"

# Copy files for amd64
cp "$DIST_DIR/xferd_windows_amd64_v1/xferd.exe" "$AMD64_DIR/xferd.exe"
cp "config.example.windows.yml" "$AMD64_DIR/config.example.yml"
cp "packaging/winsw/xferd.xml" "$AMD64_DIR/xferd.xml"
cp "packaging/winsw/README.md" "$AMD64_DIR/README.md"
cp "$WINSW_DIR/WinSW-x64.exe" "$AMD64_DIR/WinSW.exe"
cp "$WINSW_DIR/WinSW-LICENSE.txt" "$AMD64_DIR/WinSW-LICENSE.txt"
cp "$WIX_DIR/license.rtf" "$AMD64_DIR/license.rtf"

# Generate MSI for amd64
# wixl needs to run from the source directory to find files
CURRENT_DIR=$(pwd)
(cd "$AMD64_DIR" && wixl -v "$CURRENT_DIR/$WIX_DIR/xferd.wxs" \
    -o "$CURRENT_DIR/$DIST_DIR/xferd_${VERSION}_windows_amd64.msi" \
    -D Version="${VERSION#v}")

echo "MSI packages built successfully:"
echo "  - $DIST_DIR/xferd_${VERSION}_windows_amd64.msi"

# Calculate checksums
cd "$DIST_DIR"
sha256sum "xferd_${VERSION}_windows_amd64.msi" >> xferd_${VERSION}_checksums.txt
cd ..

echo "Done! MSI packages are in $DIST_DIR/"
