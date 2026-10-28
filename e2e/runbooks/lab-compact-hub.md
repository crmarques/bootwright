# ACM and GitOps on compact OpenShift

Install three schedulable masters and no workers on one libvirt host through
OpenShift's agent installer. The installer owns the API and ingress virtual
IPs on the managed NAT network; its derived platform is `baremetal` with
`provisioningNetwork: disabled`. This includes the [B766 hub rehearsal](../../specs/milestones/m4.md#b766);
operator acceptance remains open and requires the accepted compact-cluster baseline.

All addresses and names are synthetic. The controller needs hardware
virtualization, root/sudo access, outbound repository and registry access,
at least 28 logical CPUs, 80 GiB available memory and 400 GiB free pool disk,
plus about 7 GiB for five image copies. Each master requests 8 vCPU, 24 GiB
memory and a 120 GiB root disk; the provider reserves another 4 CPUs, 8 GiB
and 40 GiB. See [acceptance profiles](../../specs/openshift-coverage.md#acceptance-profiles).

| Input | Purpose |
| --- | --- |
| [Environment](../../examples/add-ons/lab-compact-hub/environment.yaml), [controller](../../examples/add-ons/lab-compact-hub/infra/machines/controller.yaml) | Existing local controller with container runtime and libvirt. |
| [Provider](../../examples/add-ons/lab-compact-hub/infra/providers/lab-libvirt.yaml), [network](../../examples/add-ons/lab-compact-hub/infra/networkconfigs/lab-guests.yaml) | Managed NAT network, compact VM profile and loopback HTTPS emulated BMCs. |
| [Masters](../../examples/add-ons/lab-compact-hub/infra/machines/) | Three installer-provisioned guests at consecutive addresses. |
| [Cluster](../../examples/add-ons/lab-compact-hub/clusters/compact/cluster.yaml) | Exact release, API and ingress VIPs, master membership and Secrets. |
| [Services](../../examples/add-ons/lab-compact-hub/infra/components/), [Secret descriptors](../../examples/add-ons/lab-compact-hub/secret-descriptors/) | Managed DNS, NTP, private artifact serving and generated credentials. |

## Prepare a private copy

A context holding a completed lab-sno apply must destroy it with the executable
that created it before importing this expanded topology. Never edit the input
of a live installation. Use an unused context for a fresh rehearsal.

```sh
hub_input=$(mktemp -d /tmp/bootwright-lab-compact-hub.XXXXXX)
cp -R examples/add-ons/lab-compact-hub/. "$hub_input/"
```

Customize this copy before initializing its context:

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
  Apply the source-scoped [network exposure](../../docs/operator-guide.md#network-exposure)
  rules for guest access.

Download a pull-secret JSON file into a private path outside the input tree.
The following commands assume `~/pull-secret.json`; Secret values never belong
in these YAML files. Run commands from the repository root with one invoking
account throughout.

## Stage and install

```sh
make build
./bin/bootwright version
./bin/bootwright validate -f "$hub_input"
./bin/bootwright setup --dry-run
./bin/bootwright setup --yes
./bin/bootwright context init --name lab-compact-hub --input-dir "$hub_input"
chmod 600 ~/pull-secret.json
./bin/bootwright secret set --context lab-compact-hub --name openshift-pull-secret --value-file ~/pull-secret.json
./bin/bootwright secret set --context lab-compact-hub --name bootstrap-repository --username example-user --password-file ~/git-token.txt
./bin/bootwright secret generate --context lab-compact-hub
./bin/bootwright secret check --context lab-compact-hub
./bin/bootwright validate --context lab-compact-hub
./bin/bootwright render effective --context lab-compact-hub
./bin/bootwright plan --context lab-compact-hub
./bin/bootwright apply --context lab-compact-hub --stage controller --yes
./bin/bootwright preflight controller --context lab-compact-hub
./bin/bootwright apply --context lab-compact-hub --stage infrastructure-services --yes
```

Configure the controller's conditional resolver for the managed cluster zone
before continuing. For a host already using systemd-resolved, substitute your
service address and domain in this example and preserve prior configuration:

```sh
sudo install -d -m 0755 /etc/systemd/resolved.conf.d
sudo tee /etc/systemd/resolved.conf.d/bootwright-lab-compact-hub.conf >/dev/null <<'RESOLVED'
[Resolve]
DNS=192.0.2.1
Domains=~lab.example.test
RESOLVED
sudo systemctl restart systemd-resolved
./bin/bootwright preflight container-cluster --context lab-compact-hub
./bin/bootwright plan --context lab-compact-hub
./bin/bootwright apply --context lab-compact-hub --yes
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
[build changes](../../docs/operator-guide.md#changing-the-build-between-runs).

```sh
./bin/bootwright status --context lab-compact-hub
./bin/bootwright machine list --context lab-compact-hub --power-status
./bin/bootwright cluster info --context lab-compact-hub --name compact
./bin/bootwright diff --context lab-compact-hub --observe
umask 077
lab_compact_kubeconfig=$(mktemp /tmp/bootwright-compact-kubeconfig.XXXXXX)
./bin/bootwright cluster kubeconfig --context lab-compact-hub --name compact > "$lab_compact_kubeconfig"
```

Protect the exported credential. Bootwright's cluster SSH and interactive client
commands are not part of this rehearsal.

## Cleanup

These commands erase the guests and their cluster data. For a partial apply,
stop each guest that exists and follow destroy's remediation for any absence or
quiescence it cannot prove. Repeat destroy to continue a partial removal.

```sh
./bin/bootwright machine stop --context lab-compact-hub --name compact-01 --yes
./bin/bootwright machine stop --context lab-compact-hub --name compact-02 --yes
./bin/bootwright machine stop --context lab-compact-hub --name compact-03 --yes
./bin/bootwright destroy --context lab-compact-hub --authorize data-loss
./bin/bootwright status --context lab-compact-hub
./bin/bootwright context delete --name lab-compact-hub --purge --yes
```

Delete the context only after removal completes. Remove the resolver drop-in
created above and restore the prior resolver configuration. Remove the exported
kubeconfig and temporary input copy when no longer needed. Destroy removes the
private media, installer work area, services and managed network; the prepared
controller runtime and tool cache remain.

## Hub packages and handoff

The [binding](../../examples/add-ons/lab-compact-hub/add-ons/binding.yaml) installs ACM with a pinned multicluster
engine, then GitOps last. The example CSVs and channels illustrate the input
shape; choose versions served by your catalog and compatible with the selected
OpenShift release before initializing the context. The HTTPS repository is
synthetic: replace it with a repository you control. The declared
`bootstrap-repository` Secret supplies its username and password or token.
Replace `example-user` in the staging command and store the repository token
in the private `~/git-token.txt` file before running it. This exercises B766's
private repository placement; the declaration contains no credential bytes.

For the B766 permission drill, create a dedicated ClusterRole granting only the
namespace and ConfigMap permissions the test repository needs, then set
`bootstrap.access.clusterRole` to that role's name before context initialization.
The test repository must exclude every Bootwright-owned object, including the
root Application and repository Secret. Bootwright creates the root once and
never waits for synchronization; the operator observes the namespace and
ConfigMap that Argo CD creates. Destroy retires add-ons with their cluster.

In addition to the compact-cluster drills above, interrupt during MCE approval,
hub readiness and default Argo CD readiness, continuing each with `apply`.
Prove the exact ACM and MCE CSVs, the hub's `Running` phase, and Argo CD's
`Available` phase. Repeat completed apply and prove no cluster access; run
`diff --observe` and prove no drift. On a separate interrupted apply, destroy
with an unreachable cluster API and prove instance retirement. Record the
remaining version, permission and credential checks from
[B766](../../specs/milestones/m4.md#b766) in private acceptance evidence.
