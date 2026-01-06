#!/bin/bash
# Pre-remove script for xferd

set -e

# Stop and disable service if running
if systemctl is-active --quiet xferd; then
    systemctl stop xferd
fi

if systemctl is-enabled --quiet xferd; then
    systemctl disable xferd
fi

