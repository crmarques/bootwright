---
title: Getting Started
description: Import, validate, and converge a Bootwright context.
---

# Getting Started

Bootwright runs from a named context on the bastion host where you invoke the
CLI. The context points at the desired-state YAML you edited for your
environment and stores local state, generated runtime files, and secret
material outside the repo under `/var/lib/bootwright`.

## 0. Install The CLI

Download the `bootwright` binary for your platform from
[GitHub Releases](https://github.com/crmarques/bootwright/releases), then
install it on your `PATH`:

```bash
chmod +x bootwright
sudo install -m 0755 bootwright /usr/local/bin/bootwright
bootwright version
```

Configure Bash completion for your user shell:

```bash
mkdir -p "${HOME}/.local/share/bash-completion/completions"
bootwright completion bash > "${HOME}/.local/share/bash-completion/completions/bootwright"
source "${HOME}/.local/share/bash-completion/completions/bootwright"
```

## 1. Prepare Input Files

Start from a generated example or an in-repo example copy, then edit the
working directory for your environment:

```text
bootwright example init my-sno-lab --output ./my-sno-lab
```

When working from a fresh checkout, copying the canonical example is
equivalent:

```text
cp -a examples/sno-libvirt-redfish ./my-sno-lab
ls -l ./my-sno-lab
```

Canonical input examples live under
[`examples/`](https://github.com/crmarques/bootwright/tree/main/examples).
Use `sno-libvirt-redfish` for the smallest single-node lab with emulated
Redfish BMCs. Use `sno-baremetal-redfish` for real bare-metal hosts with
Redfish virtual media, or `baremetal-redfish-fleet` for a two-cluster
bare-metal input layout. `baremetal-redfish-addons` adds declarative
OpenShift Virtualization bootstrap resources.
`baremetal-redfish-multidc-virtualized-odf-ceph` is the full reference layout
with two parent clusters, two KubeVirt child clusters, and stretched Ceph with
Data Foundation.

The copied directory contains desired-state files for the relevant kinds.
Starter inputs may keep shared objects under `shared/*.yaml` and one cluster
under `clusters/<cluster>/`. Fleet inputs usually split shared infrastructure
under `infra/` and cluster intent under `clusters/container/<cluster>/`:

```text
environment.yaml                         Environment
service-machine.yaml or shared/service-machines.yaml or infra/machines/*.yaml
provider.yaml or shared/provider.yaml or infra/providers/*.yaml
infra-component.yaml or shared/components.yaml or infra/components/*.yaml
networkconfig.yaml or shared/networks.yaml or infra/networkconfigs/*.yaml
cluster-machines.yaml or clusters/<cluster>/cluster-machines.yaml or clusters/container/<cluster>/cluster-machines.yaml
cluster.yaml or clusters/<cluster>/cluster.yaml or clusters/container/<cluster>/cluster.yaml
add-ons/*.yaml                           optional ClusterAddon resources
clusters/storage/<cluster>/*.yaml        optional storage resources
```

Edit these first:

- `Environment.spec.baseDomain` and `Environment.spec.secrets`.
- `Environment.spec.containerClusters[]` if the environment should select only
  part of the loaded fleet.
- `Environment.spec.infraComponents.*` and `proxyFor` when the lab uses
  external or managed proxy, DNS, artifact, registry, or NTP services.
- `Machine.spec.addresses[]` and `Machine.spec.access.ssh` references for
  provider/service hosts.
- Physical MACs, BMC addresses, or virtual machine profile refs in
  `cluster-machines.yaml`, `clusters/<cluster>/cluster-machines.yaml`, or
  `clusters/container/<cluster>/cluster-machines.yaml`.
- Machine CIDRs and NMState templates in `networkconfig.yaml`,
  `shared/networks.yaml`, or `infra/networkconfigs/*.yaml`.
- Endpoint definitions and per-machine IP overrides in
  `cluster-machines.yaml`, `clusters/<cluster>/cluster-machines.yaml`, or
  `clusters/container/<cluster>/cluster-machines.yaml`.
- OpenShift or OKD release, install mode, and node bindings in
  `cluster.yaml`, `clusters/<cluster>/cluster.yaml`, or
  `clusters/container/<cluster>/cluster.yaml`.

Provider swaps should leave `Environment` and `ContainerCluster` unchanged
unless the cluster intent itself changes.

Before importing a context, confirm the out-of-band inputs exist:

- The bastion host can use `sudo` for `/var/lib/bootwright`.
- SSH from the bastion reaches every `Machine` address used by provider or
  service actions.
- The OpenShift pull secret is available outside the repo.
- Generated or supplied BMC, proxy, and mirror secrets are planned in
  `Environment.spec.secrets`.
- Provider host tooling and permissions are available for the selected
  substrate.
- BMCs can reach the artifact endpoint selected for Redfish virtual media.
- DNS, VIPs, and load balancer addresses are reachable from the bastion and
  the cluster nodes.
- Mirror CA files and other trust bundles are local, unversioned files.
- Real BMC TLS posture is understood before using
  `disableCertificateVerification: true`.

Validate the edited YAML before it is imported into `/var/lib/bootwright`:

```text
bootwright validate -f ./my-sno-lab
```

## 2. Verify SSH Inputs

Bootwright uses SSH to reach provider and service hosts. Before importing the
context, confirm the declared key, user, and Machine address values are correct.
For example, an operator can test SSH auth manually:

```text
ssh -i "${HOME}/.ssh/bootwright-ssh-key" -o StrictHostKeyChecking=accept-new "${USER}@${HOST_ADDRESS}" true
```

Use the exact address you declare in `Machine.spec.addresses[]` for each
provider or service host. This manual check updates the user's SSH trust
state, not Bootwright's managed context trust.

## 3. Import A Context

Create the context from the edited directory:

```text
bootwright validate -f ./my-sno-lab
bootwright context init lab -f ./my-sno-lab
bootwright context update lab -f ./my-sno-lab --yes
bootwright context current
bootwright context validate
bootwright secret list
```

Bootwright records only your current context selection in
`~/.bootwright/contexts.yaml`. Context data is shared under
`/var/lib/bootwright/contexts/<context-name>/`, and the imported authoring copy
lives at `input/` inside that directory. Other sudo-capable administrators on
the same host see the same shared contexts, but keep their own current
selection. Run `bootwright ...` as your user; if you run
`sudo bootwright ...` directly, Bootwright uses root's private current-context
selection instead.

Re-run `context init` with `--yes` to replace the entire context directory, or
use `bootwright context update lab -f <input-dir> --yes` to replace only
`input/` while preserving secrets, rendered output, runtime data, run history,
and managed host/service files. Omit `--yes` when you want Bootwright to ask
for confirmation before replacing imported inputs.

## 4. Set Secrets

Desired-state YAML names secrets; secret bytes stay in the local context
secrets directory. Run the commands that match the names declared in
`Environment.spec.secrets`:

```text
bootwright secret set openshift-pull-secret --pull-secret "${HOME}/openshift-pull-secret.json"
bootwright secret generate
bootwright secret materialize
bootwright secret encryption status
bootwright host trust
bootwright secret list
bootwright context validate
```

Get the OpenShift pull secret from
`https://console.redhat.com/openshift/install/pull-secret`. Prefer
`--password-stdin` for credentials on shared shells; `--password` is useful
when credentials already come from protected environment variables.
Context-local secrets are encrypted at rest automatically; `secret encryption
status` shows the root-owned-file keyring and whether any old plaintext files
still need `secret encryption migrate --yes`.

For non-generated credentials declared by your desired state, use `secret set`
with a protected input source:

```text
printf '%s\n' "${BMC_PASS}" | bootwright secret set bmc-credentials --username "${BMC_USER}" --password-stdin
printf '%s\n' "${PROXY_PASS}" | bootwright secret set proxy-credentials --username "${PROXY_USER}" --password-stdin
```

## 5. Export Runtime Environment

```text
eval "$(bootwright print-env --sensitive)"
```

`print-env` exports `BOOTWRIGHT_CONTEXT` and any configured proxy environment.
`--sensitive` is required when proxy credentials would be printed.

## 6. Check And Apply

```text
bootwright bastion setup --yes
bootwright check all
bootwright render effective
bootwright plan
bootwright apply --yes
bootwright status --watch
bootwright cluster access-info
```

`bastion setup` installs bastion-host prerequisites. `check all` validates the
full graph before convergence. `render effective` writes
`effective-state.yaml` with defaults applied so you can inspect the normalized
state before applying it. `plan` previews the full apply task graph without
mutating provider hosts, nodes, storage clusters, or managed clusters.

`apply --yes` is the normal end-to-end convergence path. It includes
infrastructure, managed storage, OpenShift or OKD cluster install, and bound
post-install add-ons. Storage-export input effects wait for both the storage
task and a bound add-on with `provides: [data-foundation]`.
For KubeVirt child clusters, the full graph also waits for the parent cluster
install and its `provides: [kubevirt]` add-on before creating child VM
infrastructure. Focused child applies require either an already installed and
KubeVirt-ready parent, or both parent and child named in `--clusters`.
Use `apply --stage infra` to prepare providers, infra services, selected
machines, and managed storage-node prerequisites. Use `apply --stage clusters`
to install selected container clusters, provision selected storage clusters,
apply bound add-ons, and attach declared integrations. `--clusters` accepts a
comma-separated mix of `ContainerCluster` and `StorageCluster` names. Running
`apply --stage clusters --yes` again skips cluster install tasks when the prior install
record, rendered desired-input fingerprint, and kubeconfig availability probe
all match, then applies add-ons and integrations idempotently. If an interrupted
apply already booted nodes, the next apply resumes at the install wait phase
instead of recreating the ISO or rebooting machines.

Apply terminal output shows a fleet dashboard with log paths, phase status,
running work, and concise failures. Native Ansible, `oc`, SSH, SCP, Ceph, and
installer process output is kept under the run, task, and cluster logs in
Bootwright storage.

`destroy` uses the same stage selector shape as `apply`. Use
`destroy --stage infra` to tear down current-context infrastructure; omit
`--clusters` when you want Bootwright to sweep every context-owned VM that a
provider adapter can identify. Use `destroy --stage infra --clusters <name>`
for focused infrastructure teardown. Use `destroy --stage clusters` to remove
OpenShift cluster install runtime state without removing VMs or provider
infrastructure.

Use `bootwright status --watch` while an apply is running. A new apply is
blocked while the previous apply ledger has a fresh process lease. If an
interrupted process leaves only a stale ledger, the next `apply` or `destroy`
marks it cancelled before continuing.

To protect an environment from accidental teardown, set
`Environment.spec.safety.destroyProtection: requiredOverride`. Protected
destroy commands fail unless that same command includes `--override`.
`--yes` only skips the confirmation prompt; it does not authorize protected
destroy. Dry-run destroy output reports the protection without mutating state.

Stable JSON output is intentionally limited. Use these forms for automation:

| Command | JSON support | Behavior |
| --- | --- | --- |
| `bootwright context validate --output json` | Supported | Context structure and declared secret material checks |
| `bootwright validate -f <input-dir> --output json` | Supported | Pre-import diagnostics |
| `bootwright check syntax -f <input-dir> --output json` | Supported | Pre-import diagnostics |
| `bootwright check syntax --output json` | Supported | Read-only diagnostics |
| `bootwright check infra --dry-run --output json` | Supported | Dry-run preflight plan |
| `bootwright check clusters --dry-run --output json` | Supported | Dry-run preflight plan |
| `bootwright render effective --output json` | Supported | Writes normalized desired state |
| `bootwright render installer --output json` | Supported | Writes context render output |
| `bootwright cluster list --output json` | Supported | Read-only cluster access status |
| `bootwright cluster access-info --output json` | Supported | Read-only cluster access inventory |
| `bootwright secret list --output json` | Supported | Read-only secret status |
| `bootwright status --output json` | Supported | Read-only context status |
| `bootwright plan --output json` | Supported | Dry-run apply plan |
| `bootwright apply --stage infra --dry-run --output json` | Supported | Dry-run apply plan |
| `bootwright apply --stage clusters --dry-run --output json` | Supported | Dry-run apply plan |
| `bootwright apply --dry-run --output json` | Supported | Dry-run apply plan |
| `bootwright destroy --stage infra --dry-run --output json` | Supported | Dry-run destroy plan |
| `bootwright destroy --stage clusters --dry-run --output json` | Supported | Dry-run destroy plan |
| `bootwright apply ... --yes` | Not JSON | Mutates selected scope |
| `bootwright destroy ... --yes` | Not JSON | Destroys selected scope when not protected |
| `bootwright destroy ... --override --yes` | Not JSON | Destroys protected selected scope |

For mutating automation, run the apply or destroy command with `--yes`, then
poll `bootwright status --output json` or `bootwright status --watch`.

## Export External CLI Inputs

Render the normalized desired state with defaults applied:

```text
bootwright render effective
```

Render placeholder installer files into context state:

```text
bootwright render installer --scope <cluster-name>
```

Render storage tool inputs into context state:

```text
bootwright render storage --scope <storage-cluster-name>
```

To run `openshift-install` or Ansible-facing CLIs yourself, export concrete
tool inputs to a local, unversioned directory:

```text
bootwright render --output-dir ./rendered --scope <cluster-name> --sensitive
openshift-install agent create image --dir ./rendered/openshift-install/<cluster-name>
openshift-install agent wait-for install-complete --dir ./rendered/openshift-install/<cluster-name> --log-level info
```

The OpenShift installer inputs are written under
`./rendered/openshift-install/<cluster-name>/` as `install-config.yaml` and
`agent-config.yaml`, plus optional `openshift/` manifests, with secret material
inlined. Keep that directory local and remove it when you no longer need the
files.
Storage inputs are written under `./rendered/storage/<storage-cluster-name>/`
and include cephadm bootstrap, core service, and late service specs; phased
Ceph operations; and Data Foundation manifests.

## Optional Cleanup

Remove only the generated artifact publication service used for BMC ISO
fetches, including HTTPS listeners when the selected artifact server exposes
them:

```text
bootwright destroy infra --scope artifact-server --yes
```

This does not destroy container-cluster nodes or the rest of the infrastructure.

## Output Boundaries

- Authored YAML lives under
  `/var/lib/bootwright/contexts/<context>/input/`.
- Effective state lives at
  `/var/lib/bootwright/contexts/<context>/rendered/effective-state.yaml`.
- Placeholder installer output lives under
  `/var/lib/bootwright/contexts/<context>/clusters/<cluster>/rendered/installer/`.
- Rendered storage tool inputs live under
  `/var/lib/bootwright/contexts/<context>/rendered/storage/<storageCluster>/`.
- Secret-inlined runtime installer output lives under
  `/var/lib/bootwright/contexts/<context>/clusters/<cluster>/runtime/installer/`.
- Generated cluster access material lives under
  `/var/lib/bootwright/contexts/<context>/clusters/<cluster>/secrets/`.
- `render --output-dir <dir> --sensitive` writes secret-inlined external tool
  inputs under the requested directory; keep it local and unversioned.

## Add The Cluster Kube Context

After `apply --yes` completes, merge the generated admin kubeconfig into your
user kubeconfig:

```text
export CLUSTER=<cluster-name>
export BOOTWRIGHT_CONTEXT="$(bootwright context current --short)"
export SRC_KUBECONFIG="/var/lib/bootwright/contexts/${BOOTWRIGHT_CONTEXT}/clusters/${CLUSTER}/secrets/kubeconfig"
export TMP_KUBECONFIG="${TMPDIR:-/tmp}/bootwright-${CLUSTER}.kubeconfig"
export TMP_MERGED="${TMPDIR:-/tmp}/bootwright-merged-kubeconfig"

mkdir -p "${HOME}/.kube"
touch "${HOME}/.kube/config"
chmod 0600 "${HOME}/.kube/config"

sudo install -m 0600 -o "$(id -u)" -g "$(id -g)" \
  "${SRC_KUBECONFIG}" "${TMP_KUBECONFIG}"
CTX="$(oc --kubeconfig "${TMP_KUBECONFIG}" config current-context)"
oc --kubeconfig "${TMP_KUBECONFIG}" config rename-context "${CTX}" "${CLUSTER}-admin"
KUBECONFIG="${HOME}/.kube/config:${TMP_KUBECONFIG}" oc config view --flatten > "${TMP_MERGED}"
install -m 0600 "${TMP_MERGED}" "${HOME}/.kube/config"
oc config use-context "${CLUSTER}-admin"
```

Use a unique context name per cluster, such as `<cluster-name>-admin`, to avoid
overwriting another installer-generated `admin` context.

The schema reference is in
[`specs/state-model.md`](https://github.com/crmarques/bootwright/blob/main/specs/state-model.md).
