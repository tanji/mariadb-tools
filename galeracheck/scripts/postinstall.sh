#!/bin/bash
# Post-installation script for galeracheck
#
if [ -f /etc/systemd/system/galeracheck.service ]; then
    rm /etc/systemd/system/galeracheck.service
fi

if command -v systemctl &> /dev/null; then
    systemctl daemon-reload
    if [ "$1" = "configure" ] && [ -z "$2" ]; then
        systemctl enable --now galeracheck
    else
        systemctl restart galeracheck
    fi
fi

exit 0
