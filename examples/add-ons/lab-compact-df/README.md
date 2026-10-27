# Data Foundation over managed Ceph and compact OpenShift

Install three schedulable masters and no workers on one libvirt host through
OpenShift's agent installer. The installer owns the API and ingress virtual
IPs on the managed NAT network; its derived platform is `baremetal` with
`provisioningNetwork: disabled`. This is the [B66 rehearsal](../../../specs/milestones/m3.md#b66);
operator acceptance remains open. The Data Foundation acceptance gate is
[B761](../../../specs/milestones/m4.md#b761), requiring an accepted compact
cluster and qualified Ceph baseline.

All addresses and names are synthetic. The controller needs hardware
virtualization, root/sudo access, outbound repository and registry access,
at least 34 logical CPUs, 95 GiB available memory and 820 GiB free pool disk,
plus about 7 GiB for five image copies. Each master requests 6 vCPU, 16 GiB
memory and a 120 GiB root disk. Each of three Ceph guests requests 4 vCPU,
13 GiB memory, a 60 GiB root disk and an 80 GiB data disk; the provider reserves another 4 CPUs, 8 GiB
and 40 GiB. See [acceptance profiles](../../../specs/container-cluster-coverage.md#acceptance-profiles).

| Input | Purpose |
| --- | --- |
| [Environment](environment.yaml), [controller](infra/machines/controller.yaml) | Existing local controller with container runtime and libvirt. |
| [Provider](infra/providers/lab-libvirt.yaml), [network](infra/networkconfigs/lab-guests.yaml) | Managed NAT network, compact VM profile and loopback HTTPS emulated BMCs. |
| [Masters](infra/machines/) | Three installer-provisioned guests at consecutive addresses. |
| [Cluster](clusters/compact/cluster.yaml) | Exact release, API and ingress VIPs, master membership and Secrets. |
| [Services](infra/components/), [Secret descriptors](secret-descriptors/) | Managed DNS, NTP, private artifact serving and generated credentials. |

## Prepare a private copy

A context holding a completed lab-sno apply must destroy it with the executable
that created it before importing this expanded topology. Never edit the input
of a live installation. Use an unused context for a fresh rehearsal.

```sh
df_input=$(mktemp -d /tmp/bootwright-lab-compact-df.XXXXXX)
cp -R examples/add-ons/lab-compact-df/. "$df_input/"
```

Customize this copy before initializing its context:

- Review the IBM license terms before use: the tracked `storage/entitlement.yaml`
  sets `license.accept: true` as a placeholder, and accepting the license is
  your explicit act.
- Replace controller `192.0.2.1` in its Machine, service binds and artifact
  certificate SANs together; adjust its FQDN and certificate DNS SANs together.
- Select an unused guest prefix and bridge, replacing `198.51.100.0/24`,
  gateway `198.51.100.1`, bridge `virbr-lab`, master addresses `.21` to `.23`,
  API VIP `.10` and ingress VIP `.11` consistently. Keep VIPs distinct from
  Machines and the bridge, and never assign either VIP to a host interface.
- Keep `enp1s0` consistent in the Machine, NMState and route declarations,
  and retain `/dev/vda` as each guest's root device.
- Set reachable DNS forwarders and NTP upstreams; replace `lab.example.test`
  consistently. Review the exact release and capacity reserve.
- Keep BMC binds on loopback. Reserve TCP `8000`, `8001`, `8002` there,
  TCP `53`, `8443`, `8080`, `3128` and UDP `53`, `123` on the service address.
  Apply the source-scoped [network exposure](../../../docs/operator-guide.md#network-exposure)
  rules for guest access.

Download a pull-secret JSON file into a private path outside the input tree.
The following commands assume `~/pull-secret.json`; Secret values never belong
in these YAML files. Run commands from the repository root with one invoking
account throughout.

## Stage and install

```sh
make build
./bin/bootwright version
./bin/bootwright validate -f "$df_input"
./bin/bootwright setup --dry-run
./bin/bootwright setup --yes
./bin/bootwright context init --name lab-compact-df --input-dir "$df_input"
chmod 600 ~/pull-secret.json
./bin/bootwright secret set --context lab-compact-df --name openshift-pull-secret --value-file ~/pull-secret.json
./bin/bootwright secret set --context lab-compact-df --name ibm-registry --username cp --password-stdin --yes
./bin/bootwright secret set --context lab-compact-df --name rgw-issuer --certificate-file /path/to/rgw-issuer.pem
./bin/bootwright secret set --context lab-compact-df --name rgw-objects-tls --certificate-file /path/to/rgw-server.pem --private-key-file /path/to/rgw-server.key
./bin/bootwright secret generate --context lab-compact-df
./bin/bootwright secret check --context lab-compact-df
./bin/bootwright validate --context lab-compact-df
./bin/bootwright render effective --context lab-compact-df
./bin/bootwright plan --context lab-compact-df
./bin/bootwright apply --context lab-compact-df --stage controller --yes
./bin/bootwright preflight controller --context lab-compact-df
./bin/bootwright apply --context lab-compact-df --stage infra-components --yes
```

Configure the controller's conditional resolver for the managed cluster zone
before continuing. For a host already using systemd-resolved, substitute your
service address and domain in this example and preserve prior configuration:

```sh
sudo install -d -m 0755 /etc/systemd/resolved.conf.d
sudo tee /etc/systemd/resolved.conf.d/bootwright-lab-compact-df.conf >/dev/null <<'RESOLVED'
[Resolve]
DNS=192.0.2.1
Domains=~lab.example.test
RESOLVED
sudo systemctl restart systemd-resolved
./bin/bootwright preflight container-cluster --context lab-compact-df
./bin/bootwright plan --context lab-compact-df
./bin/bootwright apply --context lab-compact-df --yes
```

The agent ISO is built and privately published automatically. Before a fresh
boot, Bootwright probes the managed IPv4 VIPs for ARP replies. Every media
insert has its own private URL, revoked after eject. Boot readiness names a
node that never answers, and waits report their own progress and private logs.
Never copy private URLs or credentials into the acceptance ledger.

## Acceptance and interruption drills

Record the build, exact installed release and duration of each phase in the
private rehearsal record, and submit only sanitized evidence for owner
acceptance. B66 requires these observations on a build containing X116 to X119:

1. Prove the staged fresh apply and `preflight container-cluster` pass.
2. Interrupt after the first node boots, let the command exit, and repeat
   `apply`. Confirm that only nodes still needing boot receive media and power
   actions; the running node continues its installation.
3. Interrupt during the bootstrap wait, continue, then interrupt during the
   installation wait and continue. Repeat with SIGKILL during the installation
   wait; observation must resolve the unknown effect before retrying work.
4. Repeat a completed `apply` and confirm it settles. Export the administrator
   kubeconfig and use an independently installed matching `oc` to read three
   Ready masters and the exact release; prove the API answers at its VIP from
   the controller.
5. On a separate fresh apply, stop one master after boot and let the bootstrap
   wait fail. Confirm the diagnosis names the missing or unready master and
   its status. This drill can spend a full 90-minute bootstrap budget. Stop
   every guest, destroy, then perform a fresh successful apply. A stranded
   rendezvous host requires destroy and fresh apply.
6. Attempt destroy while guests run and confirm refusal. Stop all guests and
   complete destroy, proving their owned disks and media absent.

For the retained Q6 aged-image drill, interrupt after the media block completes,
wait at least 25 hours, then continue the same apply and record the result.
Use separate fresh runs where the required interruption points cannot all be
reached in one installation. A live operation may hold a lease that refuses
concurrent inspection. Reuse the same input and build for continuation; see
[build changes](../../../docs/operator-guide.md#changing-the-build-between-runs).

```sh
./bin/bootwright status --context lab-compact-df
./bin/bootwright machine list --context lab-compact-df --power-status
./bin/bootwright cluster info --context lab-compact-df --name compact
./bin/bootwright diff --context lab-compact-df --observe
umask 077
lab_compact_kubeconfig=$(mktemp /tmp/bootwright-compact-kubeconfig.XXXXXX)
./bin/bootwright cluster kubeconfig --context lab-compact-df --name compact > "$lab_compact_kubeconfig"
```

Protect the exported credential. Bootwright's cluster SSH and interactive client
commands are not part of this rehearsal.

## Cleanup

These commands erase the guests and their cluster data. For a partial apply,
stop each guest that exists and follow destroy's remediation for any absence or
quiescence it cannot prove. Repeat destroy to continue a partial removal.

```sh
./bin/bootwright machine stop --context lab-compact-df --name rhel-01 --yes
./bin/bootwright machine stop --context lab-compact-df --name rhel-02 --yes
./bin/bootwright machine stop --context lab-compact-df --name rhel-03 --yes
./bin/bootwright machine stop --context lab-compact-df --name compact-01 --yes
./bin/bootwright machine stop --context lab-compact-df --name compact-02 --yes
./bin/bootwright machine stop --context lab-compact-df --name compact-03 --yes
./bin/bootwright destroy --context lab-compact-df --authorize data-loss
./bin/bootwright status --context lab-compact-df
./bin/bootwright context delete --name lab-compact-df --purge --yes
```

Delete the context only after removal completes. Remove the resolver drop-in
created above and restore the prior resolver configuration. Remove the exported
kubeconfig and temporary input copy when no longer needed. Destroy removes the
private media, installer work area, services and managed network; the prepared
controller runtime and tool cache remain.

## Data Foundation preparation and acceptance

The [binding](add-ons/binding.yaml) consumes the
[managed export](storage/export.yaml), its RBD pool, CephFS `csi` subvolume
group, Prometheus service and TLS object gateway. The gateway endpoint and all
node addresses are synthetic; Ceph nodes use `.31` to `.33` and compact nodes
`.21` to `.23`. One RGW daemon is explicitly placed on `rhel-01`; the managed
DNS service publishes its address under the export's gateway name. This lab
placement has no gateway failover.
Reserve six consecutive emulated BMC ports starting at 8000. Include IBM
entitlement credentials in the OpenShift installation pull secret before
initializing the context; Bootwright never merges a live global pull secret.

Replace the RHEL boot-image URL and digest, verify the IBM Ceph coordinates,
and replace the synthetic Prometheus image digest. Import the selected boot
image through the media workflow before applying Machines; follow the
[three-node Ceph preparation](../../storage-clusters/libvirt-3-nodes-ceph/README.md#initial-setup).
The gateway certificate must cover `s3.lab.example.test` (or your replacement),
and its issuer must match `rgw-issuer`; the cluster selects it under `Always`.

Choose the catalog tag or digest, channel and CSV from your vendor-supported
combination. The example choices are placeholders. On a cluster serving that
catalog, inspect its PackageManifest with your matching `oc` client:

```sh
oc get packagemanifest odf-operator -n openshift-marketplace -o yaml
```

Read the selected channel's `currentCSV`, confirm the PackageManifest's
catalog source, and set `subscription.startingCSV`, `channel`, and the owned
catalog image before importing input. Later operator-managed dependent
InstallPlans remain the operator's responsibility.

The [B761 gate](../../../specs/milestones/m4.md#b761) is operator-run and has not
been accepted. Prove export continuation preserves the same principals and
details, interrupt operator and StorageCluster readiness and continue, and
verify all four StorageClasses, NooBaa `Ready`, and a disposable consumer of
each storage interface. Repeat a completed apply and prove no contact; destroy
retires the instances and export with their clusters. Record only sanitized
acceptance summaries, retaining credential-bearing evidence privately.
