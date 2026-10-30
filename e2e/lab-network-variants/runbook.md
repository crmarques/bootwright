# lab-network-variants

Three RHEL 9.8 guests, each installing over a different install interface and
then converging to the rest of its declared network, beside managed DNS, NTP,
proxy and artifact services on the controller. Context `lab-network-variants`;
3 guests of 2 vCPU, 4 GiB and a 40 GiB disk, each behind its own emulated
Redfish BMC on loopback ports 8000 to 8002. Bond mode is `active-backup`, the
only mode a libvirt Machine admits.

| Machine | Install interface | Attachment | Install address | Gateway | Guest-only VLAN 140 |
| --- | --- | --- | --- | --- | --- |
| `nv-bond` | `bond0` over `nic0` and `nic1` | `nv-lab`, managed, bridge `virbr-e2e` | 198.51.100.11/26 | 198.51.100.1 | `bond0.140`, 203.0.113.11/24, MTU 1400 |
| `nv-vlan` | `nic0.120` over `nic0` | `nv-trunk`, external, bridge `br-nv` | 198.51.100.71/26 | 198.51.100.65 | `nic0.140`, 203.0.113.12/24, MTU 1400 |
| `nv-vlan-bond` | `bond0.130` over `bond0` over `nic0` and `nic1` | `nv-trunk` | 198.51.100.141/26 | 198.51.100.129 | `bond0.140`, 203.0.113.13/24, MTU 1400 |

Each Machine also routes 192.0.2.0/24 through its gateway on its install
interface and searches `e2e.example.test`. The MTU is a Machine override beside
its addresses; the routes stay in each NetworkConfig. Every Machine installs,
converges to its declared network and power-cycles once.

The acceptance table at the end names the gates this case can cover. Features
are optional; selecting them never removes either interruption drill.

## 1. Review the inputs

Set these choices once in the Bash session used for the whole run. All four
default to `false`. Updates within the release require registration and direct
guest access to the subscription and content services; the lab proxy alone
does not provide that access. If the guard reports an invalid combination,
correct the choices and repeat this block before continuing:

```bash
set +e  # Keep expected refusal and Ctrl-C statuses from ending this interactive session.
FIPS=false
TPM2=false
REGISTER=false
UPDATE_WITHIN_RELEASE=false
RHSM_ORG_FILE="$HOME/rhel-org.txt"
RHSM_KEY_FILE="$HOME/rhel-activation-key.txt"
if [ "$UPDATE_WITHIN_RELEASE" = true ] && [ "$REGISTER" != true ]; then
    printf 'UPDATE_WITHIN_RELEASE=true requires REGISTER=true; stop and correct the choices.\n' >&2
    unset FEATURE_ARGS
    false
else
    FEATURE_ARGS=(--set "security.fips=$FIPS" --set "security.tpm2=$TPM2"
        --set "registration.enabled=$REGISTER"
        --set "registration.updateWithinRelease=$UPDATE_WITHIN_RELEASE")
    declare -A ROOT_PARTITIONS=()
fi
```

`TPM2=true` encrypts each guest's root disk and binds its unlock to TPM2. The
baseline Machines already have a vTPM; its presence alone does not prove root
encryption. With registration enabled, prepare the two private
[RHSM credential files](../README.md#7-prepare-credential-files). Never put
credential values in shell arguments or rendered input. Keep the same
`FEATURE_ARGS` at every render, including the negative and corrected cases.

| `--set` | Default | Effect |
| --- | --- | --- |
| `bond.mode=802.3ad` | `active-backup` | A mode the libvirt Machine refuses: the refusal drill |
| `name=nv-cutoff` with `single=true` | `lab-network-variants`, `false` | The single-Machine context of the cut-off drill: only `nv-bond`, on the managed network |
| `cutoff=true` | `false` | With `single`: a route that sends the controller's replies the wrong way |
| `security.fips=true` | `false` | FIPS mode and ECDSA fleet SSH key |
| `security.tpm2=true` | `false` | TPM2-bound root encryption and a generated recovery passphrase |
| `registration.enabled=true` | `false` | Managed RHSM registration with the private organization and activation key |
| `registration.updateWithinRelease=true` | `false` | Updates pinned to the declared RHEL release; requires registration |

[values.yaml](values.yaml) holds the rest: addresses, upstreams, media names,
guest size, the VLAN ids and networks and the converged network.

## 2. Prepare the host and external bridge

Complete [host preparation](../README.md#prepare-the-host), including diagnostic
tools and both RHEL ISOs, then return here. Free the loopback ports 8000 to 8002. The
managed network 198.51.100.0/26 on `virbr-e2e` must not exist yet: the
`substrates` stage creates it and destroy removes it, and Bootwright places it
in firewalld's `trusted` zone
([provider host realization](../../specs/substrates.md#provider-host-realization)),
for guest traffic. The listener firewall check still follows the controller
stage below.

Capacity, which the apply gate counts
([capacity admission](../../specs/substrates.md#capacity-admission)): the
three guests plus the provider's `capacityReserve` (1 CPU, 2 GiB, 10 GiB)
need 7 host CPUs not reserved by other libvirt domains, 14 GiB of
`MemAvailable` in `/proc/meminfo` (swap never counts) and 130 GiB free on the
filesystem backing `/var/lib/libvirt/images`, beside the media. Check with
`nproc`, `grep MemAvailable /proc/meminfo` and
`df -h /var/lib/libvirt/images`.

The VLAN Machines use an operator-created bridge. Require `br-nv`,
`br-nv.120` and `br-nv.130` to be absent before starting. Create these isolated
connections without a physical port; their `trusted` zone admits the lab
VLANs to host services. Do not attach this bridge to an external network.
Bootwright neither creates nor removes these connections.

When `REGISTER=true`, use NetworkManager sharing on the two downstream VLAN
profiles to provide guest outbound NAT through the host's existing uplink.
Require the `dnsmasq` binary, no conflicting DNS listener on either VLAN
gateway address (including a wildcard listener), and no conflicting DHCP
listener on their interfaces. Inspect ports 53 and 67 and require an enabled
NetworkManager `firewall-backend` (`nftables` or `iptables`, including automatic
selection; not `none`). The profile's existing address becomes the guest's
gateway; set no `ipv4.gateway` on these host profiles. NetworkManager owns
the sharing rules and helper lifetime, and `trusted` still admits host
services. See [shared IPv4 settings](https://networkmanager.dev/docs/api/latest/settings-ipv4.html),
[the NAT backend](https://networkmanager.dev/docs/api/latest/NetworkManager.conf.html)
and [Red Hat's sharing description](https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/9/html/configuring_and_managing_networking/proc_configuring-rhel-as-a-wpa2-or-wpa3-personal-access-point_configuring-and-managing-networking).
Without registration, retain the original manual profiles:

```bash
VLAN_IPV4_METHOD=manual
if [ "$REGISTER" = true ]; then
    command -v dnsmasq
    sudo ss -lntup '( sport = :53 or sport = :67 )'
    sudo NetworkManager --print-config
    VLAN_IPV4_METHOD=shared
fi
```

Inspect those prerequisite results before creating profiles. Install a missing
`dnsmasq` through the host's package manager and resolve conflicts first;
record any package added for removal with the diagnostic tools at cleanup.

```sh
sudo nmcli connection add type bridge ifname br-nv con-name br-nv bridge.stp no ipv4.method disabled ipv6.method disabled connection.zone trusted
sudo nmcli connection add type vlan ifname br-nv.120 con-name br-nv.120 vlan.parent br-nv vlan.id 120 ipv4.method "$VLAN_IPV4_METHOD" ipv4.addresses 198.51.100.65/26 ipv6.method disabled connection.zone trusted
sudo nmcli connection add type vlan ifname br-nv.130 con-name br-nv.130 vlan.parent br-nv vlan.id 130 ipv4.method "$VLAN_IPV4_METHOD" ipv4.addresses 198.51.100.129/26 ipv6.method disabled connection.zone trusted
sudo nmcli connection up br-nv
sudo nmcli connection up br-nv.120
sudo nmcli connection up br-nv.130
ip -br address show
sudo firewall-cmd --get-zone-of-interface=br-nv.120
sudo firewall-cmd --get-zone-of-interface=br-nv.130
```

Require both VLAN addresses above and `trusted` for both zone queries. These
host commands need verification on the test host; no live result is claimed.

## 3. Render and initialize

```sh
umask 077
RUN_DIR=$(mktemp -d)
e2e/render.sh lab-network-variants "${FEATURE_ARGS[@]}"
CONTEXT="lab-network-variants"
INPUT_DIR="$HOME/bootwright-e2e/lab-network-variants"
CONTROLLER_ADDRESS=$(ip -4 route get 192.0.2.1 | sed -n 's/.* src \([0-9.]*\).*/\1/p')
./bin/bootwright validate -f "$INPUT_DIR"
./bin/bootwright context init --name "$CONTEXT" --input-dir "$INPUT_DIR"
./bin/bootwright secret generate --context "$CONTEXT"
if [ "$REGISTER" = true ]; then
    ./bin/bootwright secret set --context "$CONTEXT" --name rhsm-org --value-file "${RHSM_ORG_FILE:?Set RHSM_ORG_FILE}" --yes
    ./bin/bootwright secret set --context "$CONTEXT" --name rhsm-key --value-file "${RHSM_KEY_FILE:?Set RHSM_KEY_FILE}" --yes
fi
./bin/bootwright secret check --context "$CONTEXT"
./bin/bootwright plan --context "$CONTEXT"
```

## 4. Verify the unsupported bond mode is refused

A libvirt Machine admits only `active-backup`; render the case with `bond.mode=802.3ad` into a
second directory, which validates, and import it:

```sh
e2e/render.sh lab-network-variants "${FEATURE_ARGS[@]}" --set bond.mode=802.3ad --out-dir ~/bootwright-e2e/lab-network-variants-mode
./bin/bootwright context update --name "$CONTEXT" --input-dir ~/bootwright-e2e/lab-network-variants-mode --yes
./bin/bootwright plan --context "$CONTEXT"
```

Require the plan to report "a libvirt bridge qualifies only active-backup
bonding, and bond bond0 of Machine/nv-bond declares mode 802.3ad". This is the
expected negative test. Restore the valid declaration before proceeding:

```sh
./bin/bootwright context update --name "$CONTEXT" --input-dir "$INPUT_DIR" --yes
```

## 5. Prepare the controller and services

```sh
./bin/bootwright apply --context "$CONTEXT" --stage controller --yes
```

Set the listener ports, then complete
[Prepare listener firewall](../README.md#prepare-listener-firewall).

```bash
FIREWALL_PORTS=(53/tcp 53/udp 123/udp 3128/tcp 8080/tcp 8443/tcp)
```

Continue only when the following preflight reports `ready`:

```sh
./bin/bootwright preflight controller --context "$CONTEXT"
./bin/bootwright apply --context "$CONTEXT" --stage infrastructure-services --yes
```

The plan shows, for each Machine, the impacts `configure-network` and `restart`,
each followed by the Machine name. During installation, live progress describes
network convergence as `apply the declared network, restart the machine and
prove its network`. The services answer on the host address `CONTROLLER_ADDRESS`:

```sh
curl -sk "https://$CONTROLLER_ADDRESS:8443/" -o /dev/null -w '%{http_code}\n'   # 404 on an empty root
dig @"$CONTROLLER_ADDRESS" +short controller.e2e.example.test
chronyd -Q -t 10 "server $CONTROLLER_ADDRESS iburst port 123"
```

## 6. Install with the required interruption drills

With `TPM2=true`, `secret generate` creates a context-specific `luks-recovery`
passphrase from 32 random bytes. Keep that value unchanged while its guests
exist; the recovery check below reads it only into a private temporary file.

Run two separate interruption drills (B74 and B326). Widen the terminal until
the progress descriptions below are readable in full. The three guests run in
parallel: note the Machine names and each one's progress when interrupting.
Watch each checkpoint while apply is running. Missing either interruption
requires a new complete run; a completed install does not replace the drill.

First, interrupt installation (B74). Start this command on its own:

```sh
./bin/bootwright apply --context "$CONTEXT" --yes
```

Leave apply running until a `[RUNNING]` Machine row contains this exact text:

```text
wait for the installer to write the disk and power the machine off
```

**At that moment, while apply is still running, press Ctrl-C once.** Wait for
the shell prompt to return. `runtime.interrupted` and the interrupted install
blocks becoming `unknown` are expected at this checkpoint. Then inspect the
interrupted operation and power with these commands:

```sh
./bin/bootwright status --context "$CONTEXT"
./bin/bootwright machine list --context "$CONTEXT" --power-status
```

Cancellation leaves an already booted installer running on its guest. An apply
retry first observes the unknown blocks. While a powered-on guest still has no
readable installation marker, this may return `lifecycle.unknown`; it does not
resume the cancelled installer wait. Before retrying, wait for the
interrupted installer guests to power off naturally;
repeat the power listing above until each reports `off`. A powered-on guest
without an installation marker remains unknown and cannot be replayed. If an
explicit stop is needed, use `machine stop` with only the exact names confirmed
still in the installer wait; never select guests merely because they are on
or stop all three, since another may already be converging its network. Prove
those guests `off` before retrying. Keep any runtime refusal in force.

Second, after the power-off checkpoint, resume apply and interrupt network
convergence (B326). The replay may
repeat installation for powered-off incomplete guests; guests with the matching
installation marker continue their unfinished work. Start this command on its own:

```sh
./bin/bootwright apply --context "$CONTEXT" --yes
```

Leave apply running until a `[RUNNING]` Machine row contains this exact text:

```text
apply the declared network, restart the machine and prove its network
```

**At that moment, press Ctrl-C once.** Wait for the shell prompt to return,
then inspect status and power:

```sh
./bin/bootwright status --context "$CONTEXT"
./bin/bootwright machine list --context "$CONTEXT" --power-status
```

Require the interrupted Machine's network convergence to remain unfinished,
never realized. For other guests still in their installer waits when this
interruption occurred, repeat the powered-off checkpoint above before retrying.
Repeat apply to converge it, then continue to verification:

```sh
./bin/bootwright apply --context "$CONTEXT" --yes
```

## 7. Verify installation and final network

B74, the install shapes. The first command lists one active profile per
declared interface and the second shows `active-backup` over `nic0` and `nic1`
on the two bonded Machines. The VLAN commands show `nic0.120` on `nv-vlan`,
`bond0.130` over `bond0` on `nv-vlan-bond`, and every interface name is pinned
to its MAC address:

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name nv-bond,nv-vlan,nv-vlan-bond -- nmcli -t -f NAME,DEVICE,TYPE,STATE connection show --active
./bin/bootwright machine exec --context "$CONTEXT" --name nv-bond,nv-vlan-bond -- cat /proc/net/bonding/bond0
./bin/bootwright machine exec --context "$CONTEXT" --name nv-vlan,nv-vlan-bond -- ip -d link show type vlan
./bin/bootwright machine exec --context "$CONTEXT" --name nv-bond,nv-vlan,nv-vlan-bond -- ip -br link show
virsh --connect qemu:///system domiflist bootwright-lab-network-variants-nv-bond
virsh --connect qemu:///system domiflist bootwright-lab-network-variants-nv-vlan
virsh --connect qemu:///system domiflist bootwright-lab-network-variants-nv-vlan-bond
```

On `nv-bond` and `nv-vlan-bond`, match the `Permanent HW addr` under each
`Slave Interface: nic0` and `Slave Interface: nic1` in `/proc/net/bonding/bond0`
to that Machine's `domiflist` MACs in order. Active-backup bond members may
share the bond's current MAC in `ip -br link`. On unbonded `nv-vlan`, compare
the `nic0` MAC from `ip -br link` with its `domiflist` MAC.

B326, the final network. Each Machine shows the second address and MTU 1400 on
its guest-only VLAN, the default and 192.0.2.0/24 routes, the search
`e2e.example.test`, and a marker naming the request:

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name nv-bond,nv-vlan,nv-vlan-bond -- ip -br address show
./bin/bootwright machine exec --context "$CONTEXT" --name nv-bond,nv-vlan,nv-vlan-bond -- ip route show
./bin/bootwright machine exec --context "$CONTEXT" --name nv-bond,nv-vlan,nv-vlan-bond -- cat /etc/resolv.conf
./bin/bootwright machine exec --context "$CONTEXT" --name nv-bond -- ip -o link show bond0.140
./bin/bootwright machine exec --context "$CONTEXT" --name nv-bond,nv-vlan,nv-vlan-bond -- cat /etc/bootwright/network-marker.json
./bin/bootwright diff --context "$CONTEXT" --observe
```

B326, the reboot. The convergence power-cycled each Machine after the marker
was written. Compare each Machine's marker `bootId` from the preceding check
with its running boot ID below: they differ while the declared network is up,
and `./bin/bootwright diff --context "$CONTEXT" --observe` reads every Machine
realized. The Machine info command confirms lifecycle `applied`:

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name nv-bond,nv-vlan,nv-vlan-bond -- cat /proc/sys/kernel/random/boot_id
./bin/bootwright machine info --context "$CONTEXT" --name nv-bond,nv-vlan,nv-vlan-bond
```

### Verify the selected features on each guest

Define this check once, following the same native proofs as
[lab-rhel](../lab-rhel/runbook.md#8-check-the-selected-features). It stops on
an unexpected command failure. Inspect each guest's results against the
requirements below before continuing. Context and Machine names label its
private registration records; keep those files out of tracked content.

```bash
verify_selected_features() {
    for MACHINE in "${CHECK_MACHINES[@]}"; do
        printf 'Feature checks: %s/%s\n' "$CHECK_CONTEXT" "$MACHINE"
        ./bin/bootwright machine exec --context "$CHECK_CONTEXT" --name "$MACHINE" -- cat /etc/os-release || return
        if [ "$FIPS" = true ]; then
            ./bin/bootwright machine exec --context "$CHECK_CONTEXT" --name "$MACHINE" -- fips-mode-setup --check || return
            ./bin/bootwright machine exec --context "$CHECK_CONTEXT" --name "$MACHINE" -- cat /proc/cmdline || return
            ./bin/bootwright machine exec --context "$CHECK_CONTEXT" --name "$MACHINE" -- sudo -n update-crypto-policies --show || return
            ./bin/bootwright machine exec --context "$CHECK_CONTEXT" --name "$MACHINE" -- ssh-keygen -lf /etc/ssh/ssh_host_ecdsa_key.pub || return
            ./bin/bootwright secret show --context "$CHECK_CONTEXT" --name bootwright-machine-key --part public-key || return
        fi
        if [ "$TPM2" = true ]; then
            ./bin/bootwright machine exec --context "$CHECK_CONTEXT" --name "$MACHINE" -- lsblk -pf || return
            read -r -p "Full /dev/... crypto_LUKS root partition for $MACHINE: " ROOT_PARTITION || return
            ROOT_PARTITIONS["$CHECK_CONTEXT/$MACHINE"]="${ROOT_PARTITION:?Choose this guest root partition}"
            ./bin/bootwright machine exec --context "$CHECK_CONTEXT" --name "$MACHINE" -- sudo -n clevis luks list -d "$ROOT_PARTITION" || return
        fi
        if [ "$REGISTER" = true ]; then
            ./bin/bootwright machine exec --context "$CHECK_CONTEXT" --name "$MACHINE" -- sudo -n subscription-manager status || return
            ./bin/bootwright machine exec --context "$CHECK_CONTEXT" --name "$MACHINE" -- sudo -n subscription-manager identity > "$RUN_DIR/$CHECK_CONTEXT.$MACHINE.rhsm-identity" || return
        fi
        if [ "$UPDATE_WITHIN_RELEASE" = true ]; then
            ./bin/bootwright machine exec --context "$CHECK_CONTEXT" --name "$MACHINE" -- cat /etc/dnf/vars/releasever || return
            ./bin/bootwright machine exec --context "$CHECK_CONTEXT" --name "$MACHINE" -- sudo -n subscription-manager release --show || return
            ./bin/bootwright machine exec --context "$CHECK_CONTEXT" --name "$MACHINE" -- sudo -n /usr/bin/python3 -m json.tool /etc/bootwright/update.json || return
            read -r -p "Transaction ID in $MACHINE update.json (0 means no changes): " UPDATE_TRANSACTION || return
            case "$UPDATE_TRANSACTION" in ''|*[!0-9]*) printf 'Copy the numeric transaction ID from the record.\n' >&2; return 1 ;; esac
            if [ "$UPDATE_TRANSACTION" -gt 0 ]; then
                ./bin/bootwright machine exec --context "$CHECK_CONTEXT" --name "$MACHINE" -- sudo -n dnf history info "$UPDATE_TRANSACTION" || return
            fi
            ./bin/bootwright machine exec --context "$CHECK_CONTEXT" --name "$MACHINE" -- sudo -n dnf check-update || return
        fi
    done
}
CHECK_CONTEXT="$CONTEXT"
CHECK_MACHINES=(nv-bond nv-vlan nv-vlan-bond)
verify_selected_features
```

Require `VERSION_ID="9.8"` on all three. For each enabled feature:

- **FIPS:** `FIPS mode is enabled.`, exactly one `fips=1` on `/proc/cmdline`,
  crypto policy `FIPS`, a `256 ... (ECDSA)` host key and a fleet public key
  beginning `ecdsa-sha2-nistp256`. Repeat these proofs after the explicit reboot.
- **TPM2:** in `lsblk -pf`, find the `crypto_LUKS` partition whose children
  contain the `/` mountpoint. Enter its full `NAME` path without tree drawing
  characters; never infer one guest's root device from another's. Require a
  `tpm2` pin on that partition. A vTPM without the encrypted root does not pass.
- **RHSM:** `Overall Status: Registered` (or `Disabled` under Simple Content
  Access), a successful identity read, and that consumer UUID present in the
  subscription service console. Read the UUID privately from the corresponding
  `RUN_DIR` file and retain the mapping for the removal check.
- **Update:** release variable `9.8`, `Release: 9.8`, and `update.json` with
  `state: complete`, `release: 9.8` and `pin: release`. For its exact nonzero
  `transaction`, require DNF history success and `Comment` equal to
  `bootwright:` followed by that record's `request`. Transaction `0` is valid
  when no package changed and needs no history entry. `dnf check-update` must
  return `0` with no newer packages within 9.8; `100` is an unmet check, not a
  successful update proof. See [update within release](../../specs/managed-os.md#update-within-release).

## 8. Check drift and reboot

Drift (B326). Lower the MTU of one guest-only VLAN. `./bin/bootwright diff --context "$CONTEXT" --observe` reads
`Machine/nv-bond` partial and names the block, not the field, so read the
running MTU from the guest. The note `part of this effect is realized as its
frozen request states` signals drift even if the footer begins `No differences`:
network fields may be listed as not compared. Restoring 1400 clears the partial
note and reads realized. Observation repairs nothing:

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name nv-bond -- sudo -n ip link set dev bond0.140 mtu 1300
./bin/bootwright diff --context "$CONTEXT" --observe
./bin/bootwright machine exec --context "$CONTEXT" --name nv-bond -- sudo -n nmstatectl show bond0.140
./bin/bootwright machine exec --context "$CONTEXT" --name nv-bond -- sudo -n ip link set dev bond0.140 mtu 1400
./bin/bootwright diff --context "$CONTEXT" --observe
```

Reboot (B326). Restart all three, then repeat `ip -br address show` until SSH
answers; the declared addresses return without any apply. Restart returns at
power-on, before the OS boots:

```sh
./bin/bootwright machine restart --context "$CONTEXT" --name nv-bond,nv-vlan,nv-vlan-bond --yes
./bin/bootwright machine exec --context "$CONTEXT" --name nv-bond,nv-vlan,nv-vlan-bond -- ip -br address show
```

Wait for all three to answer SSH before continuing; repeat only that read
while they boot. Then repeat the selected feature checks, choosing each root
partition from its fresh `lsblk` output:

```bash
verify_selected_features
```

With `TPM2=true`, the guests answering after reboot also proves unattended
unlock. Test the recovery passphrase on each guest as in
[lab-rhel recovery](../lab-rhel/runbook.md#9-reboot-and-check-recovery).
The real passphrase goes only through a mode-0600 file and standard input,
never an argument or an echoed value. The wrong-passphrase command is an
expected negative test, so capture its status with `if`, even in a shell using
`set -e`:

```bash
verify_recovery_passphrase() {
    if [ "$TPM2" != true ]; then return 0; fi
    for MACHINE in "${CHECK_MACHINES[@]}"; do
        ROOT_PARTITION=${ROOT_PARTITIONS["$CHECK_CONTEXT/$MACHINE"]:?Run the native root check first}
        ./bin/bootwright secret show --context "$CHECK_CONTEXT" --name luks-recovery --part value > "$RUN_DIR/recovery" || return
        chmod 0600 "$RUN_DIR/recovery" || return
        if ./bin/bootwright machine exec --context "$CHECK_CONTEXT" --name "$MACHINE" -- sudo -n cryptsetup open --test-passphrase -v "$ROOT_PARTITION" < "$RUN_DIR/recovery"; then
            RECOVERY_STATUS=0
        else
            RECOVERY_STATUS=$?
        fi
        rm -f -- "$RUN_DIR/recovery"
        printf '%s recovery-passphrase exit code: %s\n' "$MACHINE" "$RECOVERY_STATUS"
        if [ "$RECOVERY_STATUS" -ne 0 ]; then return 1; fi
        if printf '%s\n' 'Bootwright-Wrong-Recovery-Passphrase-123!' | ./bin/bootwright machine exec --context "$CHECK_CONTEXT" --name "$MACHINE" -- sudo -n cryptsetup open --test-passphrase -v "$ROOT_PARTITION"; then
            WRONG_STATUS=0
        else
            WRONG_STATUS=$?
        fi
        printf '%s wrong-passphrase exit code: %s\n' "$MACHINE" "$WRONG_STATUS"
        if [ "$WRONG_STATUS" -ne 2 ]; then return 1; fi
    done
}
verify_recovery_passphrase
```

Require `Key slot <n> unlocked.` and exit `0` for the real passphrase, then
`No key available with this passphrase.` and exit `2` for the wrong one on
every root partition. The long dummy value passes the FIPS length requirement;
an argument error or other refusal does not pass the negative test. Preserve
only the check's outcome, never the recovery file.

## 9. Destroy the three-guest context

Run the first destroy while all three guests still run. With `REGISTER=true`,
it must first complete their pre-stop unregistration. It then pauses with
`lifecycle.live`, naming the guests to stop. This expected checkpoint is its
own command block; inspect it before running any subsequent commands:

```sh
./bin/bootwright destroy --context "$CONTEXT" --authorize data-loss
```

With registration selected, verify all three recorded consumer UUIDs are gone
from the subscription console, and run:

```bash
if [ "$REGISTER" = true ]; then
    for MACHINE in nv-bond nv-vlan nv-vlan-bond; do
        if ./bin/bootwright machine exec --context "$CONTEXT" --name "$MACHINE" -- sudo -n subscription-manager identity > "$RUN_DIR/$CONTEXT.$MACHINE.rhsm-after-unregister" 2>&1; then
            printf '%s is still registered; stop and inspect the removal.\n' "$MACHINE" >&2
            break
        else
            printf '%s identity exit code after unregistration: %s\n' "$MACHINE" "$?"
        fi
    done
fi
```

Each saved result must specifically say the system is no longer registered;
an SSH, sudo or unrelated error is not evidence of unregistration. Keep the
console and identity observations private. Do not stop the guests until these
checks pass. Without registration, only the `lifecycle.live` checkpoint applies.

Stop the named guests, then require removal to complete:

```sh
./bin/bootwright machine stop --context "$CONTEXT" --name nv-bond,nv-vlan,nv-vlan-bond --yes
./bin/bootwright destroy --context "$CONTEXT" --authorize data-loss
./bin/bootwright status --context "$CONTEXT"
systemctl list-units --all 'bootwright-*'
EVIDENCE_CONTEXT="$CONTEXT"
```

Complete [Preserve evidence before context purge](../README.md#preserve-evidence-before-context-purge).
Continue only after its archive and digest verification succeeds:

```sh
./bin/bootwright context delete --name "$CONTEXT" --purge --yes
```

## 10. Verify a declaration that cuts the controller off

The three-guest context is now removed, freeing the shared bridge and ports.
This final B326 drill uses a separate one-guest context and the same listener
firewall rules. The case
renders only `nv-bond`, on the managed network, with a route that sends the
controller's replies out the guest-only VLAN to a next hop nobody answers,
under the context name `nv-cutoff`. Keep all four feature choices unchanged:

```sh
e2e/render.sh lab-network-variants "${FEATURE_ARGS[@]}" --set name=nv-cutoff --set single=true --set cutoff=true --out-dir ~/bootwright-e2e/lab-network-variants-cutoff
CUTOFF_CONTEXT="nv-cutoff"
./bin/bootwright validate -f ~/bootwright-e2e/lab-network-variants-cutoff
./bin/bootwright context init --name "$CUTOFF_CONTEXT" --input-dir ~/bootwright-e2e/lab-network-variants-cutoff
./bin/bootwright secret generate --context "$CUTOFF_CONTEXT"
if [ "$REGISTER" = true ]; then
    ./bin/bootwright secret set --context "$CUTOFF_CONTEXT" --name rhsm-org --value-file "${RHSM_ORG_FILE:?Set RHSM_ORG_FILE}" --yes
    ./bin/bootwright secret set --context "$CUTOFF_CONTEXT" --name rhsm-key --value-file "${RHSM_KEY_FILE:?Set RHSM_KEY_FILE}" --yes
fi
./bin/bootwright secret check --context "$CUTOFF_CONTEXT"
./bin/bootwright apply --context "$CUTOFF_CONTEXT" --stage controller --yes
./bin/bootwright preflight controller --context "$CUTOFF_CONTEXT"
./bin/bootwright apply --context "$CUTOFF_CONTEXT" --stage infrastructure-services --yes
```

Run the full apply on its own and wait for the deliberate negative checkpoint:

```sh
./bin/bootwright apply --context "$CUTOFF_CONTEXT" --yes
```

The last apply first provisions the guest and installs RHEL. Let it finish
naturally: the deliberate failure occurs during post-install network
convergence, after NetworkManager's rollback. The outer CLI may report only
`the adapter operation did not complete`. As root, inspect the failed block's
retained `.output` file named by that diagnostic. Require the failed task
`Stop at a declared network that cut the controller off` and its message
containing `NetworkManager rolled it back`, `cannot change under this apply`,
and the remedy to run `bootwright destroy`, correct the declaration and apply
again. The generic outer error alone does not pass this negative test.

Repeat apply, then check its retained output for the same failed task and
detailed reason before continuing:

```sh
./bin/bootwright apply --context "$CUTOFF_CONTEXT" --yes
```

After that expected refusal, stop and destroy the guest. This failure occurs
before subscription registration, even when `REGISTER=true`; do not use an
unregistration acknowledgement for a consumer that was never registered.
Render the corrected declaration with the same feature arguments:

```sh
./bin/bootwright machine stop --context "$CUTOFF_CONTEXT" --name nv-bond --yes
./bin/bootwright destroy --context "$CUTOFF_CONTEXT" --authorize data-loss
e2e/render.sh lab-network-variants "${FEATURE_ARGS[@]}" --set name=nv-cutoff --set single=true --out-dir ~/bootwright-e2e/lab-network-variants-cutoff
./bin/bootwright context update --name "$CUTOFF_CONTEXT" --input-dir ~/bootwright-e2e/lab-network-variants-cutoff --yes
./bin/bootwright apply --context "$CUTOFF_CONTEXT" --yes
```

Require successful installation, live diff realized and the same selected
feature proofs on this new guest before removing it:

```bash
./bin/bootwright status --context "$CUTOFF_CONTEXT"
./bin/bootwright diff --context "$CUTOFF_CONTEXT" --observe
CHECK_CONTEXT="$CUTOFF_CONTEXT"
CHECK_MACHINES=(nv-bond)
verify_selected_features
```

After those checks pass, restart this guest and wait for SSH:

```sh
./bin/bootwright machine restart --context "$CUTOFF_CONTEXT" --name nv-bond --yes
./bin/bootwright machine exec --context "$CUTOFF_CONTEXT" --name nv-bond -- ip -br address show
```

Restart again returns at power-on. Repeat only the SSH read until it answers,
then repeat the native feature checks and recovery-passphrase checks:

```bash
verify_selected_features
verify_recovery_passphrase
```

### Choose the corrected guest's registration removal drill

With `REGISTER=true`, choose `MANUAL_UNREGISTRATION=true` to cover B76's
manual-removal refusal using this already required second installation. With
`false`, use normal pre-stop unregistration again; that does not cover B76's
manual path. With `REGISTER=false`, keep it `false`:

```sh
MANUAL_UNREGISTRATION=false
```

For the manual path, stop the guest while it is still registered, then run
destroy as its own expected-refusal checkpoint:

```bash
if [ "$REGISTER" = true ] && [ "$MANUAL_UNREGISTRATION" = true ]; then
    ./bin/bootwright machine stop --context "$CUTOFF_CONTEXT" --name nv-bond --yes
fi
```

```bash
if [ "$REGISTER" = true ] && [ "$MANUAL_UNREGISTRATION" = true ]; then
    ./bin/bootwright destroy --context "$CUTOFF_CONTEXT" --authorize data-loss
fi
```

Require `registration-unreachable`, naming `--unregistered`. This is the
expected refusal, not successful teardown. Use this installation's private
identity record to remove its consumer through the subscription service UI.
Verify that exact UUID is absent there before acknowledging it:

```bash
if [ "$REGISTER" = true ] && [ "$MANUAL_UNREGISTRATION" = true ]; then
    ./bin/bootwright destroy --context "$CUTOFF_CONTEXT" --unregistered nv-bond --authorize data-loss
fi
```

For normal registered removal, leave the corrected guest running for the
first destroy. Require pre-stop unregistration followed by `lifecycle.live`:

```bash
if [ "$REGISTER" = true ] && [ "$MANUAL_UNREGISTRATION" != true ]; then
    ./bin/bootwright destroy --context "$CUTOFF_CONTEXT" --authorize data-loss
fi
```

Verify its consumer UUID is absent in the subscription service UI and that
identity specifically reports unregistered, not an access failure:

```bash
if [ "$REGISTER" = true ] && [ "$MANUAL_UNREGISTRATION" != true ]; then
    if ./bin/bootwright machine exec --context "$CUTOFF_CONTEXT" --name nv-bond -- sudo -n subscription-manager identity > "$RUN_DIR/$CUTOFF_CONTEXT.nv-bond.rhsm-after-unregister" 2>&1; then
        printf 'The guest is still registered; stop and inspect removal.\n' >&2
    else
        printf 'Identity exit code after unregistration: %s\n' "$?"
    fi
fi
```

Only after those checks, stop and complete normal removal. This is also the
stop-first path when registration was not selected. The manual path already
completed removal with its acknowledgement above:

```bash
if [ "$REGISTER" != true ] || [ "$MANUAL_UNREGISTRATION" != true ]; then
    ./bin/bootwright machine stop --context "$CUTOFF_CONTEXT" --name nv-bond --yes
    ./bin/bootwright destroy --context "$CUTOFF_CONTEXT" --authorize data-loss
fi
./bin/bootwright status --context "$CUTOFF_CONTEXT"
EVIDENCE_CONTEXT="$CUTOFF_CONTEXT"
```

Complete [Preserve evidence before context purge](../README.md#preserve-evidence-before-context-purge)
for this context too. Continue only after verification succeeds:

```sh
./bin/bootwright context delete --name "$CUTOFF_CONTEXT" --purge --yes
```

## 11. Clean up and record the result

Complete [Restore listener firewall](../README.md#restore-listener-firewall),
then bring down and remove the external connections created in step 2.
Deactivation stops NetworkManager's sharing helpers and removes its NAT rules;
it does not erase unrelated host configuration or any retained DHCP lease file:

```sh
sudo nmcli connection down br-nv.120
sudo nmcli connection down br-nv.130
sudo nmcli connection down br-nv
sudo nmcli connection delete br-nv.120 br-nv.130 br-nv
```

Complete [Remove diagnostic tools](../README.md#remove-diagnostic-tools),
then remove the temporary private directory. Keep the verified archives under
`.cache/e2e-evidence` for owner review and the acceptance record. Before this
removal, retain the non-secret native feature-check outcomes and the private
registration/UUID-removal mappings with those verified archives. Never copy
the recovery-passphrase file into an archive:

```sh
rm -r -- "$RUN_DIR"
```

The run succeeds only after every required step and expected result above,
including both deliberate interruptions and cleanup, on one clean build.
Record the feature choices and whether the optional manual-removal drill ran
in the [acceptance ledger](../../docs/acceptance.md). A missed interruption,
unexpected failure or mixed-build run remains an observation; evidence for
one item does not make that whole case successful. Keep environment details,
consumer UUIDs, native outputs and operation digests in the private archive.
Cached media remains.

| Item | Evidence this selected run must supply |
| --- | --- |
| [B74](../../specs/milestones/m1.md#b74) | All three install shapes, permanent interface pins, unsupported bond mode refusal, links/routes, reboot and install Ctrl-C/replay |
| [B326](../../specs/milestones/m1.md#b326) | Final network, reboot, MTU drift/restoration, the separately witnessed convergence Ctrl-C/replay and cutoff rollback/recovery |
| [B75](../../specs/milestones/m1.md#b75) | `FIPS=true`: native FIPS, kernel argument, crypto policy and ECDSA proofs on each guest before and after reboot |
| [B76](../../specs/milestones/m1.md#b76) | `REGISTER=true`: normal pre-stop unregistration of the three guests plus the optional corrected-cutoff manual-removal refusal and service-side UUID removal; skipping the latter leaves that gate open |
| [B77](../../specs/milestones/m1.md#b77) | `TPM2=true`: encrypted roots, TPM2 bindings, unattended unlock after reboot and real/wrong recovery-passphrase checks |
| [B876](../../specs/milestones/m1.md#b876) | `UPDATE_WITHIN_RELEASE=true`: complete update record, exact successful DNF transaction or valid zero, release pin and no remaining updates within 9.8 |
| [B978](../../specs/milestones/m1.md#b978) | Installer and observation invocations complete without an Ansible local RPC socket path failure |
| [B979](../../specs/milestones/m1.md#b979) | `TPM2=true`: encrypted installation obtains recovery material over the declared bond/VLAN installation network |

[B346](../../specs/milestones/m1.md#b346) needs a FIPS-mode controller; FIPS
guests alone do not qualify it. [B624](../../specs/milestones/backlog.md#b624)'s
network-field projection remains parked and is not covered by this case.
