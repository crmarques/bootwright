# lab-sno

Single-node OpenShift on one libvirt guest: `sno-01` at `198.51.100.31`
(8 vCPU, 16 GiB, 120 GiB, an emulated TPM), installed by the agent installer
through an emulated Redfish BMC, beside managed DNS, NTP, proxy and artifact
services on the controller. The node answers `api`, `api-int` and `*.apps`
itself (`platform: none`). Toggles add FIPS mode, TPM2 disk encryption and the
NMState and MetalLB add-ons. Context `lab-sno`, cluster `sno`.

It accepts [B61](../../specs/milestones/m3.md#b61) with
[B705](../../specs/milestones/m3.md#b705),
[B706](../../specs/milestones/m3.md#b706) and
[B462](../../specs/milestones/m3.md#b462) (default render),
[B755](../../specs/milestones/m4.md#b755) (`addons.nmstate`) and
[B757](../../specs/milestones/m4.md#b757) (`addons.metallb`), one ledger row per
item run. `security.fips` and `security.tpm2` only rehearse
[B722](../../specs/milestones/m3.md#b722) and
[B720](../../specs/milestones/m3.md#b720), which qualify on three nodes in
[lab-3-nodes-ocp-features](../lab-3-nodes-ocp-features/runbook.md).

## Inputs

| `--set` | Default | Effect | Needs |
| --- | --- | --- | --- |
| `security.fips=true` | `false` | FIPS cluster; the fleet and cluster keys become ECDSA P-256 | a FIPS-mode controller (`cat /proc/sys/crypto/fips_enabled` prints `1`); otherwise `plan` and `preflight controller` refuse `controller.unsupported` |
| `security.tpm2=true` | `false` | TPM2-bound LUKS on the node's installation disk | nothing; the profile already carries `tpm: {}` |
| `addons.nmstate=true` | `false` | binding `lab-sno-nmstate`: Kubernetes NMState operator and instance | `addons.subscriptions.nmstate.channel` and `.startingCSV` |
| `addons.metallb=true` | `false` | binding `lab-sno-metallb`: MetalLB operator, instance and layer 2 pool `services` | `addons.subscriptions.metallb.channel` and `.startingCSV` |

An enabled add-on with an empty channel or CSV fails the render with
`required`: they are operator choices with no default. `addons.metallbPool`
(default `198.51.100.200-198.51.100.207`) serves `LoadBalancer` Services only;
a single node has no virtual IPs and MetalLB never serves the API or ingress.
[values.yaml](values.yaml) holds the rest: addresses, upstreams, the release
(`openshift.version`) and the guest size.

## Prepare

- The [host preparation](../README.md#prepare-the-host) (ports 53, 123, 3128,
  8080, 8443 and loopback 8000 free), a pull secret saved as
  `~/pull-secret.json` with mode `0600`, `jq`, and an `oc` matching
  `openshift.version` installed independently of Bootwright. There is no RHEL
  media: the agent ISO is built from the release.
- Capacity, which the apply gate and `preflight container-cluster` count
  ([capacity admission](../../specs/substrates.md#capacity-admission)): the
  guest plus the provider's `capacityReserve` (1 CPU, 2 GiB, 10 GiB) need 9
  host CPUs not reserved by other libvirt domains, 18 GiB of `MemAvailable` in
  `/proc/meminfo` (swap never counts) and 130 GiB free on the filesystem
  backing `/var/lib/libvirt/images`, beside about 4 GiB for the image's three
  copies ([lab-sno profile](../../specs/openshift-coverage.md#acceptance-profiles)).
  Check with `nproc`, `grep MemAvailable /proc/meminfo` and
  `df -h /var/lib/libvirt/images`.
- Firewall: the managed bridge `virbr-e2e` is in firewalld's `trusted` zone
  ([provider host realization](../../specs/substrates.md#provider-host-realization)),
  so the guest reaches the host services with no rule.
- The build. B61's acceptance baseline is the build that contains X111 to
  X115; B755 needs a build that ran X138's in-tree gates and B757 one that ran
  X134's. Record `$B` and check that the commits naming those slices are in
  its history and that `make check` passed on it:

  ```sh
  B=$(./bin/bootwright version -o json | jq -r .result.commit)
  ./bin/bootwright version
  git log --oneline -E --grep='^Refs:.*X11[1-5]' "$B"
  git log --oneline -E --grep='^Refs:.*X13[48]' "$B"
  ```

  `version` reads `Source clean`; the first listing's `Refs:` lines name each
  of X111 to X115 (`git log -1 --format=%B <sha>` shows them), and the second
  X134 and X138.
- Earlier builds (B61): `./bin/bootwright context list` names every context
  and the build that registered each unfinished operation. Before installing
  this build, destroy every lab-sno context an earlier build applied, with the
  executable that applied it, when the contract guard moved a request version
  in between ([changing the build](../../docs/operator-guide.md#changing-the-build-between-runs));
  when you cannot tell, destroy it with that executable anyway.
- B706, an owner step. B706 is proved only by an image built on a controller
  where nmstate was not installed by hand, so the controller must lack
  `nmstatectl` before the controller stage. If this prints a path, remove the
  package; confirm that the transaction removes `nmstate` alone, and cancel
  it otherwise and record B706 as not reached:

  ```sh
  command -v nmstatectl
  sudo dnf remove nmstate
  command -v nmstatectl
  ```

  The second check prints nothing. The controller stage installs the package
  again, so it is present after the run; if it is not after Destroy and the
  host had it before, restore it with `sudo dnf install nmstate`.

Add-on channels and CSVs come from a cluster of the same release, so run the
default render first (it is the B61 run), read them before its destroy (step 2
of [Verify](#verify)), and use them for the add-on runs.

## Run

```sh
e2e/render.sh lab-sno                      # add --set toggles for the run
C=lab-sno; IN=~/bootwright-e2e/lab-sno
A=$(ip -4 route get 192.0.2.1 | sed -n 's/.* src \([0-9.]*\).*/\1/p')
./bin/bootwright validate -f "$IN"
./bin/bootwright setup --yes
./bin/bootwright context init --name "$C" --input-dir "$IN"
./bin/bootwright secret set --context "$C" --name openshift-pull-secret --value-file ~/pull-secret.json
./bin/bootwright secret generate --context "$C"
./bin/bootwright secret check --context "$C"
./bin/bootwright plan --context "$C"
./bin/bootwright apply --context "$C" --stage controller --yes
./bin/bootwright preflight controller --context "$C"
command -v nmstatectl                      # B706: now installed by the controller stage
./bin/bootwright apply --context "$C" --stage infrastructure-services --yes
```

The services answer on `$A`:

```sh
curl -sk "https://$A:8443/" -o /dev/null -w '%{http_code}\n'   # 404 on an empty root
dig @"$A" +short api.sno.e2e.example.test test.apps.sno.e2e.example.test   # both 198.51.100.31
chronyd -Q -t 10 "server $A iburst port 123"
```

The controller must resolve the cluster zone through the managed DNS. Route it,
and let preflight prove it (B705):

```sh
sudo install -d -m 0755 /etc/systemd/resolved.conf.d
printf '[Resolve]\nDNS=%s\nDomains=~e2e.example.test\n' "$A" | sudo tee /etc/systemd/resolved.conf.d/bootwright-e2e.conf
sudo systemctl restart systemd-resolved
./bin/bootwright preflight container-cluster --context "$C"
```

Every `resolution.*` and `capacity.*` check must be `passed`. A failed one names
the record or the route to add; it must pass before the last apply. Then:

```sh
./bin/bootwright plan --context "$C"
./bin/bootwright apply --context "$C" --yes
```

Allow over an hour: image build, boot, bootstrap wait, installation wait,
release. Record each phase's duration as the progress lines print it (`status
--detail full` repeats them) and the release installed. The apply names a
private operation-log directory before effects start; keep it as evidence.

## Verify

1. State and access:

   ```sh
   ./bin/bootwright status --context "$C"
   ./bin/bootwright machine list --context "$C" --power-status
   ./bin/bootwright cluster list --context "$C"
   ./bin/bootwright cluster info --context "$C" --name sno
   ./bin/bootwright diff --context "$C" --observe
   ./bin/bootwright apply --context "$C" --yes      # settles: boots nothing, contacts no cluster
   umask 077; KUBECONFIG=$(mktemp); export KUBECONFIG
   ./bin/bootwright cluster kubeconfig --context "$C" --name sno > "$KUBECONFIG"
   oc version; oc get nodes; oc get clusterversion; oc get co
   ```

   `oc` is the independent client. B61 wants one Ready node, the release equal
   to `openshift.version` and a clean `diff --observe`.

2. First-run facts, recorded for B61 and B462: the `openshift-client-linux`
   archive Bootwright fetched carried the release stamp (the controller stage
   did not refuse); the installer's cached base image directory, from `sudo find
   / -xdev -path '*agent/image_cache*' 2>/dev/null`; the keep task, proved by
   the B462 drill below. B706 is proved by the image building on a controller
   where `command -v nmstatectl` printed nothing before the controller stage,
   after the Prepare owner step.
   Before destroying the default run, read the add-on choices:

   ```sh
   oc get packagemanifest kubernetes-nmstate-operator -n openshift-marketplace -o jsonpath='{range .status.channels[*]}{.name}{"\t"}{.currentCSV}{"\n"}{end}'
   oc get packagemanifest metallb-operator -n openshift-marketplace -o jsonpath='{range .status.channels[*]}{.name}{"\t"}{.currentCSV}{"\n"}{end}'
   ```

3. Toggle checks:

   | Toggle | Check |
   | --- | --- |
   | fips | `oc debug node/master-0 -- chroot /host cat /proc/sys/crypto/fips_enabled` prints `1` |
   | tpm2 | `oc debug node/master-0 -- chroot /host lsblk -f` shows a `crypto_LUKS` root |
   | nmstate | `oc get csv,installplan,subscription -n openshift-nmstate`: the CSV equals `startingCSV`, one InstallPlan approved; `oc get nmstate`; `oc get deploy -n openshift-nmstate` (the webhook Deployment's name); `oc get nns` |
   | metallb | `oc get csv,installplan -n metallb-system`; `oc get metallb,ipaddresspool,l2advertisement -n metallb-system`; the pool `services` holds eight addresses |

   For B755 and B757 also record `diff --observe` reporting no drift, the
   build commit, the release minor, the channel and `startingCSV`, and the
   phase durations. The checks below re-verify the facts each section lists;
   they are this runbook's reading of those facts, unverified on a cluster
   until the first run records them.

   B755 (`<ns>` is `openshift-nmstate`):

   - `--ignore-not-found` classifies only NotFound:
     `oc get nmstate no-such-instance --ignore-not-found; echo $?` prints no
     object and `0`, while `oc get no-such-kind x --ignore-not-found; echo $?`
     fails with a nonzero status (a missing kind is not NotFound).
   - The InstallPlan's owner reference:
     `oc get installplan -n <ns> -o jsonpath='{.items[*].metadata.ownerReferences[*].kind}'`
     names `Subscription`.
   - `oc create --field-manager` records the manager:
     `oc get subscription -n <ns> -o yaml --show-managed-fields` shows
     Bootwright's manager in `metadata.managedFields`.
   - Whether `ResolutionFailed` can be transient on a Subscription of a ready
     catalog: during the approval wait, repeat
     `oc get subscription -n <ns> -o jsonpath='{range .items[*].status.conditions[*]}{.type}={.status}{"\n"}{end}'`
     and record whether `ResolutionFailed=True` ever appears and then clears.
   - The webhook Deployment's name: `oc get deploy -n <ns>` lists
     `nmstate-webhook`, the name the readiness check uses.

   B757 (`<ns>` is `metallb-system`):

   - The InstallPlan lists only the MetalLB CSV:
     `oc get installplan -n <ns> -o jsonpath='{.items[*].spec.clusterServiceVersionNames}'`.
   - The installed CSV supports all namespaces only and declares
     disconnected support:
     `oc get csv -n <ns> -o jsonpath='{.items[*].spec.installModes}'` shows
     `AllNamespaces` alone supported, and
     `oc get csv -n <ns> -o jsonpath='{.items[*].metadata.annotations}'`
     shows the disconnected feature annotation; record its exact key and
     value.
   - The webhook refuses no pool or advertisement create once `MetalLB`
     reports `Available`: the apply creates the pool and advertisement after
     that readiness without a refusal (its progress and operation log show
     each object created once), and `oc get events -n <ns>` shows no webhook
     denial.
   - `oc get ipaddresspool services -n <ns> -o yaml` shows
     `status.assignedIPv4` after the Service of step 4.
   - `oc get servicel2status -n <ns>` names the announcing node.

4. MetalLB Service (B757), in a namespace Bootwright does not own:

   ```sh
   oc create namespace lb-canary
   oc -n lb-canary create deployment web --image=registry.access.redhat.com/ubi9/httpd-24 --port=8080
   oc -n lb-canary expose deployment web --type=LoadBalancer --port=80 --target-port=8080
   oc -n lb-canary get svc web -w          # EXTERNAL-IP is inside 198.51.100.200-207
   curl -sI "http://$(oc -n lb-canary get svc web -o jsonpath='{.status.loadBalancer.ingress[0].ip}')"
   ```

   The controller reaches the address over the bridge. Remove the namespace
   afterwards: `oc delete namespace lb-canary`.

## Acceptance drills

Interrupt with Ctrl-C at the named phase, let the command exit, run `status`,
then repeat `apply --context "$C" --yes`; a failed block is retried and an
unknown outcome is observed before any retry. Keep the input, Secrets and build
unchanged ([build changes](../../docs/operator-guide.md#changing-the-build-between-runs)).
A drill that cannot reach its phase in one install needs a fresh destroy and run.

**B61** (default render). With the first run on the acceptance baseline, add
the `Container cluster row` template to the
[acceptance ledger](../../docs/acceptance.md), as B61's acceptance-row
template states, and link it from the Rows rule of the
[coverage page](../../specs/openshift-coverage.md#acceptance-profiles):

1. Interrupt during the image build; the next apply rebuilds.
2. Interrupt during the bootstrap wait; continue.
3. Interrupt during the installation wait; continue.
4. `kill -9` the `bootwright` process during the installation wait. The next
   `status` shows the block `unknown`; the next `apply` resolves it by
   observation.
5. A settled `apply` boots nothing; the kubeconfig export and `oc` reads are
   Verify step 1.
6. Destroy while the guest runs and confirm the refusal naming the stop, then
   stop and complete (see [Destroy](#destroy)). The completed destroy proves the
   domain's ownership metadata survived the emulator's boot selection and insert.

**B462** (a second short install after B61's destroy): start `apply`, poll
`sudo KUBECONFIG=/var/lib/bootwright-clusters/lab-sno/sno/auth/kubeconfig oc get clusterversion`
and `kill -9` the `bootwright` process once it reports `Available=True` and
before the apply records the block done. Then `machine stop`, `destroy`, and
interrupt it once `status` shows the media block removed; run
`cluster kubeconfig --context "$C" --name sno > /dev/null` and expect one
`access.unproved` warning on standard error. Repeat `destroy` to completion. If
the race is missed, record B462 as not reached and repeat on the next install.

**B755** and **B757**. With their first run, add the `Add-on row` template to
the [acceptance ledger](../../docs/acceptance.md), as B755's acceptance-row
template states, and link it from the Rows rule of the
[add-on coverage page](../../specs/add-on-coverage.md#acceptance-profiles).

**B755** (`addons.nmstate`, after the default run is destroyed; render with the
add-on, then `context update --name "$C" --input-dir "$IN" --yes`):

```sh
./bin/bootwright apply --context "$C" --yes --until sno.nmstate
```

Interrupt during object creation, during the approval wait and during the
readiness wait, continuing each time with `apply`; the repeat reports every
object `unchanged`. Then `diff --observe` reports no drift, a repeated `apply`
contacts no cluster, and on a final fresh run interrupted during readiness,
`machine stop --name sno-01 --yes` (the cluster API is then unreachable) and
`destroy` complete by retirement.

**B757** (`addons.metallb`, same procedure): interrupt during the approval
wait, and between the `MetalLB` resource and the pools, continuing each with
`apply` (every object `unchanged`); then Verify step 4, `diff --observe`, and a
fresh `destroy` completing by retirement. With both toggles on, `--until
sno.nmstate` stops after NMState so its drills run before MetalLB starts, and
the final destroy covers both rows.

## Destroy

```sh
./bin/bootwright destroy --context "$C" --authorize data-loss
./bin/bootwright machine stop --context "$C" --name sno-01 --yes
./bin/bootwright destroy --context "$C" --authorize data-loss
./bin/bootwright status --context "$C"
./bin/bootwright context delete --name "$C" --purge --yes
```

The first destroy refuses while the guest runs and names the stop. A complete
destroy removes the guest, disks, media, the installer work area, the managed
services and the network; the context releases its custodied kubeconfig only
when it completes. Delete the context only after that, and keep it (skip the
last command) when you re-render for an add-on run: render again, then
`context update` as above. Afterwards remove the resolver drop-in
(`sudo rm /etc/systemd/resolved.conf.d/bootwright-e2e.conf` and restart
`systemd-resolved`), the exported kubeconfig and the rendered directory.
