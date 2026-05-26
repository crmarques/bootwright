# End-To-End Fixtures

These directories are runnable desired-state cases for provider, render, and
cluster-install flows. Each case uses the current desired-state schema:

```text
environment.yaml       Environment
hosts.yaml             Host
provider.yaml          InfraProvider
infra-component.yaml   InfraComponent (when the case needs artifact serving)
networks.yaml          NetworkConfig
cluster-infra.yaml     ClusterInfra
container-cluster.yaml ContainerCluster
```

## Cases

| Case | Purpose |
| --- | --- |
| `001-sno-libvirt` | Single-node OpenShift on libvirt with Redfish emulation |
| `002-sno-emul-baremetal` | Single-node bare-metal shape backed by emulated services |
| `003-3nodes-libvirt` | Three-node compact control plane on libvirt |
| `004-3nodes-emul-baremetal` | Three-node bare-metal shape backed by emulated services |
| `005-3nodes-baremetal` | Three-node real bare-metal shape with bonded VLAN networking |

## Local Fixture Checks

Run from the repository root:

```bash
make build
make list-e2e-cases
bin/bootwright context init 001-sno-libvirt -f test/e2e/001-sno-libvirt --yes
bin/bootwright check syntax
bin/bootwright render installer --scope sno-libvirt
```

Dry-run all fixtures through the apply pipeline:

```bash
set -euo pipefail
for case_dir in test/e2e/[0-9]*; do
  case_name=$(basename "$case_dir")
  make e2e-dry-run CASE="$case_name"
done
```

## Full E2E Run

Use the full run only when the matching infrastructure exists. The real
bare-metal case is destructive to the selected machines; libvirt cases need
KVM and permission to manage libvirt on the provider host.

1. Read the selected case README and confirm its substrate requirements.
2. Choose one bastion mode:
   - [containerized-bastion.md](containerized-bastion.md) for local,
     isolated Podman-based runs.
   - [bastion.md](bastion.md) for a Linux VM or physical host.
3. Export the e2e case name and initialize a context from the fixture on the
   bastion. The command copies the YAML into
   `/var/lib/bootwright/contexts/<context>/input-files`, creates state,
   secrets, runtime, workflow, and artifact-server directories, and selects the
   context:

   ```bash
   export CASE=<case-directory>

   # Host or VM bastion; run this from the repo path on the bastion.
   bootwright context init "$CASE" -f "test/e2e/$CASE" --yes

   # Containerized bastion alternative; the repo is mounted at /work.
   # bootwright context init "$CASE" -f "/work/test/e2e/$CASE" --yes
   ```
4. Validate the context and edit the imported desired state for the target
   hosts, addresses, BMC endpoints, and secret references:

   ```bash
   bootwright context validate
   sudo vi "/var/lib/bootwright/contexts/$CASE/input-files/environment.yaml"
   ```

   If `print-env` reports that proxy credentials would be printed,
   create the `proxy-credentials` secret in [common-steps.md](common-steps.md)
   first, then rerun it with `--sensitive`.

5. Follow [common-steps.md](common-steps.md) to create secrets, apply bastion
   dependencies, provision infra, install, verify, and destroy the cluster.

To apply every fixture from the repository root, then tear it down before the
next one starts:

```bash
set -euo pipefail
for case_dir in test/e2e/[0-9]*; do
  case_name=$(basename "$case_dir")

  make e2e CASE="$case_name"
  bin/bootwright context init "$case_name" -f "$case_dir" --yes
  bin/bootwright destroy cluster --yes
  bin/bootwright destroy infra --yes
  make clean-e2e-state CASE="$case_name"
done
```

If cases are copied and edited under another parent directory, pass that parent
as `E2E_DIR=<path>` to the `make` targets.

Generated state lives under `/var/lib/bootwright/contexts/<context>/state`.
Failed apply phases print the Ansible log path under
`/var/lib/bootwright/contexts/<context>/workflow/`.
