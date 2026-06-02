<p align="center">
  <img src="images/logo-circle.png" alt="Bootwright" width="400">
</p>

# Bootwright

Bootwright is a desired-state orchestrator for provisioning fleets of OpenShift
and OKD clusters from bare hardware or virtualized substrates. You describe the
environment, providers, shared components, infrastructure, networks, clusters,
storage clusters, and bootstrap add-ons with declarative YAML kinds. Bootwright validates
that intent, renders the deterministic input files expected by installer and
provider CLIs, and converges the workflow idempotently.

**Supported distributions:** OpenShift and OKD.

## The problem it solves

Standing up one cluster is a runbook. Standing up *many* clusters — across mixed substrates, with shared services like load balancers, DNS, mirror registries, and proxies — is a coordination problem that handwritten scripts and ad-hoc installer runs cannot keep consistent. Bootwright replaces that with a few versioned objects and an idempotent apply pipeline: declare the fleet once, converge it as many times as you need, and get the same result every time.

The CLI covers the provisioning pipeline:

```text
bootwright example init lab --output ./lab-input
bootwright validate -f ./lab-input
bootwright context init lab -f ./lab-input
bootwright context update lab -f ./lab-input
bootwright context validate
bootwright secret set openshift-pull-secret --pull-secret ~/openshift-pull-secret.json
bootwright secret generate
bootwright secret materialize
bootwright apply bastion --yes
bootwright check all
bootwright render effective
bootwright plan
bootwright apply all --yes
bootwright status --watch
bootwright cluster access-info
```

`apply all` is the normal convergence path. Phase commands such as
`apply infra`, `apply storage-cluster`, `apply clusters`, and `apply addons`
remain available for advanced operations and recovery when you need one slice
of the graph.

<p align="center">
  <img src="images/high-level-overview.png" alt="Bootwright overview" width="800">
</p>

## Start Here

The user-facing docs are published from [`docs/`](docs/) as this [link](https://crmarques.github.io/bootwright/). Browse them in-tree or once Pages is enabled on the repo:

| Audience | Start |
| --- | --- |
| Newcomers | [Home](docs/index.md) and [Concepts](docs/concepts.md) |
| First context and apply flow | [Getting Started](docs/getting-started.md) |
| Authoring real environments | [Advanced](docs/advanced/) — providers, networking, proxy/disconnected, secrets |
| Contributors and coding agents | [Specs](specs/index.md) |
| Architecture decisions | [ADRs](specs/adr/README.md) |

Docs explain the workflow. Specs are the source of truth for API
shape, architecture boundaries, validation rules, CLI behavior, and
security posture.

Canonical desired-state examples live under [`examples/`](examples/). E2E
fixtures under [`test/e2e/`](test/e2e/) are runnable test assets.

## Install The CLI

Download the `bootwright` binary for your platform from
[GitHub Releases](https://github.com/crmarques/bootwright/releases), then
install it on your `PATH`:

```bash
chmod +x bootwright
sudo install -m 0755 bootwright /usr/local/bin/bootwright
bootwright version
```

If a release binary is not available yet, build the CLI from the repository
root with the [`Containerfile`](Containerfile) and copy the binary out of the
image:

```bash
export HTTP_PROXY
export HTTPS_PROXY="${HTTP_PROXY}"
export NO_PROXY="localhost,127.0.0.1,.local,10."
export http_proxy="${HTTP_PROXY}"
export https_proxy="${HTTPS_PROXY}"
export no_proxy="${NO_PROXY}"

DOCKER_BUILDKIT=1 docker build \
  --build-arg "HTTP_PROXY=${HTTP_PROXY}" \
  --build-arg "HTTPS_PROXY=${HTTPS_PROXY}" \
  --build-arg "NO_PROXY=${NO_PROXY}" \
  --build-arg "http_proxy=${http_proxy}" \
  --build-arg "https_proxy=${https_proxy}" \
  --build-arg "no_proxy=${no_proxy}" \
  -t bootwright \
  -f Containerfile \
  .

docker create --name bootwright-bin bootwright
docker cp bootwright-bin:/usr/local/bin/bootwright ./bootwright
docker rm bootwright-bin
sudo install -m 0755 ./bootwright /usr/local/bin/bootwright
bootwright version
```

## Desired-State Contract

User-authored YAML uses `apiVersion: bootwright.io/v1alpha1` and sixteen kinds:

| Kind | Owns |
| --- | --- |
| `Environment` | Shared environment defaults: selected resource files or directories, cluster selection, base domain, secret sources, service access catalog, proxy selection, registry defaults, and component image pins |
| `Host` | Neutral named addresses, SSH endpoint selection, and generic capability tags (`libvirt`, `container-runtime`); referenced by providers and infra components |
| `InfraProvider` | Named provider capability lists — `machineProfiles` and explicit `machines` — with names scoped per kind |
| `InfraComponent` | Host-bound shared infra services such as artifact servers, load balancers, proxies, name resolution, and registries |
| `NetworkConfig` | Installer `machineNetwork[]` plus reusable NMState host templates for agent installs |
| `ClusterInfra` | One cluster's wiring: platform render mode, endpoints, and selected machines under `components.machines[]` |
| `ContainerCluster` | Provider-neutral OpenShift or OKD intent: distribution, release, install mode, cluster networking, pools, and node-to-machine binding |
| `StorageCluster` | External storage cluster provisioning intent; the first implementation drives Ceph through cephadm |
| `StoragePlacementPolicy` | Storage placement policy such as the CRUSH rule and replicated pool defaults used by Ceph pools |
| `StoragePool` | Ceph pool desired state, role, placement policy, and replication settings |
| `StorageFilesystem` | CephFS desired state, including distinct metadata and data pools plus MDS placement |
| `StorageObjectGateway` | RGW desired state, public endpoint, and cephadm ingress VIP placement |
| `StorageExport` | Exported storage surface prepared for downstream consumers such as Data Foundation external mode |
| `ClusterAddon` | A reusable post-install component applied inside an installed OpenShift or OKD cluster |
| `ClusterAddonProfile` | An ordered reusable group of add-ons and nested profiles |
| `ClusterAddonBinding` | One installed cluster's post-install bootstrap set: add-ons, profiles, and binding-scoped add-on inputs |

`ContainerCluster` stays provider-neutral. Swapping from libvirt with
Redfish emulation to real bare metal edits the substrate-owned objects:
`InfraProvider`, `InfraComponent`, `NetworkConfig`, and the cluster
infrastructure machine bindings.
Post-install components intentionally stay outside
`ContainerCluster.spec.install`; they are separate desired-state resources
selected by `Environment`, bound to clusters, and applied after cluster
installation.
External storage provisioning is also separate from `ContainerCluster`.
`StorageCluster` uses the same lower-layer `ClusterInfra` and `InfraProvider`
objects for machine facts, while storage-export attachments are declared as
add-on input effects and wait for both the storage cluster and a `ClusterAddon`
that provides `data-foundation`.

Current `apply` support is explicit: libvirt with emulated Redfish BMCs,
bare metal with Redfish virtual media, and KubeVirt VMs hosted by OpenShift
Virtualization are converged by the shipped Ansible workflows. vSphere remains
a schema path until its provider role is converged; IPMI is not apply-supported
today.

## CLI

```text
bootwright example init lab --output ./lab-input
bootwright validate -f ./lab-input
bootwright context init lab -f ./lab-input
bootwright context update lab -f ./lab-input
bootwright context current
bootwright context validate
bootwright context validate --output json
bootwright cluster list
bootwright cluster access-info
bootwright cluster access-info --cluster demo-ocp
bootwright secret list
bootwright secret set openshift-pull-secret --pull-secret ~/openshift-pull-secret.json
bootwright secret generate
bootwright secret materialize
bootwright secret list
bootwright print-env [--sensitive]
bootwright check syntax
bootwright apply bastion --yes
bootwright check all
bootwright render effective
bootwright plan
bootwright apply all --yes
bootwright status --watch
bootwright cluster access-info
bootwright check all --dry-run
bootwright apply infra --dry-run
bootwright apply infra --yes
bootwright render installer --scope demo-ocp
bootwright render storage --scope ceph-stretch
bootwright render --output-dir ./rendered --scope demo-ocp --sensitive
bootwright apply storage-cluster --scope ceph-stretch --yes
bootwright apply clusters --yes
bootwright check addons
bootwright apply addons --dry-run
bootwright apply addons --yes
bootwright status
bootwright destroy container-cluster --yes
bootwright destroy infra --yes
bootwright destroy infra --scope artifact-server --yes
```

The CLI is organized around workflow command groups. Provisioning targets are
`bastion`, `infra`, `clusters`, `container-cluster`, `storage-cluster`,
`addons`, and `all`. Top-level groups are `validate`, `context`, `cluster`,
`example`, `print-env`, `secret`, `check`, `status`, `plan`, `render`,
`apply`, `destroy`, and `version`. The formal CLI contract lives in
[specs/state-model.md](specs/state-model.md#cli-contract).

Human text output is designed for operators and may evolve. Use
`--output json` where available for automation. `bootwright print-env`
intentionally prints raw shell exports. `bootwright cluster access-info` prints
URLs, local kubeconfig paths, and kubeadmin password retrieval commands, but
never prints kubeconfig or password bytes. Apply runs keep native Ansible, `oc`,
SSH, SCP, Ceph, and installer process output in run, task, and cluster logs
while the terminal shows a ledger-backed fleet dashboard with log paths,
phase status, running work, and concise failures. `bootwright apply all` is the
normal end-to-end workflow.
`bootwright apply clusters` provisions selected cluster infrastructure,
storage clusters, OpenShift or OKD clusters, bound add-ons, and declared
storage integrations as dependency-ready tasks. `bootwright apply
container-cluster` remains available for focused OpenShift install recovery,
and `bootwright apply addons` is available for standalone add-on convergence
after install. Use phase commands for scoped maintenance or recovery.

`bootwright render --output-dir ./rendered --scope <cluster> --sensitive`
exports concrete external CLI inputs, including
`openshift-install/<cluster>/{install,agent}-config.yaml`, for operators who
want to run supplier or community tools such as `openshift-install` themselves.
Treat that output as local runtime material because it contains secrets.
`bootwright render effective` writes the normalized desired state with defaults
applied to the current context rendered directory for inspection before apply.
`bootwright plan` previews the full apply task graph without mutating remote
systems.

## Repository Layout

```text
specs/      Source-of-truth definitions for humans and agents
docs/       Human workflow documentation
examples/   Safe-to-commit desired-state examples
.agents/    Project-local coding-agent skills
api/        Versioned desired-state types
cmd/        CLI entrypoints
internal/   Private implementation packages
ansible/    Embedded `bootwright.core` Ansible collection and dependency lock
test/       Test fixtures and end-to-end cases
```

Versioned content (specs, docs, fixtures) must stay safe to commit: no
kubeconfigs, pull secrets, private keys, tokens, plaintext credentials,
personal usernames, or private absolute paths.
