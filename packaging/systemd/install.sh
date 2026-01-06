#!/bin/bash
# Xferd systemd installation script

set -e

echo "Installing xferd systemd service..."

# Create xferd user if it doesn't exist
if ! id -u xferd >/dev/null 2>&1; then
    echo "Creating xferd user..."
    useradd --system --no-create-home --shell /bin/false xferd
fi

# Create directories
echo "Creating directories..."
mkdir -p /etc/xferd
mkdir -p /var/lib/xferd/temp
mkdir -p /var/lib/xferd/shadow

# Set permissions
chown -R xferd:xferd /var/lib/xferd

# Install binary
echo "Installing binary..."
cp xferd /usr/bin/xferd
chmod +x /usr/bin/xferd

# Install service file
echo "Installing systemd service..."
cp xferd.service /etc/systemd/system/
systemctl daemon-reload

# Install example config if none exists
if [ ! -f /etc/xferd/config.yml ]; then
    echo "Installing example configuration..."
    cp config.example.yml /etc/xferd/config.yml
    chown xferd:xferd /etc/xferd/config.yml
    chmod 600 /etc/xferd/config.yml
    echo "WARNING: Please edit /etc/xferd/config.yml before starting the service"
fi

echo "Installation complete!"
echo ""
echo "Next steps:"
echo "  1. Edit /etc/xferd/config.yml with your settings"
echo "  2. Enable the service: sudo systemctl enable xferd"
echo "  3. Start the service: sudo systemctl start xferd"
echo "  4. Check status: sudo systemctl status xferd"
echo "  5. View logs: sudo journalctl -u xferd -f"

