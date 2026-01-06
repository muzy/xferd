#!/bin/bash
# Script to download the latest WinSW release for MSI packaging

set -e

WINSW_REPO="winsw/winsw"
OUTPUT_DIR="packaging/winsw/bin"

# Create output directory
mkdir -p "$OUTPUT_DIR"

echo "Fetching latest WinSW release..."

# Get the latest release info from GitHub API
LATEST_RELEASE=$(curl -s "https://api.github.com/repos/${WINSW_REPO}/releases/latest")

# Extract the version tag
VERSION=$(echo "$LATEST_RELEASE" | grep -Po '"tag_name": "\K.*?(?=")')
echo "Latest WinSW version: $VERSION"

# Extract download URLs for the binaries we need
WINSW_X64_URL=$(echo "$LATEST_RELEASE" | grep -Po '"browser_download_url": "\K.*?WinSW-x64.exe(?=")')
WINSW_X86_URL=$(echo "$LATEST_RELEASE" | grep -Po '"browser_download_url": "\K.*?WinSW-x86.exe(?=")')

if [ -z "$WINSW_X64_URL" ]; then
    echo "Error: Could not find WinSW-x64.exe download URL"
    exit 1
fi

if [ -z "$WINSW_X86_URL" ]; then
    echo "Error: Could not find WinSW-x86.exe download URL"
    exit 1
fi

# Download WinSW binaries
echo "Downloading WinSW-x64.exe..."
curl -L -o "$OUTPUT_DIR/WinSW-x64.exe" "$WINSW_X64_URL"

echo "Downloading WinSW-x86.exe..."
curl -L -o "$OUTPUT_DIR/WinSW-x86.exe" "$WINSW_X86_URL"

# Download the LICENSE file
echo "Downloading WinSW LICENSE..."
WINSW_LICENSE_URL="https://raw.githubusercontent.com/${WINSW_REPO}/${VERSION}/LICENSE.txt"
curl -L -o "$OUTPUT_DIR/WinSW-LICENSE.txt" "$WINSW_LICENSE_URL"

# Create a version file
echo "$VERSION" > "$OUTPUT_DIR/winsw-version.txt"

echo "WinSW binaries downloaded successfully to $OUTPUT_DIR"
echo "  - WinSW-x64.exe"
echo "  - WinSW-x86.exe"
echo "  - WinSW-LICENSE.txt"
echo "  - winsw-version.txt"

