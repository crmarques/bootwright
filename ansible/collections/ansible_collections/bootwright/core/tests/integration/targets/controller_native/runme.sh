#!/usr/bin/env bash
set -eu
if [[ -x /usr/bin/python3.9 && ! -e /etc/fedora-release ]]; then
    /usr/bin/python3.9 -I -B "$(dirname "$0")/native_transaction.py"
else
    /usr/bin/python3 -I -B "$(dirname "$0")/native_transaction.py"
fi
