#!/bin/sh
# Ensure data directory is writable
if [ -n "$DATA_DIR" ]; then
    mkdir -p "$DATA_DIR"
fi
exec ./monitor
