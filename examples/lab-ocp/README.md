# Local OpenShift lab

This example describes one development machine that is at the same time the
Bootwright bastion, the host of every managed infrastructure service and the
libvirt hypervisor of a single-node OpenShift cluster. It follows the
[Bootwright API](../../specs/api.md) and is meant to be imported into a context
on that machine so the implemented setup journeys can be exercised end to end:
context creation, Secret custody, `bastion setup` and `preflight bastion`.

Everything a lifecycle command would later create runs on the bastion:

| Object | Role on the bastion |
| --- | --- |
| [`bastion`](infra/machines/bastion.yaml) | The provided local Machine selected by `Environment.spec.controller`; it declares `container-runtime` and `libvirt`. |
| [`lab-proxy`](infra/components/proxy.yaml) | Managed Squid container; the cluster installation egresses through it. |
| [`lab-dns`](infra/components/dns.yaml) | Managed dnsmasq container selected by the lab network. |
| [`lab-ntp`](infra/components/ntp.yaml) | Managed chrony container selected by the cluster installation. |
| [`lab-artifacts`](infra/components/artifact-server.yaml) | Managed artifact server that serves the agent ISO over HTTPS. |
| [`lab-libvirt`](infra/providers/lab-libvirt.yaml) | libvirt substrate on the bastion with sushy-tools emulating a Redfish BMC for every guest. |
| [`sno-master-01`](infra/machines/sno/master-01.yaml) | The one libvirt guest; a downstream-installer Machine whose OS the agent installer supplies. |
| [`sno`](clusters/sno/cluster.yaml) | Single-node OpenShift on that guest, `platform: none`, endpoints resolved from the node address. |

The bastion itself uses direct egress. Setup refuses a bastion whose own
route depends on a managed Proxy, because that Proxy does not exist until a
later apply creates it. The managed Proxy is a consumer choice of the cluster
installation only.

## Lab conventions

All addresses use the documentation range `192.0.2.0/24` and the domain
`lab.example.test`, so nothing in this directory identifies a real machine.
Adapt these values to the machine before importing them:

- `192.0.2.1` is the bastion address on the libvirt bridge `virbr-lab`, and
  every managed service binds it. Define that libvirt network without its own
  DNS and DHCP so port `53` on the bridge address stays free for `lab-dns`.
- `192.0.2.11` is the static install address of the guest on its `enp1s0`
  interface; API and ingress endpoints resolve to it because the cluster
  selects `source.type: node`.
- The guest profile requests 8 vCPUs, 16 GiB of memory, a 120 GiB root disk
  and an emulated TPM, the single-node OpenShift minimum.
- The Redfish emulator listens on ports `8000` and `8001` of the bastion
  address; the artifact server uses `8443` and `8080`; Squid uses `3128`.
- The release is `4.21.33`. Setup resolves the matching `openshift-install`,
  `oc` and `kubectl` from the publisher, so the version must exist.

The four [Secret descriptors](secret-descriptors/) declare context custody:
the pull secret must be set explicitly, while the cluster SSH key, the
emulator credentials and the artifact-server TLS certificate are generated.
No secret bytes live in this directory.

## Prepare the bastion

Run these on the development machine from the repository root. Bootwright
requests sudo authorization for context and setup commands.

Setup resolves publisher metadata over the network before `lab-dns` exists, so
this host needs working name resolution of its own first. Pointing the host
resolver at the lab DNS address, for example as a systemd-resolved global
server for the lab domain, makes every publisher lookup wait on a service that
a later apply has not created yet; setup then refuses and names the host it
could not resolve. Leave the host resolver on a working upstream.

```sh
make build
./bin/bootwright validate -f examples/lab-ocp
./bin/bootwright context init --name lab-ocp --input-dir "$PWD/examples/lab-ocp"
./bin/bootwright secret set --name openshift-pull-secret --value-file ~/pull-secret.json
./bin/bootwright secret generate
./bin/bootwright secret check
./bin/bootwright render effective
./bin/bootwright bastion setup --context lab-ocp --dry-run
./bin/bootwright bastion setup --context lab-ocp
./bin/bootwright preflight bastion --context lab-ocp
```

Setup resolves the latest Python, Ansible, Podman, OpenSSH, NMState and
libvirt client builds plus Helm, and the OpenShift clients and installer for
release `4.21.33`, presents the frozen plan, and installs after confirmation.
A repeated setup with unchanged intent reports `unchanged`. Preflight verifies
the retained bundle, the native runtime, the target clients and the
controller binding of this context to this host without installing anything.

## Lifecycle availability

`plan`, `apply` and `destroy` are available, but this Environment is outside
the shape [M1e](../../specs/milestones.md#m1e--lifecycle-engine-and-managed-artifact-serving)
supports: it declares managed Proxy, DNS and NTP services, a libvirt guest and
a container cluster, whose capabilities arrive with M1f, M2a and M4. An apply
therefore refuses before registering an operation, naming every object it
cannot realize, and changes nothing. Use [`examples/lab-artifacts`](../lab-artifacts)
to exercise the lifecycle engine today.

The setup journeys above are unaffected: this example remains the input for
context creation, Secret custody, `bastion setup` and `preflight bastion`.
