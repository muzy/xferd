#!/bin/bash
# Pre-install script for xferd

set -e

# Create xferd user if it doesn't exist
if ! id -u xferd >/dev/null 2>&1; then
    useradd --system --no-create-home --shell /bin/false --comment "Xferd Service User" xferd
fi

