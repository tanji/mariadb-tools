#!/bin/bash
# Pre-removal script for galeracheck

if command -v systemctl &> /dev/null; then
    if systemctl is-active --quiet galeracheck; then
        systemctl stop galeracheck
    fi
    if systemctl is-enabled --quiet galeracheck; then
        systemctl disable galeracheck
    fi
fi

exit 0
