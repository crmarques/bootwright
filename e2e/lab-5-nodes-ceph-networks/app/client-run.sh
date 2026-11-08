#!/bin/bash
#
# Run client.py in a container on client-01, on the guest's network. Run as
# root from the bundle directory the runbook copies to client-01:
#
#     sudo -n bash e2e-client/client-run.sh IMAGE URL MODE [EXPECTED_SOURCE]
#
# The script reaches the container on standard input, so nothing is mounted.
set -euo pipefail

bundle=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
image=${1:?usage: client-run.sh IMAGE URL MODE [EXPECTED_SOURCE]}
shift
exec podman run --rm -i --network host "$image" python3 - "$@" < "$bundle/client.py"
