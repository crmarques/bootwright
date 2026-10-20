# Single-node OpenShift on emulated bare metal

Install one OpenShift cluster on one libvirt guest through the agent installer.
The guest is installer-provisioned: `os.provided: false` without an install
profile lets the cluster installer write its disk. The current supported shape
is connected OpenShift through direct access on virtual nodes; physical nodes,
installation proxies, FIPS and disk encryption still refuse. This is the
[B61 rehearsal](../../../specs/milestones/m3.md#b61); its operator acceptance is
still open. See the [cluster contract](../../../specs/container-clusters.md)
for the current implementation limits.

| Input | Purpose |
| --- | --- |
| [Environment](environment.yaml), [controller](infra/machines/controller.yaml) | Select the existing local controller with `container-runtime` and `libvirt` capabilities. |
| [Provider](infra/providers/lab-libvirt.yaml), [network](infra/networkconfigs/lab-guests.yaml), [node](infra/machines/sno-01.yaml) | Create an 8-vCPU, 16-GiB RAM, 120-GiB disk guest, a NAT network and a loopback HTTPS emulated BMC. |
| [Cluster](clusters/sno/cluster.yaml) | Pin the OpenShift release, bind the master and select its media server, NTP and Secrets. |
| [Services](infra/components/) | Managed DNS, NTP, artifact serving and a proxy; this cluster uses direct access. |
| [Secret descriptors](secret-descriptors/) | One supplied pull secret and four generated credentials, keys and certificates. |

## What a single-node cluster does differently

`api`, `api-int` and `*.apps` resolve to the single node. `source.type: node`
declares that choice; the omitted `api-int` slot inherits `api`. The installer
input uses `platform: none`, as required for one master and no workers.

## Lab conventions

All addresses and domains are synthetic. Use a Fedora, RHEL or derivative
amd64 controller with hardware virtualization, sufficient spare capacity for
the guest, root/sudo access and outbound access to host repositories, locked
Python wheels, release downloads and OpenShift registries. The guest needs
outbound access through the managed NAT network. Setup's exact host admission
is in [supported hosts](../../../specs/controller.md#supported-hosts).

Copy the input outside Git from the repository root, retaining this shell:

```sh
lab_sno_input=$(mktemp -d /tmp/bootwright-lab-sno.XXXXXX)
cp -R examples/container-clusters/lab-sno/. "$lab_sno_input/"
```

Customize the copy before creating its context:

- Replace controller `192.0.2.1` with an existing local address in the
  controller, service bind addresses and artifact certificate IP SANs; adjust
  its FQDN and certificate DNS SANs together.
- Replace guest `198.51.100.0/24`, gateway `198.51.100.1`, node
  `198.51.100.21/24` and `virbr-lab` together. Select an unused subnet and
  bridge; Bootwright creates them. Keep node `interface`, NMState interface
  and route interface consistent, and retain the VM's `/dev/vda` root hint.
- Set reachable DNS forwarders and NTP upstreams in the service files.
  Change `lab.example.test` consistently in Environment, Machine names and
  network search domains. Review the VM profile and exact release pin.
- Leave the BMC bound to `127.0.0.1`. Reserve TCP ports `3128`, `53`, `8443`
  and `8080`, UDP ports `53` and `123` on the controller service address,
  plus TCP `8000` on loopback. Allow source-scoped guest access as described
  in [network exposure](../../../docs/operator-guide.md#network-exposure).

## The pull secret

Download an OpenShift pull-secret JSON file from your provider into a private
path outside the input tree. The commands below use `~/pull-secret.json`.
`secret set` requires an invoking-user-owned file with mode `0600` or `0400`;
`chmod 600` fixes an ordinary browser download. No Secret values belong in
YAML. Generate the other four Secrets after finalizing certificate SANs.

## Run it

Run from the repository root, one line at a time. Bootwright elevates the
commands that need host access; use the same invoking account throughout and
an unused context name. `setup` prepares host packages and the locked runtime
without reading this graph. The controller stage installs the selected
OpenShift tools and hypervisor prerequisites.

```sh
make build
./bin/bootwright version
./bin/bootwright validate -f "$lab_sno_input"
./bin/bootwright setup --dry-run
./bin/bootwright setup --yes
./bin/bootwright context init --name lab-sno --input-dir "$lab_sno_input"
chmod 600 ~/pull-secret.json
./bin/bootwright secret set --context lab-sno --name openshift-pull-secret --value-file ~/pull-secret.json
./bin/bootwright secret generate --context lab-sno
./bin/bootwright secret check --context lab-sno
./bin/bootwright validate --context lab-sno
./bin/bootwright render effective --context lab-sno
./bin/bootwright plan --context lab-sno
./bin/bootwright apply --context lab-sno --stage controller --yes
./bin/bootwright preflight controller --context lab-sno
./bin/bootwright apply --context lab-sno --stage infra-components --yes
```

No `media add` is needed: the cluster-media block builds the agent ISO with
the exact release's installer and privately publishes it through the artifact
server. There is no MachineImage or Bootwright OS installation in this graph.

The controller must resolve the cluster zone through the now-running managed
DNS server. Configure your host resolver's conditional route before continuing.
For a host already using systemd-resolved, this is one option; substitute your
customized service address and domain, and preserve any existing configuration:

```sh
sudo install -d -m 0755 /etc/systemd/resolved.conf.d
sudo tee /etc/systemd/resolved.conf.d/bootwright-lab-sno.conf >/dev/null <<'RESOLVED'
[Resolve]
DNS=192.0.2.1
Domains=~lab.example.test
RESOLVED
sudo systemctl restart systemd-resolved
./bin/bootwright preflight container-cluster --context lab-sno
```

Each name must answer only with the node's customized IP. Bootwright proves
this resolution before booting. Continue with the remaining stages:

```sh
./bin/bootwright plan --context lab-sno
./bin/bootwright apply --context lab-sno --yes
```

Allow substantial time for image creation and installation. Progress names a
private operation-log directory before effects start. A successful repeated
apply settles without reinstalling. A release or topology edit in the source
copy does not update imported input; use the recovery procedure below.

## Inspect and recover

After apply returns, inspect its durable result and the declared cluster:

```sh
./bin/bootwright status --context lab-sno
./bin/bootwright machine list --context lab-sno --power-status
./bin/bootwright cluster list --context lab-sno
./bin/bootwright cluster info --context lab-sno --name sno
./bin/bootwright diff --context lab-sno --observe
```

After installation is proved complete, export the custodied administrator
kubeconfig to a private file outside the input tree:

```sh
umask 077
lab_sno_kubeconfig=$(mktemp /tmp/bootwright-sno-kubeconfig.XXXXXX)
./bin/bootwright cluster kubeconfig --context lab-sno --name sno > "$lab_sno_kubeconfig"
```

The export writes raw credential bytes; protect the file and use an
independently installed matching `oc` client to inspect the cluster. Bootwright's
`cluster oc`, `cluster kubectl` and OpenShift SSH sessions remain unavailable.

On interruption, the node may keep installing. Let the command finish exiting,
then use `status`, `plan` and the named logs to diagnose the result. Correct an
external prerequisite, such as DNS or reachability, and repeat `apply` with the
same context, input and build to continue; an unknown effect is observed before
retrying. A live operation can hold the store lease, so concurrent inspection
may refuse. See [build changes and continuation](../../../docs/operator-guide.md#changing-the-build-between-runs)
when a repair needs another executable.

To change imported declarations, stop the guest and complete destroy as below,
keeping the context. Edit the external copy and import it for a fresh apply:

```sh
./bin/bootwright context update --name lab-sno --input-dir "$lab_sno_input" --yes
./bin/bootwright validate --context lab-sno
./bin/bootwright secret check --context lab-sno
./bin/bootwright plan --context lab-sno
```

If a generated certificate descriptor changed, renew that named Secret after
destroy with `secret generate --context lab-sno --name artifact-server-tls --renew`
(substitute its name). Run `secret check` again, then repeat the staged apply
above, checking DNS after services restart. Continuation and destroy need the
Secret versions the operation bound; rotation does not repair a lost binding.

## Cleanup

This removes the VM and its cluster disks. Stop its guest before destroy;
`--authorize data-loss` acknowledges erasure, independently of confirmation.
Destroy also withdraws private media, the installer work area, managed services
and the network. It retains the prepared controller runtime and tool cache.

```sh
./bin/bootwright machine stop --context lab-sno --name sno-01 --yes
./bin/bootwright destroy --context lab-sno --yes --authorize data-loss
./bin/bootwright status --context lab-sno
./bin/bootwright context delete --name lab-sno --purge --yes
```

For a partial apply, stop any guest that exists and follow `status` and destroy's
remediation if absence or quiescence cannot be proved; repeat destroy to continue
a partial removal. Delete the context only after removal completes. Context
deletion is local bookkeeping and does not remove guests or services.

Remove the resolver drop-in created above and restart systemd-resolved, or
restore your host's prior conditional DNS route. Remove the exported kubeconfig
and the temporary input copy when no longer needed. The installer work area
and its initial administrator password are removed by destroy; the context
releases its custodied kubeconfig only when destroy completes.
