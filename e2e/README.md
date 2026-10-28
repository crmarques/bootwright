# End-to-end cases

Each case renders a complete Bootwright input from one Helm chart and runs it
on one libvirt host, from host preparation to cleanup. [input.yaml](input.yaml)
is the default input for every case; [render.sh](render.sh) renders one case
into a directory. [runbooks/](runbooks/) holds the full runbook of each
[example](../examples/README.md) input: an item that names an example as its
gate is accepted from that runbook, and the case table below says where a
templated case can stand in.

| Case | Contexts | Guests (vCPU / GiB each) | Outcome |
| --- | --- | --- | --- |
| [rhel](#case-rhel) | `e2e-rhel` | 1 (2 / 4) | RHEL on a libvirt guest with managed DNS, NTP, proxy and artifact server; optional FIPS, TPM2 root encryption, RHSM registration and update within the release. |
| [ceph](#case-ceph) | `e2e-ceph` | 3 (4 / 8) | IBM Storage Ceph on three RHEL guests with block, file, object, NFS, SMB and observability; NVMe-oF optional. |
| [sno](#case-sno) | `e2e-sno` | 1 (8 / 16) | Single-node OpenShift; optional FIPS, TPM2 and the NMState and MetalLB add-ons. |
| [stretch](#case-stretch) | `e2e-stretch` | 5 (2 / 4) | Ceph over three sites: four OSD hosts in two sites and a monitor-only arbiter. A two-context variant consumes the arbiter as a provided-OS host, blocked by [B882](../specs/milestones/backlog.md#b882). |
| [compact](#case-compact) | `e2e-compact` | 3 (6 / 16) | Three-node compact OpenShift with installer-owned API and ingress VIPs and MetalLB for `LoadBalancer` Services. |

Run one case at a time: the cases share the bridge, the guest subnet and the
service ports. The stretch variant's two contexts are the exception.

## Prepare the host

Once per host, from the repository root:

1. A Fedora or RHEL amd64 host with `/dev/kvm`, sudo, `helm` and network
   egress ([controller requirements](../specs/controller.md#supported-hosts)).
   Bootwright installs everything else it needs.
2. Build and prepare: `make build`, then `./bin/bootwright setup --yes`.
3. Free ports on the host address: 53/tcp+udp, 123/udp, 3128, 8080, 8443 and,
   on loopback, 8000. The two-context stretch variant also uses 8081, 8444
   and 8001. A
   wildcard host `chronyd` or `dnsmasq` must not hold them:

   ```sh
   ss -lntup | grep -E ':(53|123|3128|808[01]|844[34]|800[01])\b'
   ```

4. Pin the host address. `host.address: auto` takes the default-route source
   address at render time and every managed service binds it. A DHCP lease
   that moves later breaks a running case, so prefer a reservation.
5. Media and credentials, outside the repository:
   - RHEL cases (rhel, ceph, stretch): the entitled RHEL 9.8 boot and DVD ISOs,
     added once per host (step 1 of [Run a case](#run-a-case)).
   - OpenShift cases (sno, compact): a pull secret saved as
     `~/pull-secret.json` with mode `0600`.
   - Ceph cases: an IBM entitlement key for `cp.icr.io`. The rendered
     Entitlement carries `license.accept: true`; accepting the IBM license
     terms is your act.
   - rhel with `registration.enabled`: an RHSM organization ID and activation
     key.
6. Memory. Each case's total is in the table above, plus about 4 GiB for the
   host and services. compact declares 48 GiB of guests: on a 32 GiB host add
   swap before running it, and expect a slow installation:

   ```sh
   sudo fallocate -l 32G /var/swap-e2e && sudo chmod 600 /var/swap-e2e
   sudo mkswap /var/swap-e2e && sudo swapon /var/swap-e2e
   ```

   Remove it after cleanup with `sudo swapoff /var/swap-e2e && sudo rm /var/swap-e2e`.

## Render a case

```sh
e2e/render.sh <case> [--values FILE]... [--set KEY=VALUE]... [--out-dir DIR]
```

The output defaults to `~/bootwright-e2e/<case>`. A render replaces only an
empty directory or an earlier render's, and never writes inside the repository. Change inputs with `--set` (for example `--set security.fips=true`) or
with a copy of [input.yaml](input.yaml) passed as `--values`; later values win.
Every key is described in [input.yaml](input.yaml). An enabled add-on needs
its `addons.subscriptions` channel and CSV, so `compact` renders only once they
are set. Check the render:

```sh
./bin/bootwright validate -f ~/bootwright-e2e/<case>
```

Context import freezes the rendered input: render again and run
`context update` only after a complete destroy.

## Run a case

The common journey; each case section below adds its own steps. `C` is the
context name from the table, `IN` the rendered directory.

1. Media, once per host (RHEL cases):

   ```sh
   ./bin/bootwright media add --name rhel-9.8-x86_64-boot.iso --from-file /path/to/rhel-9.8-x86_64-boot.iso
   ./bin/bootwright media add --name rhel-9.8-x86_64-dvd.iso --from-file /path/to/rhel-9.8-x86_64-dvd.iso
   ./bin/bootwright media list --checksums
   ```

2. Context and Secrets:

   ```sh
   ./bin/bootwright context init --name "$C" --input-dir "$IN"
   ./bin/bootwright secret generate --context "$C"
   ./bin/bootwright secret check --context "$C"
   ./bin/bootwright plan --context "$C"
   ```

   Set every `contextStore` Secret the case lists before the first apply:
   `secret check` names the missing ones, and the first apply freezes them.

3. Apply by stage, checking each before the next:

   ```sh
   ./bin/bootwright apply --context "$C" --stage controller --yes
   ./bin/bootwright preflight controller --context "$C"
   ./bin/bootwright apply --context "$C" --stage infrastructure-services --yes
   ./bin/bootwright apply --context "$C" --yes
   ```

   After the services stage the controller answers on its address:

   ```sh
   A=<host address>
   curl -sk "https://$A:8443/" -o /dev/null -w '%{http_code}\n'   # 404 on an empty root
   dig @"$A" +short controller.e2e.example.test
   chronyd -Q -t 10 "server $A iburst port 123"
   systemctl list-units 'bootwright-*'
   ```

4. Inspect, then prove the completed operation settles:

   ```sh
   ./bin/bootwright status --context "$C"
   ./bin/bootwright machine list --context "$C" --power-status
   ./bin/bootwright diff --context "$C" --observe
   ./bin/bootwright apply --context "$C" --yes
   ```

## Case rhel

Guest `rhel-01` at `198.51.100.11`; `C=e2e-rhel`. Toggles:

| `--set` | Effect | Secrets to set before apply |
| --- | --- | --- |
| `security.fips=true` | FIPS-mode install; the machine key becomes ECDSA P-256 | none |
| `security.tpm2=true` | LUKS root unlocked by the guest TPM2, with a recovery passphrase | `luks-recovery` |
| `registration.enabled=true` | RHSM registration from the host's hosted service | `rhsm-org`, `rhsm-key` |
| `registration.updateWithinRelease=true` | With registration: update to the newest 9.8 packages, then reboot | as above |

```sh
./bin/bootwright secret set --context "$C" --name luks-recovery --value-stdin
./bin/bootwright secret set --context "$C" --name rhsm-org --value-stdin
./bin/bootwright secret set --context "$C" --name rhsm-key --value-stdin
```

After the run:

```sh
./bin/bootwright machine exec --context "$C" --name rhel-01 -- cat /etc/os-release
./bin/bootwright machine exec --context "$C" --name rhel-01 -- chronyc sources
./bin/bootwright machine exec --context "$C" --name rhel-01 -- getent hosts controller.e2e.example.test
./bin/bootwright machine exec --context "$C" --name rhel-01 -- fips-mode-setup --check            # fips
./bin/bootwright machine exec --context "$C" --name rhel-01 -- sudo update-crypto-policies --show # fips: FIPS
./bin/bootwright machine exec --context "$C" --name rhel-01 -- lsblk -f                           # tpm2: crypto_LUKS root
./bin/bootwright machine exec --context "$C" --name rhel-01 -- sudo subscription-manager status    # registration
./bin/bootwright machine exec --context "$C" --name rhel-01 -- sudo dnf check-update              # update: nothing within 9.8
./bin/bootwright machine restart --context "$C" --name rhel-01 --yes
./bin/bootwright machine exec --context "$C" --name rhel-01 -- uptime                             # repeat until SSH answers
```

The restart proves the FIPS boot state and the unattended TPM2 unlock. The
recovery passphrase is the `luks-recovery` value you set; it unlocks the root
at the guest console (`virsh --connect qemu:///system console
bootwright-e2e-rhel-rhel-01`) whenever the TPM2 unlock does not.
Destroy unregisters a running guest (see
[Destroy and clean up](#destroy-and-clean-up)). When the guest is gone or
unreachable, unregister it at the subscription service yourself and name it
with `destroy --unregistered rhel-01`.

## Case ceph

Guests `rhel-01..03` at `198.51.100.11-13`, with one 30 GiB OSD disk each
(`sizes.ceph.osdDisks`); cluster `ceph-e2e`; `C=e2e-ceph`. Each service group
is a switch under `ceph.services`, and all are on except `nvmeof`. NFS and SMB
need `file`. The object ingress VIP is `198.51.100.60` (TLS 443). The
management gateway VIP is `198.51.100.62:9443`. Neither has a DNS record: use
`curl --resolve`. With every group on, 8 GiB guests are tight; switch groups off
or raise `sizes.ceph.memoryMiB` if OSDs flap.

Before the first apply:

```sh
./bin/bootwright secret set --context "$C" --name ibm-registry --username cp --password-stdin --yes
./bin/bootwright secret set --context "$C" --name files-smb-user --value-stdin   # smb
```

Apply by stage to see each layer settle; check that the guests resolve the IBM
hosts before the clusters stage:

```sh
./bin/bootwright apply --context "$C" --stage controller --yes
./bin/bootwright apply --context "$C" --stage infrastructure-services,substrates --yes
./bin/bootwright apply --context "$C" --stage machines --yes
./bin/bootwright machine exec --context "$C" --name rhel-01,rhel-02,rhel-03 -- getent hosts cp.icr.io public.dhe.ibm.com
./bin/bootwright preflight storage-cluster --context "$C"
./bin/bootwright apply --context "$C" --stage clusters --yes
```

`cluster info --context "$C" --name ceph-e2e` prints the admin command; below
`ceph` stands for it, run through
`./bin/bootwright cluster exec --context "$C" --name ceph-e2e --node rhel-01 -- sudo -n <command>`.

| Group | Checks |
| --- | --- |
| core | `ceph -s` is `HEALTH_OK` (or only `CHA_ERROR_SENDING_REPORT` while Call Home cannot reach IBM) with 3 mons and every OSD up and in; `ceph pg stat` all `active+clean`; `ceph orch ps` shows a `crash` daemon per host |
| block | `rbd info rbd-data/rbd-disk`; as client `rbd-canary` with its private keyring ([pool client](runbooks/libvirt-3-nodes-ceph.md#pool-client)), write 1 MiB with `rbd import`, read it back with `rbd export` and `cmp` |
| file | `ceph fs status files` shows one active and one standby MDS; `ceph fs subvolume ls files --group_name apps`; mount `cephfs-canary` with `cluster keyring --client cephfs-canary`, write, read, compare; `ceph mds fail files:0` and check the standby takes over |
| object | `cluster info --name ceph-e2e --object-user s3-canary` gives keys; `aws s3api put-object`, `get-object`, `delete-object` on `s3-canary-bucket` through `https://s3.e2e.example.test` resolved to the VIP with the `rgw-objects-tls` CA (`curl --connect-to s3.e2e.example.test:443:198.51.100.60:443 --cacert <ca>`); stop one RGW daemon with `ceph orch daemon stop` and repeat the get |
| nfs | from `rhel-02`, `mount -t nfs4 198.51.100.11:/docs /mnt`, then write, read and delete as uid 2000; the same mount from `rhel-03` is refused |
| smb | `ceph smb show`; `smbclient //rhel-02.e2e.example.test/documents -U smbuser -c 'put f; get f; del f'`, again through `rhel-03`; a wrong user is refused |
| observability | `ceph orch ls` lists prometheus, alertmanager, grafana, loki, alloy and mgmt-gateway; `curl -k https://198.51.100.62:9443`; Prometheus targets are up; a Loki query returns daemon logs |
| nvmeof | needs AVX-512 (`grep -c avx512f /proc/cpuinfo`); `nvme discover -t tcp -a 198.51.100.12 -s 4420`, connect with the `initiator` host NQN, write, read, compare |

The [libvirt-3-nodes-ceph](runbooks/libvirt-3-nodes-ceph.md),
[block-file](runbooks/libvirt-3-nodes-ceph-block-file.md),
[object](runbooks/libvirt-3-nodes-ceph-object.md) and
[NFS](runbooks/libvirt-3-nodes-ceph-nfs.md) runbooks hold the full checks,
including the pool-scoped client and its confidential keyring handling. For
the outage row, run `machine stop --context "$C" --name rhel-03 --yes`,
watch `ceph -s` degrade and serve I/O, then `machine start` and wait for
`HEALTH_OK`. Ceph removal needs every guest stopped before
`destroy --authorize data-loss`.

## Case sno

Guest `sno-01` at `198.51.100.31`; cluster `sno`; `C=e2e-sno`. Toggles:
`security.fips`, `security.tpm2`, `addons.sno.nmstate` and `addons.sno.metallb`.
An enabled add-on needs `addons.subscriptions.<package>.channel` and
`.startingCSV` (see [input.yaml](input.yaml)); MetalLB serves
`198.51.100.200-207` to `LoadBalancer` Services. A FIPS cluster also needs a
FIPS-mode controller (`cat /proc/sys/crypto/fips_enabled` prints `1`).

There is no RHEL media: the agent ISO is built from the release. Set the pull
secret, and once the services stage is up, route the cluster domain to the
managed DNS:

```sh
./bin/bootwright secret set --context "$C" --name openshift-pull-secret --value-file ~/pull-secret.json
sudo install -d -m 0755 /etc/systemd/resolved.conf.d
printf '[Resolve]\nDNS=%s\nDomains=~e2e.example.test\n' "$A" | sudo tee /etc/systemd/resolved.conf.d/bootwright-e2e.conf
sudo systemctl restart systemd-resolved
./bin/bootwright preflight container-cluster --context "$C"
```

Then `apply --context "$C" --yes`. Allow over an hour. After it settles:

```sh
umask 077; KUBECONFIG=$(mktemp); export KUBECONFIG
./bin/bootwright cluster kubeconfig --context "$C" --name sno > "$KUBECONFIG"
oc get nodes; oc get clusterversion; oc get co
oc debug node/master-0 -- chroot /host cat /proc/sys/crypto/fips_enabled   # fips: 1
oc debug node/master-0 -- chroot /host lsblk -f                            # tpm2: crypto_LUKS root
oc get csv -n openshift-nmstate; oc get nns                                # nmstate
oc get csv,ipaddresspool,l2advertisement -n metallb-system                 # metallb
```

Use an `oc` matching the release. For MetalLB, create a Deployment and a
`type: LoadBalancer` Service, check that its `EXTERNAL-IP` is in the pool, and
`curl` it from the host. For the add-on rows, interrupt during the
InstallPlan approval and readiness waits and continue, then run
`diff --observe`. [B61](../specs/milestones/m3.md#b61) needs interruptions in
the image build, the bootstrap wait and the install wait, plus one `kill -9`
during the install wait. The [lab-sno runbook](runbooks/lab-sno.md) holds the
full procedure.

## Case stretch

Five guests in three sites, one context, `C=e2e-stretch`: `ceph-01..04` at
`198.51.100.61-64` (dc1: 01-02, dc2: 03-04) carry the OSDs, and `arbiter-01`
at `198.51.100.65` (dc3) is a monitor-only node. Cluster `ceph-stretch` has
five monitors, two managers and one RBD pool, image and client. Native stretch
mode (`topology.stretch`) is not declared: admission defers it until
[B486](../specs/milestones/m2.md#b486). This case proves placement across
sites and a monitor-only arbiter, not stretch failover. Run it like
[ceph](#case-ceph), with the `ibm-registry` Secret and the staged apply, then
check that `ceph -s` shows 5 monitors in quorum and `ceph osd tree` shows OSDs
only on `ceph-01..04`.

### Two-context variant

`stretch-arbiter` installs `arbiter-01` in its own context, and `stretch-ceph`
consumes it as a provided-OS Machine: Bootwright receives a host whose RHEL is
already installed. Both render and validate, but `stretch-ceph` does not plan
yet: core Ceph binds only hosts its own context installed
([B882](../specs/milestones/backlog.md#b882)). The design, for when B882 lands:

| Context | Owns | Ports |
| --- | --- | --- |
| `e2e-stretch-ceph` (`K`) | DNS, NTP, proxy, artifact server, BMC, `ceph-01..04` and the cluster; it attaches to A's bridge as external | 53, 123, 3128, 8443, 8080, BMC 8000 |
| `e2e-stretch-arbiter` (`A`) | the managed network and `arbiter-01`, using K's DNS, NTP and proxy as external services | 8444, 8081, BMC 8001 |

```sh
e2e/render.sh stretch-ceph && e2e/render.sh stretch-arbiter
K=e2e-stretch-ceph; A=e2e-stretch-arbiter
./bin/bootwright context init --name "$K" --input-dir ~/bootwright-e2e/stretch-ceph
./bin/bootwright context init --name "$A" --input-dir ~/bootwright-e2e/stretch-arbiter
./bin/bootwright secret generate --context "$K"
./bin/bootwright secret generate --context "$A"
umask 077; key=$(mktemp)
./bin/bootwright secret show --context "$A" --name bootwright-machine-key --part private-key > "$key"
./bin/bootwright secret set --context "$K" --name arbiter-access-key --private-key-file "$key"
rm -f "$key"
./bin/bootwright secret set --context "$K" --name ibm-registry --username cp --password-stdin --yes
./bin/bootwright apply --context "$K" --stage controller --yes
./bin/bootwright apply --context "$K" --stage infrastructure-services --yes
./bin/bootwright apply --context "$A" --yes        # network and arbiter-01
./bin/bootwright apply --context "$K" --yes        # ceph-01..04 and the cluster
```

A needs K's services and K needs A's bridge and installed arbiter, so the order
is fixed. Destroy K first, then A: K's claim on the bridge makes A's network
removal refuse while K exists. K's destroy leaves the arbiter's operating
system, because that host is provided.

## Case compact

Masters `compact-01..03` at `198.51.100.41-43`; API VIP `198.51.100.50`;
ingress VIP `198.51.100.51`; cluster `compact`; `C=e2e-compact`. The installer
holds both VIPs with keepalived: MetalLB never serves the API or the default
ingress. It serves `198.51.100.200-207` to `LoadBalancer` Services. Set
`addons.subscriptions.metallb.*`, or `--set addons.compact.metallb=false`.
`security.fips` and `security.tpm2` work as in sno. Read the swap note in
[Prepare the host](#prepare-the-host) first. B66 requires B61's accepted
row, so run sno first. The full drills are in the
[lab-compact runbook](runbooks/lab-compact.md).

The pull secret, the resolver route and the run are as in [sno](#case-sno).
After it settles:

```sh
oc get nodes                                  # three Ready masters
curl -k https://198.51.100.50:6443/version    # API on its VIP from the host
curl -kI https://198.51.100.51                # ingress router
dig +short api.compact.e2e.example.test test.apps.compact.e2e.example.test
```

Then run the MetalLB checks from sno. [B66](../specs/milestones/m3.md#b66) also
needs these steps:

- Interrupt after the first node boots, then continue; only the others boot.
- Interrupt in the bootstrap and the install waits; one `kill -9` among them.
- The stalled-node drill: stop one master during bootstrap. The failure names it.
- A refused destroy while the masters run, then a complete one.

## Interrupt and continue

Continuation is part of every acceptance: interrupt an apply with Ctrl-C (or
`kill -9` of the `bootwright` process for the SIGKILL row) at the phase the
item names, then:

```sh
./bin/bootwright status --context "$C"
./bin/bootwright apply --context "$C" --yes
```

A failed block is retried and an unknown outcome is observed before any retry.
To abandon an apply instead, stop its guests (`machine stop --force` when an
installer cannot shut down) and destroy.
Keep the input, Secrets and media unchanged while continuing. A rebuilt binary
continues the same operation
([changing the build](../docs/operator-guide.md#changing-the-build-between-runs)).
An interrupted destroy continues with `destroy`, never `apply`.

## Destroy and clean up

Run destroy and follow what it names. Its plan lists the guests to stop first
(a Ceph cluster retires with its stopped hosts) and, under
`Stop after online cleanup`, guests it still needs running, such as one to
unregister from RHSM. When it refuses or pauses, stop the guests it names and
repeat it until it completes:

```sh
./bin/bootwright destroy --context "$C" --authorize data-loss
./bin/bootwright machine stop --context "$C" --name <the guests it names> --yes
./bin/bootwright destroy --context "$C" --authorize data-loss
./bin/bootwright status --context "$C"
systemctl list-units --all 'bootwright-*'
./bin/bootwright context delete --name "$C" --purge --yes
```

With `--yes` or without a terminal, `--authorize` takes the plan digest it
prints (`data-loss=<digest>`). Destroy removes the guests, disks, network, BMCs
and managed services; verify that before deleting the context, which removes
only local state and Secrets. Media stays reserved until no context uses it;
remove it with `media delete --name <name>` when done. Remove any resolver
drop-in, swap file and rendered directory the run added.

## Items each case accepts

Record each accepted run as a row of the
[acceptance ledger](../docs/acceptance.md). An item's own section names its
exact evidence. Where it names an example as its gate, run that example's
runbook, or record the templated case's row and note the substitution, which
is yours to accept.

| Case | Accepts | Rehearses only, or elsewhere |
| --- | --- | --- |
| rhel | [B75](../specs/milestones/m1.md#b75) with `security.fips`; [B77](../specs/milestones/m1.md#b77) with `security.tpm2`; [B76](../specs/milestones/m1.md#b76) with `registration.enabled`; [B876](../specs/milestones/m1.md#b876) with `registration.updateWithinRelease` | [B346](../specs/milestones/m1.md#b346) needs a FIPS-mode controller; [B877](../specs/milestones/m1.md#b877) needs a Satellite; [B73](../specs/milestones/m1.md#b73) and [B496](../specs/milestones/m1.md#b496) run [lab-baremetal](runbooks/lab-baremetal.md); [B74](../specs/milestones/m1.md#b74) and [B326](../specs/milestones/m1.md#b326) run [libvirt-network-variants](runbooks/libvirt-network-variants.md) |
| ceph | [B626](../specs/milestones/m2.md#b626): one run per IBM Storage Ceph release, set in `ceph.*` | [B81](../specs/milestones/m2.md#b81), [B492](../specs/milestones/m2.md#b492), [B477](../specs/milestones/m2.md#b477), [B478](../specs/milestones/m2.md#b478), [B489](../specs/milestones/m2.md#b489), [B686](../specs/milestones/m2.md#b686), [B479](../specs/milestones/m2.md#b479), [B689](../specs/milestones/m2.md#b689), [B482](../specs/milestones/m2.md#b482) and [B480](../specs/milestones/m2.md#b480) are accepted through [B484](../specs/milestones/m2.md#b484)'s profile rows P1 to P7, each from its [runbook](runbooks/); this case rehearses them on one cluster |
| sno | [B61](../specs/milestones/m3.md#b61), with [B705](../specs/milestones/m3.md#b705), [B706](../specs/milestones/m3.md#b706) and [B462](../specs/milestones/m3.md#b462) (gate: [lab-sno](runbooks/lab-sno.md)); [B755](../specs/milestones/m4.md#b755) with `addons.sno.nmstate` ([lab-sno-nmstate](runbooks/lab-sno-nmstate.md)); [B757](../specs/milestones/m4.md#b757) with `addons.sno.metallb` ([lab-sno-metallb](runbooks/lab-sno-metallb.md)) | [B720](../specs/milestones/m3.md#b720) and [B722](../specs/milestones/m3.md#b722) qualify on three nodes ([lab-features](runbooks/lab-features.md)); `security.*` here rehearses them |
| compact | [B66](../specs/milestones/m3.md#b66), with [B709](../specs/milestones/m3.md#b709), [B710](../specs/milestones/m3.md#b710) and [B260](../specs/milestones/m3.md#b260) (gate: [lab-compact](runbooks/lab-compact.md), same sizes) | [B729](../specs/milestones/m3.md#b729) needs five nodes and three networks ([lab-networks](runbooks/lab-networks.md)) |
| stretch | none yet: native stretch is [B486](../specs/milestones/m2.md#b486) to [B488](../specs/milestones/m2.md#b488), and the two-context variant waits for [B882](../specs/milestones/backlog.md#b882) | — |

Physical items, such as B70, B78, B305, B319, B485, B730 and B880, need real
hardware and are out of scope here.
