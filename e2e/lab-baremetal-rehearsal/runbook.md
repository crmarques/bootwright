# lab-baremetal-rehearsal

The physical installation path, rehearsed without hardware. Two contexts share
one host. The substrate context `rehearsal-substrate` creates one empty libvirt
guest, `rehearsal-01` (4 vCPU, 8 GiB, a 60 GiB disk and a TPM) on the NAT
network `virbr-e2e` (`198.51.100.0/24`), behind an emulated Redfish BMC on
`127.0.0.1:8000` whose serving leaf the generated authority `lab-bmc-ca`
issued. The install context `lab-baremetal` treats that guest as a physical
server, `metal-01`: it targets the BMC with verified TLS against `lab-bmc-ca`
only, proves the server's declared NIC inventory, then installs RHEL 9.8 at
`198.51.100.11` through virtual media from a hosted DVD tree, beside managed
DNS, NTP and artifact services on the controller.

It accepts [B73](../../specs/milestones/m1.md#b73) (the physical installation:
target proof, private host-key delivery, pinned completion, reboot and
interruption) and [B496](../../specs/milestones/m1.md#b496) (the CA-issued
emulator leaf verified through `trustBundleRef`). The same runs are the
rehearsal that B73 names as its operator gate.

The install copy omits the `firmware` block (`mode: prove`, Secure Boot on,
UEFI) of [the lab-baremetal example](../../examples/machines/lab-baremetal/README.md):
the emulated server is a libvirt domain with BIOS firmware
([machine realization](../../specs/substrates.md#machine-realization)), so
that proof would refuse `firmware-setting-mismatch`. Firmware qualification
stays with [B305](../../specs/milestones/m1.md#b305) and
[B78](../../specs/milestones/m1.md#b78) (D104).

## Inputs

[values.yaml](values.yaml) holds the addresses, upstreams, media names and the
guest size. The install context needs two facts only the realized guest
knows, so the case renders twice: once with the defaults, which validate, to
create the substrate, then again with these keys.

| `--set` | Default | Effect |
| --- | --- | --- |
| `install.systemURL` | a placeholder | The guest's absolute Redfish system URL, the discovered absolute URL under `https://127.0.0.1:8000/redfish/v1/Systems/`, read from the BMC after the substrate applies |
| `install.nicMAC` | `52:54:00:00:00:01` | The guest's NIC MAC address, read the same way |
| `install.address` | `198.51.100.11` | The install address in the guest network |
| `guest.memoryMiB` | `8192` | The guest's memory |

The renders write `substrate` and `install` below the output directory, one
input per context. Both contexts must be rendered from the same values except
for the two `install.` keys.

## Prepare

- The [host preparation](../README.md#prepare-the-host) and the RHEL 9.8 boot
  and DVD ISOs added as media. Ports 53/tcp+udp, 123/udp, 8080 and 8443 on the
  host address and 8000 on loopback must be free; the guest, the BMC and the
  emulator reach the controller on the host address.
- Capacity, which the substrate's apply gate counts
  ([capacity admission](../../specs/substrates.md#capacity-admission)): the
  guest plus the provider's `capacityReserve` (1 CPU, 2 GiB, 10 GiB) need 5
  host CPUs not reserved by other libvirt domains, 10 GiB of `MemAvailable`
  in `/proc/meminfo` (swap never counts) and 70 GiB free on the filesystem
  backing `/var/lib/libvirt/images`, beside the media. Check with `nproc`,
  `grep MemAvailable /proc/meminfo` and `df -h /var/lib/libvirt/images`; the
  gate refuses before registration when they do not fit.
- Firewall: the managed bridge `virbr-e2e` is in firewalld's `trusted` zone
  ([provider host realization](../../specs/substrates.md#provider-host-realization)),
  so no rule is needed; the emulated BMC's fetches are local.
- `jq`, used below to read the BMC's JSON (an operator tool, not a Bootwright
  prerequisite).
- The build. B73's acceptance baseline is the commit whose `Refs:` line names
  X45, and B496's is `bba3a43b117f6c4d355a7c7741aff03675583f01`; the
  executable must descend from both, from a clean source:

  ```sh
  B="$(./bin/bootwright version -o json | jq -r .result.commit)"
  ./bin/bootwright version
  git merge-base --is-ancestor "$(git log -1 --format=%H --grep='^Refs: X45')" "$B" && echo B73-baseline-ok
  git merge-base --is-ancestor bba3a43b117f6c4d355a7c7741aff03675583f01 "$B" && echo B496-baseline-ok
  ```

  `./bin/bootwright version` reads `Source clean` and both lines print; record `$B` in the
  ledger row.

Destroying the substrate deletes the guest's disk, so the install context
always goes first.

## Run

The substrate:

```sh
e2e/render.sh lab-baremetal-rehearsal
S="rehearsal-substrate"
IN="$HOME/bootwright-e2e/lab-baremetal-rehearsal"
A="$(ip -4 route get 192.0.2.1 | sed -n 's/.* src \([0-9.]*\).*/\1/p')"
./bin/bootwright validate -f "$IN/substrate"
./bin/bootwright context init --name "$S" --input-dir "$IN/substrate"
./bin/bootwright secret generate --context "$S"
./bin/bootwright secret check --context "$S"
./bin/bootwright plan --context "$S"
./bin/bootwright apply --context "$S" --stage controller --yes
./bin/bootwright apply --context "$S" --yes
./bin/bootwright machine info --context "$S" --name rehearsal-01
```

Export the public authority and the BMC account to a protected directory,
never to a command line, for the explicit verified curl commands below:

```sh
umask 077
X="$(mktemp -d)"
./bin/bootwright secret show --context "$S" --name lab-bmc-ca --part certificate > "$X/ca.pem"
./bin/bootwright secret show --context "$S" --name lab-bmc-credentials --part username > "$X/username"
./bin/bootwright secret show --context "$S" --name lab-bmc-credentials --part password > "$X/password"
printf 'machine 127.0.0.1 login %s password %s\n' "$(cat "$X/username")" "$(cat "$X/password")" > "$X/netrc"
```

The username file reads `admin`. Read the guest's identity from its BMC: the
Systems collection's one member is the system path, and the interface path is
the one member of that system's `EthernetInterfaces` collection. The emulator
chooses both ids; never guess them:

```sh
curl -sf --cacert "$X/ca.pem" --netrc-file "$X/netrc" "https://127.0.0.1:8000/redfish/v1/Systems" | jq '.Members | length'
SYSP="$(curl -sf --cacert "$X/ca.pem" --netrc-file "$X/netrc" "https://127.0.0.1:8000/redfish/v1/Systems" | jq -r '.Members[0]["@odata.id"]')"
SYS="https://127.0.0.1:8000$SYSP"
curl -sf --cacert "$X/ca.pem" --netrc-file "$X/netrc" "https://127.0.0.1:8000$SYSP/EthernetInterfaces" | jq '.Members | length'
NICP="$(curl -sf --cacert "$X/ca.pem" --netrc-file "$X/netrc" "https://127.0.0.1:8000$SYSP/EthernetInterfaces" | jq -r '.Members[0]["@odata.id"]')"
MAC="$(curl -sf --cacert "$X/ca.pem" --netrc-file "$X/netrc" "https://127.0.0.1:8000$NICP" | jq -r .MACAddress)"
printf '%s %s\n' "$SYS" "$MAC"
```

Both lengths read `1`; `SYS` is the absolute system URL `install.systemURL`
needs. The install context:

```sh
e2e/render.sh lab-baremetal-rehearsal --set install.systemURL="$SYS" --set install.nicMAC="$MAC"
C=lab-baremetal
./bin/bootwright validate -f "$IN/install"
./bin/bootwright context init --name "$C" --input-dir "$IN/install"
./bin/bootwright secret generate --context "$C"
./bin/bootwright secret set --context "$C" --name lab-bmc-ca --certificate-file "$X/ca.pem"
./bin/bootwright secret set --context "$C" --name lab-bmc-credentials --username admin --password-file "$X/password"
./bin/bootwright secret check --context "$C"
./bin/bootwright plan --context "$C"
./bin/bootwright apply --context "$C" --stage controller --authorize data-loss
./bin/bootwright preflight controller --context "$C"
./bin/bootwright apply --context "$C" --stage infrastructure-services --authorize data-loss
```

Check the artifact server from the host address; an empty root answers 404:

```sh
curl -sk "https://$A:8443/" -o /dev/null -w '%{http_code}\n'
```

Before the install, record the virtual-media trust the BMC presents (B73).
Follow the system's manager to its virtual-media devices, then read the CD or
DVD device's `Inserted`, `VerifyCertificate` and `Certificates` collection.
The member ids are the emulator's:

```sh
MGR="$(curl -sf --cacert "$X/ca.pem" --netrc-file "$X/netrc" "https://127.0.0.1:8000$SYSP" | jq -r '.Links.ManagedBy[0]["@odata.id"]')"
curl -sf --cacert "$X/ca.pem" --netrc-file "$X/netrc" "https://127.0.0.1:8000$MGR/VirtualMedia" | jq -r '.Members[]["@odata.id"]'
VM="" # Set to the CD or DVD member path printed above, including its leading slash.
: "${VM:?Set VM to the observed CD or DVD device path}"
curl -sf --cacert "$X/ca.pem" --netrc-file "$X/netrc" "https://127.0.0.1:8000${VM:?Set VM to the observed CD or DVD device path}" | jq '{Inserted, Image, VerifyCertificate, Certificates}'
curl -sf --cacert "$X/ca.pem" --netrc-file "$X/netrc" "https://127.0.0.1:8000${VM:?Set VM to the observed CD or DVD device path}/Certificates" | jq '.Members'
```

Install. The authorization covers the whole frozen plan, which erases the
guest's disk; review the plan, then finish. While the installer runs (the
progress shows the installation wait), count the private subtree's token
directories on disk in a second terminal, without printing them:

```sh
./bin/bootwright apply --context "$C" --authorize data-loss
sudo find /var/lib/bootwright-services/lab-baremetal/artifact-server/lab-artifacts/public/private/os/metal-01 -mindepth 1 -maxdepth 1 -type d | wc -l
```

## Verify

B496, the emulator's trust. The leaf verifies against the authority the
install context names and against nothing else:

```sh
openssl s_client -connect 127.0.0.1:8000 -servername localhost -CAfile "$X/ca.pem" -verify_return_error </dev/null 2>&1 | grep -E 'Verif|verify'
openssl s_client -connect 127.0.0.1:8000 -servername localhost </dev/null 2>&1 | grep -E 'Verif|verify'
grep -rn 'trustBundleRef\|verify' "$IN/install/infra"
```

The first command reads `Verification: OK` (`Verify return code: 0 (ok)`), the
second fails against the system store, and the declaration names
`trustBundleRef: lab-bmc-ca` and sets no `verify: false`.

B73, the target proof. The install apply's block `machine-metal-01` proves the
system's identity and its complete `EthernetInterfaces` MAC set against the
declared NIC before any media insertion
([physical machine realization](../../specs/substrates.md#physical-machine-realization));
its progress line, and `./bin/bootwright status --detail full --context "$C"`, show that block done before
the installation starts. `./bin/bootwright machine list --power-status --context "$C"` only reads power
through the verified BMC leg.

B73, the installation, once the install `./bin/bootwright apply --context "$C" --authorize data-loss` completes:

```sh
./bin/bootwright status --context "$C" --detail full
./bin/bootwright machine list --context "$C" --power-status
./bin/bootwright machine exec --context "$C" --name metal-01 -- cat /etc/os-release
./bin/bootwright machine exec --context "$C" --name metal-01 -- findmnt -no SOURCE /
./bin/bootwright machine exec --context "$C" --name metal-01 -- ip -br address show
./bin/bootwright secret show --context "$C" --name metal-01-host-key --part public-key
./bin/bootwright machine exec --context "$C" --name metal-01 -- sudo cat /etc/ssh/ssh_host_ed25519_key.pub
sudo find /var/lib/bootwright-services/lab-baremetal/artifact-server/lab-artifacts/public/private/os/metal-01 -mindepth 1 -maxdepth 1 -type d | wc -l
sudo journalctl -u bootwright-lab-baremetal-artifacts-lab-artifacts
curl -sf --cacert "$X/ca.pem" --netrc-file "$X/netrc" "https://127.0.0.1:8000${VM:?Set VM to the observed CD or DVD device path}" | jq '{Inserted, Image, VerifyCertificate, Certificates}'
curl -sf --cacert "$X/ca.pem" --netrc-file "$X/netrc" "https://127.0.0.1:8000${VM:?Set VM to the observed CD or DVD device path}/Certificates" | jq '.Members'
./bin/bootwright diff --context "$C" --observe
./bin/bootwright apply --context "$C" --authorize data-loss
```

| Evidence | Reads |
| --- | --- |
| Target proof | the `machine-metal-01` block completed over verified TLS before the insert; a mismatching MAC set refuses there |
| Installation | `os-release` reads RHEL 9.8; the root device is `/dev/vda`; `eno1` holds `198.51.100.11/24` |
| Pinned SSH completion | machine execution succeeds on the first connection, and the delivered public key equals the guest's host key |
| Private delivery | the token-directory count read `1` while the installer ran and reads `0` after completion: the subtree was withdrawn before the completion inspection ([physical installation](../../specs/managed-os.md#physical-installation)) |
| Fetches | the artifact server unit's journal lists each fetch's client address, method, status and size, from the host address (the emulated BMC's insert) and from `198.51.100.11` (the installer); its log format records no request path, so it never shows which file was fetched |
| Media trust | before the install the device held no media; after it `Inserted` reads false, and the `Certificates` collection and `VerifyCertificate` read as recorded (the default `importCertificate` imports the artifact server's certificate and turns verification on before the insert) |
| Settled | `./bin/bootwright diff --observe --context "$C"` reads realized; the final apply repairs nothing |

What this case does not prove of the media trust: the emulator fetches media
without verifying the artifact server whatever the device says
([provider host realization](../../specs/substrates.md#provider-host-realization)),
so the import and settlement are observed as written, not as enforced; the
case keeps `removeCertificateAfterBoot` at its default `false`, so the
eject's certificate deletion is not exercised; and whether this emulator
publishes a manager `HttpsTransferCertVerification` switch, which would decide
instead of the device flag
([identity and power operations](../../specs/substrates.md#identity-and-power-operations)),
is recorded as read. Firmware behavior stays with
[B305](../../specs/milestones/m1.md#b305).

## Acceptance drills

- Reboot: `./bin/bootwright machine restart --context "$C" --name metal-01 --yes`, then repeat
  `./bin/bootwright machine exec --context "$C" --name metal-01 -- uptime` until SSH answers. Power goes through the
  verified BMC leg; restart returns at power-on, before the OS boots.
- Interruption and continuation: during the install `./bin/bootwright apply --context "$C" --authorize data-loss`, press Ctrl-C
  while the installer runs, run `./bin/bootwright status --context "$C"`, then repeat
  `./bin/bootwright apply --context "$C" --authorize data-loss` with the same input until it
  completes. Run it on a fresh install; a failed attempt can leave its private
  subtree published until the next apply clears it.
- Cleanup order: destroy the install context and prove its private publication
  is gone before the substrate; see Destroy.

Record each result as a row of the [acceptance ledger](../../docs/acceptance.md)
with the build commit `$B`: verified BMC TLS, target proof, media trust as
observed, installation, pinned SSH completion, private delivery, reboot,
interruption and continuation, and cleanup.

## Destroy

Install context first. Destroy releases the claim and removes the managed
services and publications, and retains the server and its installed disk:

```sh
./bin/bootwright destroy --context "$C"
./bin/bootwright status --context "$C"
sudo ls /var/lib/bootwright-services/lab-baremetal
systemctl list-units --all 'bootwright-lab-baremetal-*'
./bin/bootwright context delete --name "$C" --purge --yes
```

`ls` finds no such directory: the content root and its private subtree are
gone. Then the substrate, which deletes the guest's disk:

```sh
./bin/bootwright machine stop --context "$S" --name rehearsal-01 --yes
./bin/bootwright destroy --context "$S" --authorize data-loss
./bin/bootwright status --context "$S"
systemctl list-units --all 'bootwright-rehearsal-substrate-*'
./bin/bootwright context delete --name "$S" --purge --yes
rm -r "$X"
```

Media stays for the next case.
