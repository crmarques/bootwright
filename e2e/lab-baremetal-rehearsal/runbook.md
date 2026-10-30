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

## 1. Review the inputs

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

## 2. Prepare the host

- Complete [host preparation](../README.md#prepare-the-host), including
  diagnostic tools and both RHEL ISOs, then return here. Ports 53/tcp+udp, 123/udp, 8080 and 8443 on the
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
  for guest traffic. Prepare listener admission in step 5 below.
- The build. B73's acceptance baseline is the commit whose `Refs:` line names
  X45, and B496's is `bba3a43b117f6c4d355a7c7741aff03675583f01`; the
  executable must descend from both, from a clean source:

  ```sh
  BUILD_COMMIT="$(./bin/bootwright version -o json | jq -r .result.commit)"
  ./bin/bootwright version
  git merge-base --is-ancestor "$(git log -1 --format=%H --grep='^Refs: X45')" "$BUILD_COMMIT" && echo B73-baseline-ok
  git merge-base --is-ancestor bba3a43b117f6c4d355a7c7741aff03675583f01 "$BUILD_COMMIT" && echo B496-baseline-ok
  ```

  `./bin/bootwright version` reads `Source clean` and both lines print; record `$BUILD_COMMIT` in the
  ledger row.

Destroying the substrate deletes the guest's disk, so the install context
always goes first.

## 3. Create the substrate

```sh
e2e/render.sh lab-baremetal-rehearsal
SUBSTRATE_CONTEXT="rehearsal-substrate"
INPUT_DIR="$HOME/bootwright-e2e/lab-baremetal-rehearsal"
CONTROLLER_ADDRESS="$(ip -4 route get 192.0.2.1 | sed -n 's/.* src \([0-9.]*\).*/\1/p')"
./bin/bootwright validate -f "$INPUT_DIR/substrate"
./bin/bootwright context init --name "$SUBSTRATE_CONTEXT" --input-dir "$INPUT_DIR/substrate"
./bin/bootwright secret generate --context "$SUBSTRATE_CONTEXT"
./bin/bootwright secret check --context "$SUBSTRATE_CONTEXT"
./bin/bootwright plan --context "$SUBSTRATE_CONTEXT"
./bin/bootwright apply --context "$SUBSTRATE_CONTEXT" --stage controller --yes
./bin/bootwright preflight controller --context "$SUBSTRATE_CONTEXT"
./bin/bootwright apply --context "$SUBSTRATE_CONTEXT" --yes
./bin/bootwright machine info --context "$SUBSTRATE_CONTEXT" --name rehearsal-01
```

## 4. Discover the emulated server

Export the public authority and the BMC account to a protected directory,
never to a command line, for the explicit verified curl commands below:

```sh
umask 077
RUN_DIR="$(mktemp -d)"
./bin/bootwright secret show --context "$SUBSTRATE_CONTEXT" --name lab-bmc-ca --part certificate > "$RUN_DIR/ca.pem"
./bin/bootwright secret show --context "$SUBSTRATE_CONTEXT" --name lab-bmc-credentials --part username > "$RUN_DIR/username"
./bin/bootwright secret show --context "$SUBSTRATE_CONTEXT" --name lab-bmc-credentials --part password > "$RUN_DIR/password"
printf 'machine 127.0.0.1 login %s password %s\n' "$(cat "$RUN_DIR/username")" "$(cat "$RUN_DIR/password")" > "$RUN_DIR/netrc"
```

The username file reads `admin`. Read the guest's identity from its BMC: the
Systems collection's one member is the system path, and the interface path is
the one member of that system's `EthernetInterfaces` collection. The emulator
chooses both ids; never guess them:

```sh
curl -sf --cacert "$RUN_DIR/ca.pem" --netrc-file "$RUN_DIR/netrc" "https://127.0.0.1:8000/redfish/v1/Systems" | jq '.Members | length'
SYSTEM_PATH="$(curl -sf --cacert "$RUN_DIR/ca.pem" --netrc-file "$RUN_DIR/netrc" "https://127.0.0.1:8000/redfish/v1/Systems" | jq -r '.Members[0]["@odata.id"]')"
SYSTEM_URL="https://127.0.0.1:8000$SYSTEM_PATH"
curl -sf --cacert "$RUN_DIR/ca.pem" --netrc-file "$RUN_DIR/netrc" "https://127.0.0.1:8000$SYSTEM_PATH/EthernetInterfaces" | jq '.Members | length'
NIC_PATH="$(curl -sf --cacert "$RUN_DIR/ca.pem" --netrc-file "$RUN_DIR/netrc" "https://127.0.0.1:8000$SYSTEM_PATH/EthernetInterfaces" | jq -r '.Members[0]["@odata.id"]')"
NIC_MAC="$(curl -sf --cacert "$RUN_DIR/ca.pem" --netrc-file "$RUN_DIR/netrc" "https://127.0.0.1:8000$NIC_PATH" | jq -r .MACAddress)"
printf '%s %s\n' "$SYSTEM_URL" "$NIC_MAC"
```

Both lengths read `1`; `SYSTEM_URL` is the absolute system URL `install.systemURL`
needs.

## 5. Initialize the install context and start services

```sh
e2e/render.sh lab-baremetal-rehearsal --set install.systemURL="$SYSTEM_URL" --set install.nicMAC="$NIC_MAC"
CONTEXT=lab-baremetal
./bin/bootwright validate -f "$INPUT_DIR/install"
./bin/bootwright context init --name "$CONTEXT" --input-dir "$INPUT_DIR/install"
./bin/bootwright secret generate --context "$CONTEXT"
./bin/bootwright secret set --context "$CONTEXT" --name lab-bmc-ca --certificate-file "$RUN_DIR/ca.pem" --yes
./bin/bootwright secret set --context "$CONTEXT" --name lab-bmc-credentials --username admin --password-file "$RUN_DIR/password" --yes
./bin/bootwright secret check --context "$CONTEXT"
./bin/bootwright plan --context "$CONTEXT"
./bin/bootwright apply --context "$CONTEXT" --stage controller --authorize data-loss
```

Set the listener ports, then complete
[Prepare listener firewall](../README.md#prepare-listener-firewall).
The substrate has only a loopback BMC listener, so these are the full case's
host listener ports:

```bash
FIREWALL_PORTS=(53/tcp 53/udp 123/udp 8080/tcp 8443/tcp)
```

Require preflight `ready` before starting services:

```sh
./bin/bootwright preflight controller --context "$CONTEXT"
./bin/bootwright apply --context "$CONTEXT" --stage infrastructure-services --authorize data-loss
```

Check the artifact server from the host address; an empty root answers 404:

```sh
curl -sk "https://$CONTROLLER_ADDRESS:8443/" -o /dev/null -w '%{http_code}\n'
```

## 6. Install and test interruption

Before the install, record the virtual-media trust the BMC presents (B73).
Follow the system's manager to its virtual-media devices, then read the CD or
DVD device's `Inserted`, `VerifyCertificate` and `Certificates` collection.
The member ids are the emulator's:

```sh
MANAGER_PATH="$(curl -sf --cacert "$RUN_DIR/ca.pem" --netrc-file "$RUN_DIR/netrc" "https://127.0.0.1:8000$SYSTEM_PATH" | jq -r '.Links.ManagedBy[0]["@odata.id"]')"
curl -sf --cacert "$RUN_DIR/ca.pem" --netrc-file "$RUN_DIR/netrc" "https://127.0.0.1:8000$MANAGER_PATH/VirtualMedia" | jq -r '.Members[]["@odata.id"]'
read -r -p 'CD or DVD member path printed above (including leading slash): ' MEDIA_PATH
: "${MEDIA_PATH:?Set MEDIA_PATH to the observed CD or DVD device path}"
curl -sf --cacert "$RUN_DIR/ca.pem" --netrc-file "$RUN_DIR/netrc" "https://127.0.0.1:8000${MEDIA_PATH:?Set MEDIA_PATH to the observed CD or DVD device path}" | jq '{Inserted, Image, VerifyCertificate, Certificates}'
curl -sf --cacert "$RUN_DIR/ca.pem" --netrc-file "$RUN_DIR/netrc" "https://127.0.0.1:8000${MEDIA_PATH:?Set MEDIA_PATH to the observed CD or DVD device path}/Certificates" | jq '.Members'
```

Install. The authorization covers the whole frozen plan, which erases the
guest's disk; review the plan, then finish. While the installer runs (the
progress shows the installation wait), count the private subtree's token
directories on disk in a second terminal, without printing them:

```sh
./bin/bootwright apply --context "$CONTEXT" --authorize data-loss
```

In the second terminal while the installer runs, require a count of `1`:

```sh
sudo find /var/lib/bootwright-services/lab-baremetal/artifact-server/lab-artifacts/public/private/os/metal-01 -mindepth 1 -maxdepth 1 -type d | wc -l
```

For B73's interruption test, press Ctrl-C in the first terminal during that
wait. In the original Bash session, inspect status and finish the same install:

```sh
./bin/bootwright status --context "$CONTEXT"
./bin/bootwright apply --context "$CONTEXT" --authorize data-loss
```

## 7. Verify installation and trust

B496, the emulator's trust. The leaf verifies against the authority the
install context names and against nothing else:

```sh
openssl s_client -connect 127.0.0.1:8000 -servername localhost -CAfile "$RUN_DIR/ca.pem" -verify_return_error </dev/null 2>&1 | grep -E 'Verif|verify'
openssl s_client -connect 127.0.0.1:8000 -servername localhost </dev/null 2>&1 | grep -E 'Verif|verify'
grep -rn 'trustBundleRef\|verify' "$INPUT_DIR/install/infra"
```

The first command reads `Verification: OK` (`Verify return code: 0 (ok)`), the
second fails against the system store, and the declaration names
`trustBundleRef: lab-bmc-ca` and sets no `verify: false`.

B73, the target proof. The install apply's block `machine-metal-01` proves the
system's identity and its complete `EthernetInterfaces` MAC set against the
declared NIC before any media insertion
([physical machine realization](../../specs/substrates.md#physical-machine-realization));
its progress line, and `./bin/bootwright status --detail full --context "$CONTEXT"`, show that block done before
the installation starts. `./bin/bootwright machine list --power-status --context "$CONTEXT"` only reads power
through the verified BMC leg.

B73, the installation, once the install `./bin/bootwright apply --context "$CONTEXT" --authorize data-loss` completes:

```sh
./bin/bootwright status --context "$CONTEXT" --detail full
./bin/bootwright machine list --context "$CONTEXT" --power-status
./bin/bootwright machine exec --context "$CONTEXT" --name metal-01 -- cat /etc/os-release
./bin/bootwright machine exec --context "$CONTEXT" --name metal-01 -- findmnt -no SOURCE /
./bin/bootwright machine exec --context "$CONTEXT" --name metal-01 -- ip -br address show
./bin/bootwright secret show --context "$CONTEXT" --name metal-01-host-key --part public-key
./bin/bootwright machine exec --context "$CONTEXT" --name metal-01 -- sudo cat /etc/ssh/ssh_host_ed25519_key.pub
sudo find /var/lib/bootwright-services/lab-baremetal/artifact-server/lab-artifacts/public/private/os/metal-01 -mindepth 1 -maxdepth 1 -type d | wc -l
sudo journalctl -u bootwright-lab-baremetal-artifacts-lab-artifacts
curl -sf --cacert "$RUN_DIR/ca.pem" --netrc-file "$RUN_DIR/netrc" "https://127.0.0.1:8000${MEDIA_PATH:?Set MEDIA_PATH to the observed CD or DVD device path}" | jq '{Inserted, Image, VerifyCertificate, Certificates}'
curl -sf --cacert "$RUN_DIR/ca.pem" --netrc-file "$RUN_DIR/netrc" "https://127.0.0.1:8000${MEDIA_PATH:?Set MEDIA_PATH to the observed CD or DVD device path}/Certificates" | jq '.Members'
./bin/bootwright diff --context "$CONTEXT" --observe
./bin/bootwright apply --context "$CONTEXT" --authorize data-loss
```

| Evidence | Reads |
| --- | --- |
| Target proof | the `machine-metal-01` block completed over verified TLS before the insert; a mismatching MAC set refuses there |
| Installation | `os-release` reads RHEL 9.8; the root device is `/dev/vda`; `eno1` holds `198.51.100.11/24` |
| Pinned SSH completion | machine execution succeeds on the first connection, and the delivered public key equals the guest's host key |
| Private delivery | the token-directory count read `1` while the installer ran and reads `0` after completion: the subtree was withdrawn before the completion inspection ([physical installation](../../specs/managed-os.md#physical-installation)) |
| Fetches | the artifact server unit's journal lists each fetch's client address, method, status and size, from the host address (the emulated BMC's insert) and from `198.51.100.11` (the installer); its log format records no request path, so it never shows which file was fetched |
| Media trust | before the install the device held no media; after it `Inserted` reads false, and the `Certificates` collection and `VerifyCertificate` read as recorded (the default `importCertificate` imports the artifact server's certificate and turns verification on before the insert) |
| Settled | `./bin/bootwright diff --observe --context "$CONTEXT"` reads realized; the final apply repairs nothing |

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

## 8. Verify reboot

```sh
./bin/bootwright machine restart --context "$CONTEXT" --name metal-01 --yes
./bin/bootwright machine exec --context "$CONTEXT" --name metal-01 -- uptime
```

Restart returns at power-on; poll the uptime read until SSH answers. Power
must use the verified BMC connection.

## 9. Destroy both contexts in order

Install context first. Destroy releases the claim and removes the managed
services and publications, and retains the server and its installed disk:

```sh
./bin/bootwright destroy --context "$CONTEXT"
./bin/bootwright status --context "$CONTEXT"
sudo ls /var/lib/bootwright-services/lab-baremetal
systemctl list-units --all 'bootwright-lab-baremetal-*'
./bin/bootwright context delete --name "$CONTEXT" --purge --yes
```

`ls` finds no such directory: the content root and its private subtree are
gone. Then the substrate, which deletes the guest's disk:

```sh
./bin/bootwright machine stop --context "$SUBSTRATE_CONTEXT" --name rehearsal-01 --yes
./bin/bootwright destroy --context "$SUBSTRATE_CONTEXT" --authorize data-loss
./bin/bootwright status --context "$SUBSTRATE_CONTEXT"
systemctl list-units --all 'bootwright-rehearsal-substrate-*'
./bin/bootwright context delete --name "$SUBSTRATE_CONTEXT" --purge --yes
```

## 10. Clean up and record the result

Complete [Restore listener firewall](../README.md#restore-listener-firewall),
then [Remove diagnostic tools](../README.md#remove-diagnostic-tools).
After both succeed, remove the private files:

```sh
rm -r -- "$RUN_DIR"
```

Record build `$BUILD_COMMIT` and every required result in the
[acceptance ledger](../../docs/acceptance.md): verified BMC TLS, target proof,
media trust as observed, installation, pinned SSH completion, private delivery,
interruption, reboot and cleanup. The run succeeds only after all steps and
expected checks, including cleanup. Keep raw evidence and environment details
private. Cached media remains.
