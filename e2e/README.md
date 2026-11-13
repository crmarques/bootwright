# End-to-end cases

Each directory is one self-contained case that runs on a single libvirt
laptop-class host, from host preparation to cleanup:

```text
e2e/<case>/
  values.yaml    the case's default input
  templates/     the Bootwright input as Helm templates, one object per file
  runbook.md     prepare, run, verify, acceptance drills, destroy
```

[render.sh](render.sh) renders one case into an input directory. Each case
exists because a milestone item awaits its run: the runbook names the items it
accepts and the evidence each needs. Record each accepted run as a row of the
[acceptance ledger](../docs/acceptance.md).
`TestEveryE2ECaseRendersToValidInput` and
`TestEveryE2ECaseRenderPlansWithoutRefusal` in `cmd/bootwright` render every
case, validate the result and plan each rendered input, so a new case needs
its renders, each service switch its runbook offers included, listed there.

| Case | Guests | CPUs | Memory (GiB) | Disk (GiB) | Accepts |
| --- | --- | --- | --- | --- | --- |
| [lab-rhel](lab-rhel/runbook.md) | 1 | 3 | 6 | 50 | [B75](../specs/milestones/m1.md#b75), [B76](../specs/milestones/m1.md#b76), [B77](../specs/milestones/delivered.md#x246--bounded-managed-os-operator-acceptance), [B876](../specs/milestones/delivered.md#x246--bounded-managed-os-operator-acceptance) |
| [lab-network-variants](lab-network-variants/runbook.md) | 3 | 7 | 14 | 130 | [B74](../specs/milestones/m1.md#b74), [B326](../specs/milestones/m1.md#b326) |
| [lab-3-nodes-ceph](lab-3-nodes-ceph/runbook.md) | 3 | 13 | 26 | 220 | [B626](../specs/milestones/m2.md#b626), one run per release |
| [lab-5-nodes-ceph-networks](lab-5-nodes-ceph-networks/runbook.md) | 5 | 13 | 24 | 210 | none: the owner's live run of managed Networks with a Ceph replication VLAN, a separate user network and an RBD, CephFS and S3 sample server with its client ([X323](../specs/milestones/delivered.md#x323--the-host-networking-batch), [X324](../specs/milestones/delivered.md#x324--the-cluster-networking-batch)) |
| [lab-6-nodes-ceph-network-matrix](lab-6-nodes-ceph-network-matrix/runbook.md) | 6 | 15 | 23.5 | 190 | [B1266](../specs/milestones/delivered.md#x444--owner-acceptance-of-validated-operator-gates), [B74](../specs/milestones/m1.md#b74) and [B326](../specs/milestones/m1.md#b326) with lab-network-variants' cut-off drill, [B1213](../specs/milestones/delivered.md#x444--owner-acceptance-of-validated-operator-gates), [B1215](../specs/milestones/delivered.md#x444--owner-acceptance-of-validated-operator-gates), [B1020](../specs/milestones/delivered.md#x444--owner-acceptance-of-validated-operator-gates), [B1036](../specs/milestones/delivered.md#x444--owner-acceptance-of-validated-operator-gates), [B1044](../specs/milestones/delivered.md#x444--owner-acceptance-of-validated-operator-gates), [B1189](../specs/milestones/delivered.md#x444--owner-acceptance-of-validated-operator-gates), [B1221](../specs/milestones/delivered.md#x444--owner-acceptance-of-validated-operator-gates), [B1223](../specs/milestones/delivered.md#x444--owner-acceptance-of-validated-operator-gates); every network a `Network`, the trunk, legs and routing Bootwright's ([B1281](../specs/milestones/delivered.md#x444--owner-acceptance-of-validated-operator-gates), [B1282](../specs/milestones/m1.md#b1282), [B1283](../specs/milestones/m1.md#b1283)) |
| [lab-5-nodes-ceph-stretch](lab-5-nodes-ceph-stretch/runbook.md) | 5, two contexts | 12 | 20 | 200 | [B486](../specs/milestones/m2.md#b486), [B487](../specs/milestones/m2.md#b487), [B893](../specs/milestones/m2.md#b893), [B894](../specs/milestones/m2.md#b894); the emulated half of [B488](../specs/milestones/m2.md#b488) |
| [lab-sno](lab-sno/runbook.md) | 1 | 9 | 18 | 130 | [B61](../specs/milestones/m3.md#b61), [B1125](../specs/milestones/m4.md#b1125) |
| [lab-sno-ceph-df](lab-sno-ceph-df/runbook.md) | 4, larger host | 25 | 58 | 400 | rehearses the `fdf` path of [B85](../specs/milestones/m4.md#b85) and [B761](../specs/milestones/m4.md#b761) on a single node, as [B1131](../specs/milestones/m4.md#b1131); accepts none |
| [lab-sno-fips-controller](lab-sno-fips-controller/runbook.md) | 2, two contexts on two controllers | 14 | 24 | 200 | [B743](../specs/milestones/delivered.md#x294--a-fips-cluster-built-from-a-controller-off-its-libvirt-host) and [B722](../specs/milestones/delivered.md#x294--a-fips-cluster-built-from-a-controller-off-its-libvirt-host); [B1123](../specs/milestones/delivered.md#x302--two-isolated-environments-on-one-node), [B720](../specs/milestones/delivered.md#x302--two-isolated-environments-on-one-node), [B726](../specs/milestones/delivered.md#x302--two-isolated-environments-on-one-node), [B729](../specs/milestones/delivered.md#x302--two-isolated-environments-on-one-node), [B889](../specs/milestones/delivered.md#x302--two-isolated-environments-on-one-node), [B755](../specs/milestones/delivered.md#x302--two-isolated-environments-on-one-node) and [B757](../specs/milestones/delivered.md#x302--two-isolated-environments-on-one-node); accepted |
| [lab-3-nodes-ocp](lab-3-nodes-ocp/runbook.md) | 3, larger host | 22 | 56 | 400 | [B66](../specs/milestones/m3.md#b66) |
| [lab-3-nodes-ocp-features](lab-3-nodes-ocp-features/runbook.md) | 3, larger host | 28 | 56 | 400 | [B721](../specs/milestones/m3.md#b721), [B724](../specs/milestones/m3.md#b724); IPv6: [B725](../specs/milestones/m3.md#b725), [B703](../specs/milestones/m1.md#b703) |
| [lab-3-nodes-ceph-host-variants](lab-3-nodes-ceph-host-variants/runbook.md) | 3, larger host | 13 | 40 | 430 | [B484](../specs/milestones/m2.md#b484)'s P5 row |

CPUs, memory and disk are what the apply gate requires on the host: the
declared guests plus the provider's reserve
([capacity admission](../specs/substrates.md#capacity-admission)). Memory is
`MemAvailable`, and swap never counts; disk is free space on the filesystem
under `/var/lib/libvirt`. The disk column counts the guests' declared disks and
the reserve only. A case that installs RHEL needs more, and the gate counts it
when it shares that filesystem: what the apply itself writes before the guests
fill their disks, the published package tree, about the DVD's size (about
15.5 GB for RHEL 9.8), and about 3 GB per installed guest, its installer image
as the artifact server publishes it (about 1.5 GB) plus the copy staged in the
provider's virtual-media pool ([B1213](../specs/milestones/delivered.md#x444--owner-acceptance-of-validated-operator-gates)).
A 16-CPU, 32 GiB laptop leaves at most about 28 GiB
available, so the cases marked "larger host" need one. Run one case at a time:
they share the bridge `virbr-e2e`, the guest subnet `198.51.100.0/24` and the
service ports.

## Conventions

Choose one case and its feature settings, then follow its runbook from the
first step through cleanup in one Bash session, from the checkout being tested.
The case tells you when to use the shared steps below and when to return.
Do not execute this README as a second runbook.

Begin with the case's contexts and guest resources absent. Shared host tools
and cached media may already exist. Never delete a context's local state while
its resources still need removal.

A successful run completes every step and expected check for the selected
settings, including deliberate failure drills and cleanup, on one clean build.
An unexpected failure stops the run; retain its evidence and cleanup records.
After correcting the problem and cleaning up that attempt, start a new complete
run. A partial run or a run that skips steps is not successful. Test another
feature combination or build in another complete run.

Use these names consistently:

| Variable | Meaning |
| --- | --- |
| `CONTEXT` | Bootwright context being tested |
| `INPUT_DIR` | Rendered desired-state directory |
| `CONTROLLER_ADDRESS` | Host address on which the services listen |
| `RUN_DIR` | Private temporary directory for case files and evidence |
| `EVIDENCE_CONTEXT` | Context whose completed run is being preserved before purge |
| `HOST_CHECKS` | Private record of diagnostic packages installed for this run |

Keep the same shell and private directories through cleanup. Configure inputs
where the runbook introduces them; prompts ask for values discovered on the
host. Credentials come from private files, never literal values in commands.
Record results using the [acceptance ledger](../docs/acceptance.md); only the
human owner accepts a run, and confidential environment details stay in the
private evidence archive.

## Preserve evidence before context purge

After destroy and the final status check, while no command is active for the
context, set `EVIDENCE_CONTEXT` as the case directs and run this Bash block
from the checkout. Context purge removes operation and power logs; it makes no
archive. This step keeps only their logs and adapter output plus final status,
outside `RUN_DIR`, under a Git-verified ignored shared-cache directory, so
removing a task worktree keeps the evidence. It excludes input
revisions, keyrings, frozen plans and records, registry and SSH-trust data.
Bounded power output retains only the newest 16 runs; preserve earlier
checkpoints separately when the case needs them.

```bash
(
    set -euo pipefail
    umask 077
    : "${EVIDENCE_CONTEXT:?Set the context to preserve}"
    sudo -v
    EVIDENCE_CACHE=$(./scripts/cache-dir)
    EVIDENCE_ROOT="$EVIDENCE_CACHE/e2e-evidence"
    git -C "$(dirname -- "$EVIDENCE_CACHE")" check-ignore -q -- "$EVIDENCE_ROOT/.ignore-check"
    test ! -L "$EVIDENCE_ROOT"
    mkdir -p -m 0700 -- "$EVIDENCE_ROOT"
    chmod 0700 -- "$EVIDENCE_ROOT"
    EVIDENCE_DIR=$(mktemp -d "$EVIDENCE_ROOT/${EVIDENCE_CONTEXT}.XXXXXXXX")
    LOG_DIR="/var/log/bootwright/contexts/$EVIDENCE_CONTEXT"
    ./bin/bootwright status --context "$EVIDENCE_CONTEXT" --output json > "$EVIDENCE_DIR/status.json"
    sudo bash -euo pipefail -c '
        find "$1/operations" -mindepth 1 -maxdepth 1 -type d -print0 |
            sort -z | while IFS= read -r -d "" LOGS; do
                OPERATION_ID=$(basename -- "$LOGS")
                DIGEST=$(find "$LOGS" -type f -print0 | sort -z | xargs -0 sha256sum | sha256sum)
                printf "%s\t%s\t%s\n" "$OPERATION_ID" "$LOGS" "$DIGEST"
            done
    ' _ "$LOG_DIR" > "$EVIDENCE_DIR/operation-log-digests.tsv"
    test -s "$EVIDENCE_DIR/operation-log-digests.tsv"
    sudo bash -euo pipefail -c '
        cd -- "$1"
        find operations runs -type f -print0 |
            sort -z | tar -cf - --null --no-recursion -T -
    ' _ "$LOG_DIR" | gzip > "$EVIDENCE_DIR/logs.tar.gz"
    cd -- "$EVIDENCE_DIR"
    gzip -t logs.tar.gz
    tar -tzf logs.tar.gz > archive-files.txt
    test -s archive-files.txt
    sha256sum logs.tar.gz status.json archive-files.txt operation-log-digests.tsv > SHA256SUMS
    sha256sum -c SHA256SUMS
    printf 'Evidence verified and retained: %s\n' "$EVIDENCE_DIR"
)
```

Proceed to purge only after this succeeds. On failure, keep the context and
the partial evidence directory, correct the cause and repeat into a new
directory. The private mapping records each original log path and its
[acceptance digest](../docs/acceptance.md) before purge; archive checksums
protect the retained bytes. Retain `.cache/e2e-evidence` through temporary-file
cleanup and owner acceptance. No live acceptance is implied by archiving.

## Prepare the host

Complete these steps once, when the case asks for host preparation.

### 1. Check the host and build tools

Use a [supported](../specs/controller.md#supported-hosts) Fedora or RHEL amd64
host with `/dev/kvm`, network egress and the required
[sudo policy](../docs/operator-guide.md#the-sudo-rule). Building needs `make`
and the [Go toolchain](../docs/development.md#toolchain). Rendering needs Helm:
install it from approved host packages (`sudo dnf install helm` on Fedora) or
[verified upstream binaries](https://helm.sh/docs/intro/install/).

```sh
test -e /dev/kvm
command -v make
helm version --short
sudo -v
```

### 2. Build and install controller prerequisites

Enter the checkout containing the code being tested before these commands.
Require the binary's `Commit` to match `HEAD` and its `Source` to be `clean`.
Use this checkout's binary throughout the run.

```sh
git branch --show-current
git rev-parse HEAD
make build
./bin/bootwright version
./bin/bootwright controller install --yes
```

`controller install` installs Bootwright's runtime and baseline execution
packages. The case's later `apply --stage controller` installs its selected
libvirt, QEMU, TPM and installer tools and binds the context to this host. No
manual installation of those packages is needed.

### 3. Prepare diagnostic tools

The runbooks use `curl`, `dig`, `chronyd` and `jq` for independent checks.
Record pre-existing packages, then install only missing diagnostic packages.
Keep the printed directory until the final cleanup step.

```bash
umask 077
HOST_CHECKS=$(mktemp -d)
printf 'Diagnostic package record: %s\n' "$HOST_CHECKS"
(
    set -e
    rpm -qa --qf '%{NAME}\n' > "$HOST_CHECKS/before"
    : > "$HOST_CHECKS/added"
    for package in bind-utils chrony jq; do
        if ! grep -Fxq "$package" "$HOST_CHECKS/before"; then
            printf '%s\n' "$package" >> "$HOST_CHECKS/added"
        fi
    done
    if ! command -v curl >/dev/null; then
        if grep -Eq '^(curl|curl-minimal)$' "$HOST_CHECKS/before"; then
            printf 'Restore the installed curl executable before starting the run.\n' >&2
            exit 1
        fi
        printf 'curl\n' >> "$HOST_CHECKS/added"
    fi
    mapfile -t packages < "$HOST_CHECKS/added"
    if ((${#packages[@]})); then
        sudo dnf install --setopt=assumeyes=False --setopt=defaultyes=False \
            --setopt=install_weak_deps=False -- "${packages[@]}"
    fi
    for tool in curl dig chronyd jq; do
        command -v "$tool" >/dev/null
    done
)
```

Review the package transaction before confirming. Preserve any working
`curl-minimal` installation. Additional case-specific tools are named in the
case's preparation step.

### 4. Select managed host firewall preparation

The case templates select `Machine.spec.firewall.management: managed` on the
controller and its provided libvirt host. The controller stage installs,
enables and starts firewalld, resolves each listener's interface and runtime
zone, and publishes owned permanent and runtime admission before services
start. No manual firewalld setup is needed. The shared daemon remains enabled
after cleanup; destroy removes only the context's owned listener services.
An existing firewall managed by another service requires an administrator's
migration before opting in. Unrelated runtime-only rules are preserved: a
reload which would discard them refuses rather than overwriting that policy.

### 5. Check ports, address and capacity

The case lists its ports and capacity. Inspect listeners after diagnostic
installation, since those packages can affect host services:

```sh
sudo ss -lntup | grep -E ':(53|123|3128|808[0-9]|844[0-9]|800[0-9])\b'
grep MemAvailable /proc/meminfo
df -h /var/lib
ip -br address
```

No output from the listener filter (exit 1) means no matching listener.
Require the case's ports to be free on its bind addresses. Resolve conflicts
through the service owner's configuration before starting, keeping any changes
needed to restore it at cleanup. Require the table's available RAM and free
disk on the filesystem that will hold `/var/lib/libvirt`; swap does not count.
Keep the controller address stable for the entire run, for example with a DHCP
reservation. The render's `host.address: auto` uses the default-route source.

### 6. Add RHEL media

For a RHEL case, including Ceph, provide both RHEL 9.8
x86_64 ISOs. OpenShift cases skip this step. If media is already present,
verify its name and checksum with `media list --checksums`.

```bash
read -r -p 'Absolute path to the RHEL boot ISO: ' BOOT_ISO
read -r -p 'Absolute path to the RHEL DVD ISO: ' DVD_ISO
./bin/bootwright media add --name rhel-9.8-x86_64-boot.iso --from-file "${BOOT_ISO:?Enter the boot ISO path}"
./bin/bootwright media add --name rhel-9.8-x86_64-dvd.iso --from-file "${DVD_ISO:?Enter the DVD ISO path}"
./bin/bootwright media list --checksums
```

### 7. Prepare credential files

Supply the files required by the selected case and features, owned by you with
mode `0600` or `0400`. Adjust the file paths in the case's input block.

| Default path | Variable | Content | Cases |
| --- | --- | --- | --- |
| `~/rhel-org.txt` | `RHSM_ORG_FILE` | RHSM organization ID, without a final line feed | RHEL registration, Ceph host variants |
| `~/rhel-activation-key.txt` | `RHSM_KEY_FILE` | RHSM activation key | RHEL registration, Ceph host variants |
| `~/ibm-ceph-registry.txt` | `IBM_REGISTRY_FILE` | `cp:<entitlement key>` for `cp.icr.io`, or the key alone | Ceph |
| `~/openshift-pull-secret` | `PULL_SECRET_FILE` | OpenShift pull-secret JSON | OpenShift |

For each required file:

```bash
read -r -p 'Absolute path to a required credential file: ' SECRET_FILE
chmod 600 -- "${SECRET_FILE:?Enter the credential file path}"
```

Host preparation is complete. Return to the next step of the case runbook.

## Prepare listener firewall

The case's `apply --stage controller` prepares its explicitly managed host
firewalls, including both the runtime and permanent grants derived from the
context's effective listeners. Port overrides need no separate list, and a
reload retains these grants. Run the case's next preflight step and require
`ready` before starting its services. Preflight and plan remain read-only.

A missing grant on an operator-managed host still refuses with its manual
remedy. A managed bridge's trusted zone does not replace inspection of the
interface holding the controller listener address.

## Restore listener firewall

The case's destroy stops consumers and removes its owned firewalld services
and memberships. It preserves other contexts' grants, operator rules and the
shared daemon's installation and enablement. Require destroy to complete
before removing the case's evidence directory; no firewall-cmd cleanup is
needed.

## Remove diagnostic tools

After the case's verification and resource cleanup, remove only the diagnostic
packages recorded as absent before this run:

```bash
(
    set -e
    mapfile -t packages < "${HOST_CHECKS:?Keep HOST_CHECKS until cleanup completes}/added"
    if ((${#packages[@]})); then
        sudo dnf remove --setopt=assumeyes=False --setopt=defaultyes=False \
            --setopt=clean_requirements_on_remove=False -- "${packages[@]}"
    fi
)
```

Review the transaction. Confirm only if every removed package appears in the
saved `added` list; cancel if it removes a pre-existing package or another
workload's dependency. Dependencies installed alongside the diagnostics stay
installed. Keep the record if removal fails or is declined. After successful
removal:

```sh
rm -r -- "$HOST_CHECKS"
```

Return to the case's final cleanup and result step. The shared controller
installation, build tools, firewalld and cached media remain available for
later cases.

[Removing Bootwright from the host](../docs/operator-guide.md#uninstalling-bootwright-from-this-host)
uses `./bin/bootwright controller uninstall` after every environment is
destroyed, every context deleted and all imported media removed. That command
removes the shared controller state and install-owned packages; it belongs
to host removal after testing, beyond an individual case's cleanup.
