#!/bin/bash
# Post-install script for xferd

set -e

# Create directories
mkdir -p /etc/xferd
mkdir -p /var/lib/xferd/temp
mkdir -p /var/lib/xferd/shadow

# Set ownership
chown -R xferd:xferd /var/lib/xferd

# If config doesn't exist, copy example
if [ ! -f /etc/xferd/config.yml ]; then
    if [ -f /etc/xferd/config.yml.example ]; then
        cp /etc/xferd/config.yml.example /etc/xferd/config.yml
        chown xferd:xferd /etc/xferd/config.yml
        chmod 600 /etc/xferd/config.yml
        echo "Example configuration installed to /etc/xferd/config.yml"
        echo "Please edit this file before starting the service."
    fi
fi

# Reload systemd
systemctl daemon-reload

echo ""
echo "Xferd installed successfully!"
echo ""
echo "Next steps:"
echo "  1. Edit /etc/xferd/config.yml with your settings"
echo "  2. Enable the service: sudo systemctl enable xferd"
echo "  3. Start the service: sudo systemctl start xferd"
echo "  4. Check status: sudo systemctl status xferd"
echo "  5. View logs: sudo journalctl -u xferd -f"

