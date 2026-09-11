#!/usr/bin/env bash
set -eu
"${ANSIBLE_TEST_PYTHON_INTERPRETER:-python3}" -I -B "$(dirname "$0")/supervisor.py"
