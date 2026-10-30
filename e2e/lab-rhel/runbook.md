# lab-rhel

One RHEL 9.8 guest, `rhel-01` at `198.51.100.11`, installed through an
emulated Redfish BMC from a hosted DVD tree, beside managed DNS, NTP, proxy and
artifact services on the controller. Toggles add FIPS mode, TPM2 root
encryption, RHSM registration and an update within the release. Context
`lab-rhel`; 1 guest of 2 vCPU and 4 GiB.

It accepts [B75](../../specs/milestones/m1.md#b75) (FIPS),
[B77](../../specs/milestones/m1.md#b77) (TPM2),
[B76](../../specs/milestones/m1.md#b76) (registration) and
[B876](../../specs/milestones/m1.md#b876) (update within the release), one
ledger row per toggle run.

Run every numbered step in order in one Bash session. A successful run includes
verification, the drills for the selected features, destroy and host cleanup.
An unexpected failure stops the run and makes it unsuccessful; keep its
private records for cleanup. The deliberate interruption and refusal checks
below are test steps with stated expected outcomes.

## Prepare

### 1. Choose the features

Set these values once. `UPDATE_WITHIN_RELEASE=true` requires `REGISTER=true`.
Registration acceptance includes two installations: normal unregistration,
then manual unregistration while the guest is stopped.

```sh
FIPS=true
TPM2=false
REGISTER=false
UPDATE_WITHIN_RELEASE=false
RHSM_ORG_FILE="$HOME/rhel-org.txt"
RHSM_KEY_FILE="$HOME/rhel-activation-key.txt"
CONTEXT=lab-rhel
INPUT_DIR="$HOME/bootwright-e2e/lab-rhel"
MACHINE=rhel-01
umask 077
RUN_DIR=$(mktemp -d)
printf 'Private run directory: %s\n' "$RUN_DIR"
```

| Variable | Meaning |
| --- | --- |
| `CONTEXT` | Bootwright context name |
| `INPUT_DIR` | Rendered input directory |
| `CONTROLLER_ADDRESS` | Host address used by the services; set in step 3 |
| `RUN_DIR` | Private directory containing this run's cleanup records |
| `MACHINE` | Guest name declared in the input |

### 2. Prepare the host

Complete [Prepare the host](../README.md#prepare-the-host) from the checkout
being tested. This case needs the RHEL 9.8 boot and DVD ISOs, 3 host CPUs,
6 GiB of available memory and 50 GiB free on the filesystem holding libvirt's
guest disks. With registration enabled, prepare the two RHSM credential files
above and access to the
[Red Hat Hybrid Cloud Console](https://console.redhat.com/insights/inventory)
to verify removal of the guest's consumer. Keep `RUN_DIR` until step 12.
[values.yaml](values.yaml) documents the remaining input defaults.

## Run

### 3. Render and initialize the context

Start with no context named `lab-rhel`. Render the selected features, initialize
that context and populate its secrets:

```sh
e2e/render.sh lab-rhel --out-dir "$INPUT_DIR" \
  --set "security.fips=$FIPS" --set "security.tpm2=$TPM2" \
  --set "registration.enabled=$REGISTER" \
  --set "registration.updateWithinRelease=$UPDATE_WITHIN_RELEASE"
CONTROLLER_ADDRESS=$(ip -4 route get 192.0.2.1 | sed -n 's/.* src \([0-9.]*\).*/\1/p')   # the host address
./bin/bootwright validate -f "$INPUT_DIR"
./bin/bootwright context init --name "$CONTEXT" --input-dir "$INPUT_DIR"
./bin/bootwright secret generate --context "$CONTEXT"
if [ "$REGISTER" = true ]; then
  ./bin/bootwright secret set --context "$CONTEXT" --name rhsm-org --value-file "${RHSM_ORG_FILE:?Set RHSM_ORG_FILE to the RHSM organization ID file}" --yes
  ./bin/bootwright secret set --context "$CONTEXT" --name rhsm-key --value-file "${RHSM_KEY_FILE:?Set RHSM_KEY_FILE to the RHSM activation key file}" --yes
fi
./bin/bootwright secret check --context "$CONTEXT"
```

### 4. Apply controller prerequisites

Install the context-selected packages and bind the context to this controller:

```sh
./bin/bootwright plan --context "$CONTEXT"
./bin/bootwright apply --context "$CONTEXT" --stage controller --yes
```

Require the controller block to finish successfully.

### 5. Prepare listener firewall and require readiness

Set the listener ports for this case:

```bash
FIREWALL_PORTS=(53/tcp 53/udp 123/udp 3128/tcp 8080/tcp 8443/tcp)
```

Complete
[Prepare listener firewall](../README.md#prepare-listener-firewall) in this
same shell using `CONTROLLER_ADDRESS`, `CONTEXT` and `RUN_DIR` above.
It records the runtime port rules this run adds. `CONTROLLER_ADDRESS` must
match the address in the rendered input.

Then check controller readiness:

```sh
./bin/bootwright preflight controller --context "$CONTEXT"
```

Require `Outcome ready` before step 6.

### 6. Start services and install the guest

```sh
./bin/bootwright apply --context "$CONTEXT" --stage infrastructure-services --yes
```

With `security.tpm2=true`, `secret generate` also creates `luks-recovery`
from 32 random bytes, encoded as a 43-character base64url passphrase. Keep
the value used for installation for recovery; never renew or replace it while
the guest exists. The recovery-passphrase check in step 9 reads the
stored value into a private file.

The services answer on the host address `CONTROLLER_ADDRESS`:

```sh
curl -sk "https://$CONTROLLER_ADDRESS:8443/" -o /dev/null -w '%{http_code}\n'   # 404 on an empty root
dig @"$CONTROLLER_ADDRESS" +short controller.e2e.example.test
dig @"$CONTROLLER_ADDRESS" +tcp +short rhel-01.e2e.example.test
chronyd -Q -t 10 "server $CONTROLLER_ADDRESS iburst port 123"
```

Test installation continuation: press Ctrl-C once while the installation
runs, inspect its status, then require the second apply to complete:

```sh
./bin/bootwright apply --context "$CONTEXT" --yes      # Ctrl-C once during the installation
./bin/bootwright status --context "$CONTEXT"
./bin/bootwright apply --context "$CONTEXT" --yes
```

## Verify

### 7. Check the installed guest

```sh
./bin/bootwright status --context "$CONTEXT"
./bin/bootwright machine list --context "$CONTEXT" --power-status
./bin/bootwright machine exec --context "$CONTEXT" --name "$MACHINE" -- cat /etc/os-release
./bin/bootwright machine exec --context "$CONTEXT" --name "$MACHINE" -- chronyc sources
./bin/bootwright machine exec --context "$CONTEXT" --name "$MACHINE" -- getent hosts controller.e2e.example.test
./bin/bootwright diff --context "$CONTEXT" --observe
./bin/bootwright apply --context "$CONTEXT" --yes      # settles without contacting the guest
```

### 8. Check the selected features

**FIPS (B75).** Run this block when `FIPS=true`:

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name "$MACHINE" -- fips-mode-setup --check
./bin/bootwright machine exec --context "$CONTEXT" --name "$MACHINE" -- cat /proc/cmdline
./bin/bootwright machine exec --context "$CONTEXT" --name "$MACHINE" -- sudo update-crypto-policies --show
./bin/bootwright machine exec --context "$CONTEXT" --name "$MACHINE" -- ssh-keygen -lf /etc/ssh/ssh_host_ecdsa_key.pub
./bin/bootwright secret show --context "$CONTEXT" --name bootwright-machine-key --part public-key
```

Require `FIPS mode is enabled.`, `fips=1` once on the command line, `FIPS`,
a `256 ... (ECDSA)` host key and `ecdsa-sha2-nistp256` for the fleet key.
The reboot drill repeats them.

**TPM2 (B77).** Run these blocks when `TPM2=true`:

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name "$MACHINE" -- lsblk -pf
```

Find the `crypto_LUKS` partition whose children contain the `/` mountpoint.
Copy its full `/dev/...` path from `NAME`, without the tree drawing characters,
before checking its TPM2 binding:

```sh
read -r -p 'Full /dev/... path of the guest crypto_LUKS root partition: ' ROOT_PARTITION
./bin/bootwright machine exec --context "$CONTEXT" --name "$MACHINE" -- sudo clevis luks list -d "${ROOT_PARTITION:?Set ROOT_PARTITION to the full /dev/... path of the guest crypto_LUKS root partition}"
```

Require a `tpm2` pin on that root partition.

**Registration (B76).** Run this block when `REGISTER=true`:

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name "$MACHINE" -- sudo subscription-manager status
./bin/bootwright machine exec --context "$CONTEXT" --name "$MACHINE" -- sudo subscription-manager identity
```

Require `Overall Status: Registered` (or `Disabled` under Simple Content
Access) and record the `system identity` UUID; the console's
inventory lists the system under that UUID. Unregistration is checked in
[Destroy](#destroy).

**Update within the release (B876).** Run this block when
`UPDATE_WITHIN_RELEASE=true`:

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name "$MACHINE" -- cat /etc/dnf/vars/releasever
./bin/bootwright machine exec --context "$CONTEXT" --name "$MACHINE" -- sudo subscription-manager release --show
./bin/bootwright machine exec --context "$CONTEXT" --name "$MACHINE" -- sudo /usr/bin/python3 -m json.tool /etc/bootwright/update.json
read -r -p 'Transaction ID from update.json (0 means no package changes): ' UPDATE_TRANSACTION
if [ "${UPDATE_TRANSACTION:?Set UPDATE_TRANSACTION to the transaction number from update.json}" -gt 0 ]; then
  ./bin/bootwright machine exec --context "$CONTEXT" --name "$MACHINE" -- sudo dnf history info "$UPDATE_TRANSACTION"
fi
./bin/bootwright machine exec --context "$CONTEXT" --name "$MACHINE" -- sudo dnf check-update
printf 'check-update exit code: %s\n' "$?"
```

Require `9.8` in the release variable, `Release: 9.8`, and an update record
whose `state` is `complete` and `pin` is `release`. When `transaction` is
nonzero, require that history entry to report success and its `Comment` to
equal `bootwright:` followed by the record's `request` value. A zero transaction
means no package changes and needs no history entry. Require
`VERSION_ID="9.8"` in `/etc/os-release` above, no newer package listed within
9.8, and check-update exit code `0` (`100` means updates are available).
The [update contract](../../specs/managed-os.md#update-within-release) owns
these files.

## Acceptance drills

### 9. Reboot and check recovery

Restart the guest and wait for SSH:

```sh
./bin/bootwright machine restart --context "$CONTEXT" --name "$MACHINE" --yes
./bin/bootwright machine exec --context "$CONTEXT" --name "$MACHINE" -- uptime      # restart returns at power-on; repeat until SSH answers
```

With fips, repeat the five FIPS reads: `fips-mode-setup --check`, `fips=1` in
`/proc/cmdline`, the `FIPS` policy, the ECDSA host key, and the session's own
host-key proof on the rebooted system. With tpm2, the answer itself proves the
unattended TPM2 unlock; repeat `lsblk -pf`.

**Recovery passphrase (B77).** With `TPM2=true`, unlock with the recovery
passphrase, kept in a private file and fed on standard input, never typed or
shown:

```sh
./bin/bootwright secret show --context "$CONTEXT" --name luks-recovery --part value > "$RUN_DIR/recovery"
./bin/bootwright machine exec --context "$CONTEXT" --name "$MACHINE" -- sudo cryptsetup open --test-passphrase -v "${ROOT_PARTITION:?Set ROOT_PARTITION to the full /dev/... path of the guest crypto_LUKS root partition}" < "$RUN_DIR/recovery"
printf 'recovery-passphrase exit code: %s\n' "$?"
rm -f "$RUN_DIR/recovery"
printf '%s\n' 'Bootwright-Wrong-Recovery-Passphrase-123!' | ./bin/bootwright machine exec --context "$CONTEXT" --name "$MACHINE" -- sudo cryptsetup open --test-passphrase -v "${ROOT_PARTITION:?Set ROOT_PARTITION to the full /dev/... path of the guest crypto_LUKS root partition}"
printf 'wrong-passphrase exit code: %s\n' "$?"
```

Require `Key slot <n> unlocked.` and exit 0 (cryptsetup reads a non-terminal
standard input up to its first newline), then `No key available with this
passphrase.` and exit `2` for the wrong one. Use the long dummy passphrase
above so FIPS password-length checks do not reject the input before the
unlock check. A parameter error does not pass this test. The bad-passphrase
failure is the expected result of the negative test.

## Destroy

### 10. Remove the installation

Run the first destroy while the guest is still running:

```sh
./bin/bootwright destroy --context "$CONTEXT" --authorize data-loss
```

Expect a pause naming the guest to stop. With `REGISTER=true`, destroy first
unregisters the guest. Verify its consumer UUID is gone from the console and
that the following identity check reports it is no longer registered. That
check is expected to return nonzero:

```sh
if [ "$REGISTER" = true ]; then
  ./bin/bootwright machine exec --context "$CONTEXT" --name "$MACHINE" -- sudo subscription-manager identity
fi
```

Stop the guest and complete removal:

```sh
./bin/bootwright machine stop --context "$CONTEXT" --name "$MACHINE" --yes
./bin/bootwright destroy --context "$CONTEXT" --authorize data-loss
./bin/bootwright status --context "$CONTEXT"
virsh --connect qemu:///system list --all
```

Require successful removal and no remaining domain or disk belonging to the
context, then delete its local state:

```sh
./bin/bootwright context delete --name "$CONTEXT" --purge --yes
```

Media stays available for later tests.

### 11. Test manual unregistration

With `REGISTER=false`, this step has no commands to run. With `REGISTER=true`,
make a second installation to test the required manual-removal path for B76.
The first installation and its context must have been removed in step 10.
Keep the existing firewall rules and private cleanup records for this test:

```sh
if [ "$REGISTER" = true ]; then
  e2e/render.sh lab-rhel --out-dir "$INPUT_DIR" \
    --set "security.fips=$FIPS" --set "security.tpm2=$TPM2" \
    --set "registration.enabled=$REGISTER" \
    --set "registration.updateWithinRelease=$UPDATE_WITHIN_RELEASE"
  ./bin/bootwright validate -f "$INPUT_DIR"
  ./bin/bootwright context init --name "$CONTEXT" --input-dir "$INPUT_DIR"
  ./bin/bootwright secret generate --context "$CONTEXT"
  ./bin/bootwright secret set --context "$CONTEXT" --name rhsm-org --value-file "$RHSM_ORG_FILE" --yes
  ./bin/bootwright secret set --context "$CONTEXT" --name rhsm-key --value-file "$RHSM_KEY_FILE" --yes
  ./bin/bootwright secret check --context "$CONTEXT"
  ./bin/bootwright plan --context "$CONTEXT"
  ./bin/bootwright apply --context "$CONTEXT" --stage controller --yes
  ./bin/bootwright preflight controller --context "$CONTEXT"
fi
```

Require `Outcome ready`, then install and record this installation's new
consumer UUID:

```sh
if [ "$REGISTER" = true ]; then
  ./bin/bootwright apply --context "$CONTEXT" --yes
  ./bin/bootwright machine exec --context "$CONTEXT" --name "$MACHINE" -- sudo subscription-manager identity
  ./bin/bootwright machine stop --context "$CONTEXT" --name "$MACHINE" --yes
  ./bin/bootwright destroy --context "$CONTEXT" --authorize data-loss
fi
```

Require `registration-unreachable`, naming `--unregistered`. This refusal is
expected. Remove the new consumer through the subscription console and verify
that its UUID is gone. Only then run:

```sh
if [ "$REGISTER" = true ]; then
  ./bin/bootwright destroy --context "$CONTEXT" --unregistered "$MACHINE" --authorize data-loss
  ./bin/bootwright status --context "$CONTEXT"
  virsh --connect qemu:///system list --all
fi
```

Require complete removal without contacting the stopped guest, and no
remaining domain or disk belonging to the context. Then delete its local state:

```sh
if [ "$REGISTER" = true ]; then
  ./bin/bootwright context delete --name "$CONTEXT" --purge --yes
fi
```

## Clean up host preparation

### 12. Restore the host and record the result

First run [Restore listener firewall](../README.md#restore-listener-firewall)
with the retained `RUN_DIR`. It reads `$RUN_DIR/firewall-zone` and
`$RUN_DIR/firewall-added-ports` so existing rules are preserved. Do not delete that
record before restoring the rules.

Complete [Remove diagnostic tools](../README.md#remove-diagnostic-tools)
using the `HOST_CHECKS` record created in step 2. It removes only diagnostic
packages added by this run. Bootwright's runtime and execution packages remain
installed.

Restore any host daemon configuration changed to free the service ports. Only
after successful firewall and diagnostic cleanup, remove the private records:

```sh
rm -rf -- "${RUN_DIR:?Set RUN_DIR to the private directory for this run}"
```

Record the build, feature selections and results in the
[acceptance ledger](../../docs/acceptance.md). Mark the run successful only
when all twelve steps and every check for its selected features passed,
including both removal tests when registration was enabled.
