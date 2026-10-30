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

| Case | Guests | CPUs | Memory (GiB) | Disk (GiB) | Accepts |
| --- | --- | --- | --- | --- | --- |
| [lab-rhel](lab-rhel/runbook.md) | 1 | 3 | 6 | 50 | [B75](../specs/milestones/m1.md#b75), [B76](../specs/milestones/m1.md#b76), [B77](../specs/milestones/m1.md#b77), [B876](../specs/milestones/m1.md#b876) |
| [lab-baremetal-rehearsal](lab-baremetal-rehearsal/runbook.md) | 1, two contexts | 5 | 10 | 70 | [B73](../specs/milestones/m1.md#b73), [B496](../specs/milestones/m1.md#b496) |
| [lab-network-variants](lab-network-variants/runbook.md) | 3 | 7 | 14 | 130 | [B74](../specs/milestones/m1.md#b74), [B326](../specs/milestones/m1.md#b326) |
| [lab-3-nodes-ceph](lab-3-nodes-ceph/runbook.md) | 3 | 13 | 26 | 220 | [B626](../specs/milestones/m2.md#b626), one run per release |
| [lab-5-nodes-ceph-stretch](lab-5-nodes-ceph-stretch/runbook.md) | 5, two contexts | 12 | 20 | 200 | [B486](../specs/milestones/m2.md#b486), [B487](../specs/milestones/m2.md#b487), [B893](../specs/milestones/m2.md#b893), [B894](../specs/milestones/m2.md#b894); the emulated half of [B488](../specs/milestones/m2.md#b488) |
| [lab-sno](lab-sno/runbook.md) | 1 | 9 | 18 | 130 | [B61](../specs/milestones/m3.md#b61), [B755](../specs/milestones/m4.md#b755), [B757](../specs/milestones/m4.md#b757) |
| [lab-3-nodes-ocp](lab-3-nodes-ocp/runbook.md) | 3, larger host | 22 | 56 | 400 | [B66](../specs/milestones/m3.md#b66) |
| [lab-3-nodes-ocp-features](lab-3-nodes-ocp-features/runbook.md) | 3, larger host | 28 | 56 | 400 | [B720](../specs/milestones/m3.md#b720), [B721](../specs/milestones/m3.md#b721), [B724](../specs/milestones/m3.md#b724); IPv6: [B725](../specs/milestones/m3.md#b725), [B703](../specs/milestones/m1.md#b703) |
| [lab-3-nodes-ceph-host-variants](lab-3-nodes-ceph-host-variants/runbook.md) | 3, larger host | 13 | 40 | 430 | [B484](../specs/milestones/m2.md#b484)'s P5 row |

CPUs, memory and disk are what the apply gate requires on the host: the
declared guests plus the provider's reserve
([capacity admission](../specs/substrates.md#capacity-admission)). Memory is
`MemAvailable`, and swap never counts; disk is free space on the filesystem
under `/var/lib/libvirt`. A 16-CPU, 32 GiB laptop leaves at most about 28 GiB
available, so the last three cases need a larger host. Run one case at a time:
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
| `RUN_DIR` | Private temporary directory for case files and firewall cleanup records |
| `EVIDENCE_CONTEXT` | Context whose completed run is being preserved before purge |
| `HOST_CHECKS` | Private record of diagnostic packages installed for this run |
| `FIREWALL_PORTS` | Case's array of listener ports to admit |

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
    STATE_DIR="/var/lib/bootwright/contexts/$EVIDENCE_CONTEXT/state"
    ./bin/bootwright status --context "$EVIDENCE_CONTEXT" --output json > "$EVIDENCE_DIR/status.json"
    sudo bash -euo pipefail -c '
        find "$1/operations" -mindepth 2 -maxdepth 2 -type d -name logs -print0 |
            sort -z | while IFS= read -r -d "" LOGS; do
                OPERATION_ID=$(basename -- "$(dirname -- "$LOGS")")
                DIGEST=$(find "$LOGS" -type f -print0 | sort -z | xargs -0 sha256sum | sha256sum)
                printf "%s\t%s\t%s\n" "$OPERATION_ID" "$LOGS" "$DIGEST"
            done
    ' _ "$STATE_DIR" > "$EVIDENCE_DIR/operation-log-digests.tsv"
    test -s "$EVIDENCE_DIR/operation-log-digests.tsv"
    sudo bash -euo pipefail -c '
        cd -- "$1"
        find operations runs -type f \( -path "operations/*/logs/*" -o -path "runs/*/run.output" \) -print0 |
            sort -z | tar -cf - --null --no-recursion -T -
    ' _ "$STATE_DIR" | gzip > "$EVIDENCE_DIR/logs.tar.gz"
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

### 4. Prepare firewalld

Use a lab host selected to use firewalld. An existing firewall managed by
another service must be migrated by its administrator before this run.

```sh
if ! rpm -q firewalld; then
    sudo dnf install firewalld
fi
sudo systemctl enable --now firewalld.service
sudo systemctl is-enabled firewalld.service
sudo firewall-cmd --state
```

Require `enabled` and `running`. Bootwright checks this prerequisite but does
not enable the host firewall. Firewalld stays installed and enabled after the
case. Listener rules are prepared later, after the context's controller stage.

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

For a RHEL case, including Ceph and baremetal rehearsal, provide both RHEL 9.8
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

Run this step when the case asks, after `apply --stage controller` and before
context preflight. The case has set `CONTEXT`, `CONTROLLER_ADDRESS`, `RUN_DIR`
and `FIREWALL_PORTS`. For two contexts, the array contains both plans' listener
ports; loopback BMC ports need no opening.

Inspect the plan and find the interface carrying the printed listener address:

```sh
./bin/bootwright plan --context "${CONTEXT:?Set CONTEXT in the case runbook}"
printf 'Listener address: %s\n' "${CONTROLLER_ADDRESS:?Set CONTROLLER_ADDRESS in the case runbook}"
ip -br address
sudo firewall-cmd --get-active-zones
sudo firewall-cmd --get-default-zone
```

Enter the interface name, then its runtime zone. If the first query reports
`no zone` (exit 2), enter the default zone shown above. Any other query failure
stops the run.

```bash
read -r -p "Interface carrying $CONTROLLER_ADDRESS: " FIREWALL_INTERFACE
sudo firewall-cmd --get-zone-of-interface="${FIREWALL_INTERFACE:?Enter the interface name}"
read -r -p 'Runtime zone for that interface: ' FIREWALL_ZONE
sudo firewall-cmd --zone="${FIREWALL_ZONE:?Enter the zone name}" --list-all
```

The following rules admit every client reaching this zone. Use a lab interface
whose reachable clients are appropriate for these services, as described in
[network exposure](../docs/operator-guide.md#network-exposure). The inspector
requires an accepting zone or explicit port/range admission. Rich rules,
source-selected zones and unsupported policies remain `unknown`; preserve them
and prepare a suitable lab interface before starting the run. A managed bridge
in `trusted` does not replace this check of the listener's host interface.

Review `FIREWALL_PORTS` against the plan, including any port overrides. Save
which ports were absent before adding rules, so cleanup preserves existing
grants and ranges:

```bash
(
    set -e
    : "${RUN_DIR:?Set RUN_DIR in the case runbook}"
    : "${FIREWALL_ZONE:?Enter the runtime zone}"
    : "${FIREWALL_PORTS[0]:?Set FIREWALL_PORTS in the case runbook}"
    test ! -e "$RUN_DIR/firewall-added-ports"
    sudo firewall-cmd --zone="$FIREWALL_ZONE" --list-all > "$RUN_DIR/firewall-before"
    : > "$RUN_DIR/firewall-added-ports.pending"
    for socket in "${FIREWALL_PORTS[@]}"; do
        if sudo firewall-cmd --zone="$FIREWALL_ZONE" --query-port="$socket"; then
            continue
        else
            query_status=$?
            [ "$query_status" -eq 1 ] || exit "$query_status"
            printf '%s\n' "$socket" >> "$RUN_DIR/firewall-added-ports.pending"
        fi
    done
    printf '%s\n' "$FIREWALL_ZONE" > "$RUN_DIR/firewall-zone"
    mv "$RUN_DIR/firewall-added-ports.pending" "$RUN_DIR/firewall-added-ports"
    while IFS= read -r socket; do
        sudo firewall-cmd --zone="$FIREWALL_ZONE" --add-port="$socket"
    done < "$RUN_DIR/firewall-added-ports"
)
```

These are runtime rules; keep the host running without a firewall reload
through the case. No permanent rule or zone assignment is changed. Return to
the case's preflight step and require `ready` before starting services.

## Restore listener firewall

Run this after destroying every context in the case and before removing
`RUN_DIR`. Review `firewall-before`, the recorded ports and the current zone.
Remove only grants this run added; a grant another workload now needs must be
retained and recorded as incomplete cleanup.

```bash
(
    set -e
    : "${RUN_DIR:?Keep RUN_DIR until cleanup completes}"
    if [ -f "$RUN_DIR/firewall-added-ports" ]; then
        FIREWALL_ZONE=$(cat "$RUN_DIR/firewall-zone")
        sudo firewall-cmd --zone="$FIREWALL_ZONE" --list-all
        while IFS= read -r socket; do
            if sudo firewall-cmd --zone="$FIREWALL_ZONE" --query-port="$socket"; then
                sudo firewall-cmd --zone="$FIREWALL_ZONE" --remove-port="$socket"
            else
                query_status=$?
                [ "$query_status" -eq 1 ] || exit "$query_status"
            fi
        done < "$RUN_DIR/firewall-added-ports"
    fi
)
```

Keep the record until cleanup succeeds. Leave shared firewalld service state
and permanent rules in place. Return to the case's next cleanup step.

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
