#!/usr/bin/env bash
set -eu
/usr/bin/python3.9 -I -B "$(dirname "$0")/transaction.py"
