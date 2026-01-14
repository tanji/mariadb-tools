#!/bin/bash
# Post-installation script for galeracheck

if command -v systemctl &> /dev/null; then
    systemctl daemon-reload
    echo "Systemd daemon reloaded. To enable and start galeracheck:"
    echo "  systemctl enable galeracheck"
    echo "  systemctl start galeracheck"
fi

exit 0
