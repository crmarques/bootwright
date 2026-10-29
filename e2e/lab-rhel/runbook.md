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

## Inputs

| `--set` | Default | Effect | Secrets it adds |
| --- | --- | --- | --- |
| `security.fips=true` | `false` | FIPS-mode install; the fleet key becomes ECDSA P-256 | none |
| `security.tpm2=true` | `false` | LUKS root unlocked by the guest TPM2, recovered by a passphrase | `luks-recovery` |
| `registration.enabled=true` | `false` | RHSM registration with the hosted service | `rhsm-org`, `rhsm-key` |
| `registration.updateWithinRelease=true` | `false` | With registration: newest 9.8 packages, then a reboot | as above |

[values.yaml](values.yaml) holds the rest: addresses, upstreams, media names
and the guest size.

## Prepare

The [host preparation](../README.md#prepare-the-host), the RHEL 9.8 boot and
DVD ISOs added as media, and for registration an RHSM organization ID and
activation key and access to the
[Red Hat Hybrid Cloud Console](https://console.redhat.com/insights/inventory)
to read and remove the guest's consumer. The
[capacity gate](../../specs/substrates.md#capacity-admission) counts the guest
and the provider reserve (1 vCPU, 2 GiB, 10 GiB): 3 host CPUs, 6 GiB of
available memory and 50 GiB free on the filesystem that holds libvirt's guest
disks.

## Run

Set the feature switches below for this run. Updating within the release
requires `REGISTER=true`.

```sh
FIPS=true
TPM2=false
REGISTER=false
UPDATE_WITHIN_RELEASE=false
C=lab-rhel
IN="$HOME/bootwright-e2e/lab-rhel"
MACHINE=rhel-01
e2e/render.sh lab-rhel --out-dir "$IN" \
  --set "security.fips=$FIPS" --set "security.tpm2=$TPM2" \
  --set "registration.enabled=$REGISTER" \
  --set "registration.updateWithinRelease=$UPDATE_WITHIN_RELEASE"
A=$(ip -4 route get 192.0.2.1 | sed -n 's/.* src \([0-9.]*\).*/\1/p')   # the host address
umask 077; D=$(mktemp -d)                        # private scratch for this run
./bin/bootwright validate -f "$IN"
./bin/bootwright context init --name "$C" --input-dir "$IN"
./bin/bootwright secret generate --context "$C"
if [ "$REGISTER" = true ]; then
  { IFS= read -rsp 'RHSM organization ID: ' v; echo >&2; printf '%s' "$v"; } |
    ./bin/bootwright secret set --context "$C" --name rhsm-org --value-stdin --yes
  ./bin/bootwright secret set --context "$C" --name rhsm-key --value-stdin --yes
fi
./bin/bootwright secret check --context "$C"
./bin/bootwright plan --context "$C"
./bin/bootwright apply --context "$C" --stage controller --yes
./bin/bootwright preflight controller --context "$C"
./bin/bootwright apply --context "$C" --stage infrastructure-services --yes
```

With `security.tpm2=true`, `secret generate` also creates `luks-recovery`
from 32 random bytes, encoded as a 43-character base64url passphrase. Keep
the value used for installation for recovery; never renew or replace it while
the guest exists. The [recovery drill](#recovery-passphrase-b77) reads the
stored value into a private file.

The services answer on the host address `A`:

```sh
curl -sk "https://$A:8443/" -o /dev/null -w '%{http_code}\n'   # 404 on an empty root
dig @"$A" +short controller.e2e.example.test
dig @"$A" +tcp +short rhel-01.e2e.example.test
chronyd -Q -t 10 "server $A iburst port 123"
```

Then install the guest. Continuation is part of the run: press Ctrl-C once
while the installation runs, inspect, and repeat the apply until it completes:

```sh
./bin/bootwright apply --context "$C" --yes      # Ctrl-C once during the installation
./bin/bootwright status --context "$C"
./bin/bootwright apply --context "$C" --yes
```

## Verify

```sh
./bin/bootwright status --context "$C"
./bin/bootwright machine list --context "$C" --power-status
./bin/bootwright machine exec --context "$C" --name "$MACHINE" -- cat /etc/os-release
./bin/bootwright machine exec --context "$C" --name "$MACHINE" -- chronyc sources
./bin/bootwright machine exec --context "$C" --name "$MACHINE" -- getent hosts controller.e2e.example.test
./bin/bootwright diff --context "$C" --observe
./bin/bootwright apply --context "$C" --yes      # settles without contacting the guest
```

### FIPS: B75

```sh
./bin/bootwright machine exec --context "$C" --name "$MACHINE" -- fips-mode-setup --check
./bin/bootwright machine exec --context "$C" --name "$MACHINE" -- cat /proc/cmdline
./bin/bootwright machine exec --context "$C" --name "$MACHINE" -- sudo update-crypto-policies --show
./bin/bootwright machine exec --context "$C" --name "$MACHINE" -- ssh-keygen -lf /etc/ssh/ssh_host_ecdsa_key.pub
./bin/bootwright secret show --context "$C" --name bootwright-machine-key --part public-key
```

Require `FIPS mode is enabled.`, `fips=1` once on the command line, `FIPS`,
a `256 ... (ECDSA)` host key and `ecdsa-sha2-nistp256` for the fleet key.
The reboot drill repeats them.

### TPM2: B77

```sh
./bin/bootwright machine exec --context "$C" --name "$MACHINE" -- lsblk -f
```

Set the variable from that output before checking its TPM2 binding:

```sh
ROOT_PARTITION=''
./bin/bootwright machine exec --context "$C" --name "$MACHINE" -- sudo clevis luks list -d "${ROOT_PARTITION:?Set ROOT_PARTITION to the guest crypto_LUKS partition}"
```

Set `ROOT_PARTITION` to the guest's `crypto_LUKS` root partition from
`lsblk` above (`/dev/vda3` on the default layout). Require a `tpm2` pin.

### Registration: B76

```sh
./bin/bootwright machine exec --context "$C" --name "$MACHINE" -- sudo subscription-manager status
./bin/bootwright machine exec --context "$C" --name "$MACHINE" -- sudo subscription-manager identity
```

Require `Overall Status: Registered` (or `Disabled` under Simple Content
Access) and record the `system identity` UUID; the console's
inventory lists the system under that UUID. Unregistration is checked in
[Destroy](#destroy).

### Update within the release: B876

With `registration.updateWithinRelease=true`:

```sh
./bin/bootwright machine exec --context "$C" --name "$MACHINE" -- cat /etc/dnf/vars/releasever
./bin/bootwright machine exec --context "$C" --name "$MACHINE" -- sudo subscription-manager release --show
./bin/bootwright machine exec --context "$C" --name "$MACHINE" -- sudo cat /etc/bootwright/update.json
./bin/bootwright machine exec --context "$C" --name "$MACHINE" -- sudo dnf history list
./bin/bootwright machine exec --context "$C" --name "$MACHINE" -- sudo dnf check-update
```

Require `9.8` in the release variable, `Release: 9.8`, an update record whose
state is `complete`, a history transaction carrying the comment
`bootwright:<request digest>` the record names (none when nothing was newer),
`VERSION_ID="9.8"` in `/etc/os-release` above, and no newer package within
9.8. The [update contract](../../specs/managed-os.md#update-within-release)
owns these files.

## Acceptance drills

### Reboot: B75, B77

```sh
./bin/bootwright machine restart --context "$C" --name "$MACHINE" --yes
./bin/bootwright machine exec --context "$C" --name "$MACHINE" -- uptime      # restart returns at power-on; repeat until SSH answers
```

With fips, repeat the five FIPS reads: `fips-mode-setup --check`, `fips=1` in
`/proc/cmdline`, the `FIPS` policy, the ECDSA host key, and the session's own
host-key proof on the rebooted system. With tpm2, the answer itself proves the
unattended TPM2 unlock; repeat `lsblk -f`.

### Recovery passphrase: B77

Unlock with the recovery passphrase, kept in the private scratch file and fed
on standard input, never typed or shown:

```sh
./bin/bootwright secret show --context "$C" --name luks-recovery --part value > "$D/recovery"
./bin/bootwright machine exec --context "$C" --name "$MACHINE" -- sudo cryptsetup open --test-passphrase -v "${ROOT_PARTITION:?Set ROOT_PARTITION to the guest crypto_LUKS partition}" < "$D/recovery"
rm -f "$D/recovery"
printf 'wrong\n' | ./bin/bootwright machine exec --context "$C" --name "$MACHINE" -- sudo cryptsetup open --test-passphrase -v "${ROOT_PARTITION:?Set ROOT_PARTITION to the guest crypto_LUKS partition}"
```

Require `Key slot <n> unlocked.` and exit 0 (cryptsetup reads a non-terminal
standard input up to its first newline), then `No key available with this
passphrase.` for the wrong one. If the TPM2 unlock ever fails at boot, the
same passphrase unlocks at the guest console:

```sh
virsh --connect qemu:///system console "bootwright-$C-$MACHINE"
```

### Manual unregistration: B76

A separate registered run, from a fresh render and `context init`, drives the
path for a guest that cannot answer
([removal](../../specs/managed-os.md#registration)). After Verify's
registration reads:

```sh
./bin/bootwright machine stop --context "$C" --name "$MACHINE" --yes
./bin/bootwright destroy --context "$C" --authorize data-loss
```

The destroy must refuse `registration-unreachable`, naming
`--unregistered`. Remove the recorded consumer UUID with the subscription
service's own tools (the console's inventory, Remove system), confirm it is
gone, then:

```sh
./bin/bootwright destroy --context "$C" --unregistered "$MACHINE" --authorize data-loss
./bin/bootwright status --context "$C"
./bin/bootwright context delete --name "$C" --purge --yes
```

The guest is never contacted on that invocation; require the destroy to
complete with no domain or disk of the context left.

## Destroy

```sh
./bin/bootwright destroy --context "$C" --authorize data-loss
if [ "$REGISTER" = true ]; then
  ./bin/bootwright machine exec --context "$C" --name "$MACHINE" -- sudo subscription-manager identity
fi
./bin/bootwright machine stop --context "$C" --name "$MACHINE" --yes
./bin/bootwright destroy --context "$C" --authorize data-loss
./bin/bootwright status --context "$C"
virsh --connect qemu:///system list --all
./bin/bootwright context delete --name "$C" --purge --yes
rm -rf "$D"
```

The first destroy unregisters a registered guest while it runs, then pauses
and names the stop; without registration it refuses at once, naming the same
stop. With registration, the guest must no longer be registered, and
the recorded consumer UUID must be gone from the console's inventory. Media
stays for the next case.
