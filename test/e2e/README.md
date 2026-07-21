# End-To-End Fixtures

These directories are runnable desired-state cases for provider, render, and
cluster-install flows. Each case uses the current desired-state schema. The flat
cases (001-005, 007) lay their objects out as single files at the case root:

```text
environment.yaml       Environment
service-machines.yaml             Machine
provider.yaml          InfraProvider
infra-component.yaml   InfraComponent shared infra services
networks.yaml          NetworkConfig
cluster-machines.yaml  Machine
container-cluster.yaml ContainerCluster
```

The nested cases (006, 008, 009, 010) instead use a `clusters/` + `infra/` tree
with per-object files (one object per file), mirroring a real multi-cluster
workspace; browse the case directory for its exact layout.

## Cases

| Case | Purpose |
| --- | --- |
| `001-sno-libvirt` | Single-node OpenShift on libvirt with Redfish emulation |
| `002-sno-emul-baremetal` | Single-node bare-metal shape backed by emulated services |
| `003-3nodes-libvirt` | Three-node compact control plane on libvirt |
| `004-3nodes-emul-baremetal` | Three-node bare-metal shape backed by emulated services |
| `005-3nodes-baremetal` | Three-node real bare-metal shape with bonded VLAN networking |
| `006-ceph-3nodes-libvirt-managed-os` | Three-node Ceph on libvirt with managed RHEL install |
| `007-sno-vsphere` | Single-node OpenShift on vCenter-managed vSphere VMs |
| `008-ceph-3nodes-vsphere-managed-os` | Three-node Ceph on vSphere with managed RHEL install |
| `009-ceph-3nodes-baremetal-managed-os` | Three-node Ceph on real bare metal with managed RHEL install |
| `010-ceph-3nodes-libvirt-boot-iso` | Three-node Ceph on libvirt booted from a prebuilt boot ISO |

## Local Fixture Checks

Run from the repository root:

```bash
make build
make list-e2e-cases
bin/bootwright context init --name 001-sno-libvirt -f test/e2e/001-sno-libvirt --yes
bin/bootwright validate
bin/bootwright render installer --clusters sno-libvirt
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
   bastion. The command records the fixture directory's absolute path as the
   context workspace (nothing is copied), prepares the context directories,
   and selects the context:

   ```bash
   export CASE=<case-directory>

   # Machine or VM bastion; run this from the repo path on the bastion.
   bootwright context init --name "$CASE" -f "test/e2e/$CASE" --yes

   # Containerized bastion alternative; the repo is mounted at /work.
   # bootwright context init --name "$CASE" -f "/work/test/e2e/$CASE" --yes
   ```
4. Validate the context and edit the workspace desired state for the target
   hosts, addresses, BMC endpoints, and secret references. Edits are picked up
   directly by the next command:

   ```bash
   bootwright status
   vi "test/e2e/$CASE/environment.yaml"
   ```

   In an externally proxied environment, export the proxy values the
   Environment declares (`HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY`) into your shell,
   and create the `proxy-credentials` secret in
   [common-steps.md](common-steps.md) first if the proxy requires
   authentication.

5. Follow [common-steps.md](common-steps.md) to create secrets, set up bastion
   dependencies, provision infra, install, verify, and destroy the cluster.

To apply every fixture from the repository root, then tear it down before the
next one starts:

```bash
set -euo pipefail
for case_dir in test/e2e/[0-9]*; do
  case_name=$(basename "$case_dir")

  make e2e CASE="$case_name"
  bin/bootwright context init --name "$case_name" -f "$case_dir" --yes
  bin/bootwright destroy --stage clusters --yes
  bin/bootwright destroy --stage infra --yes
  make clean-e2e-state CASE="$case_name"
done
```

If cases are copied and edited under another parent directory, pass that parent
as `E2E_DIR=<path>` to the `make` targets.

Reviewable generated output lives under
`/var/lib/bootwright/contexts/<context>/rendered/`, cluster runtime output
under `clusters/<cluster>/runtime/`, managed service files under
`managed-services/`, provider state under `provider-state/`, and apply ledgers
under `runs/`. Failed phases print the relevant log path.
