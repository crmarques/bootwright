# lab-baremetal

One physical server installed with RHEL through its own Redfish management
controller. It is the smallest complete shape
[B73](../../specs/milestones/m4.md#b73) supports: a controller
Machine running the managed services, a bare-metal `InfraProvider`, and one
Machine that declares the hardware it must be proved to be.

**Physical installation is refused today.** Until private host-key delivery is
repaired (B73), the installation of `Machine/metal-01`
[refuses before registration](../../specs/managed-os.md#physical-installation),
because the delivered host key would be readable from the publicly served
installer image, so an apply of this example registers nothing.
[Run it today](#run-it-today) walks what an operator can run before that
refusal; [rehearsing it without hardware](#rehearsing-it-without-hardware)
describes the run that repair restores.

What differs from [lab-rhel](../lab-rhel/README.md), which installs the same
operating system on a libvirt guest, follows from the machine existing before
Bootwright and outliving this context:

- **`substrates` is empty.** A bare-metal provider runs nothing Bootwright
  installs, so it plans no provider-host block. The `machines` stage carries
  the whole realization: one block claims and proves the server, and one
  installs it.
- **The apply is authorized.** `apply --authorize data-loss` is required,
  because the installation erases a disk that already held something. The
  removal needs no authorization: it releases the claim and retains the
  machine, its disks and whatever is installed on them.
- **The machine declares what it is.** Its NICs and their addresses, its boot
  NIC, its management controller and its root device are all declared, and the
  controller's own inventory is compared against them before any media is
  inserted. A machine that answers but reports a different address set refuses.
- **The host key is declared, not discovered.** `os.install.hostKeyRef` names
  the key pair the installation delivers, so completion is proved against a key
  that was known before the machine was ever contacted.

**A RHEL controller brings its own `lorax` and `xorriso`.** The artifact
server sits on the controller, so the installation builds its media there with
`lorax` and `xorriso`, and no public source Bootwright resolves from carries
them for RHEL. On a RHEL controller, install both from the host's own enabled
Red Hat repositories (`dnf install lorax xorriso`) before the apply
([D106](../../specs/milestones/backlog.md#decisions)). The controller stage
proves them by presence and the qualified Red Hat release key, and otherwise
refuses before it acquires anything, naming the package that is missing or
signed by another key; `preflight controller --context lab-baremetal` reports
the same check. A Fedora controller's stage installs them itself.

## Run it today

What runs today is everything before the installation: admission, the import
into a context, the Secrets it generates, and the refusal itself. It needs no
hardware and no media, and the last line deletes the context the block
created. Run the block one line at a time from the repository root; the
[operator guide](../../docs/operator-guide.md#prepare-a-host) covers the build
and privileges.

```sh
make build
./bin/bootwright validate -f examples/lab-baremetal
./bin/bootwright context init --name lab-baremetal
./bin/bootwright context update --name lab-baremetal --input-dir "$PWD/examples/lab-baremetal" --yes
./bin/bootwright secret generate
./bin/bootwright secret check
./bin/bootwright plan
./bin/bootwright apply --authorize data-loss
./bin/bootwright status
./bin/bootwright context delete --name lab-baremetal --purge
```

`validate` admits all 14 files, and the import copies them. `secret generate`
creates the serving certificate, the fleet key and `metal-01-host-key`.
`secret check` fails on `lab-bmc-credentials`, the management controller's
account, until `secret set` stores it; nothing in this block reads it. At a
terminal, this command, with `<account>` replaced by that account's name,
prompts for the password on standard error with echo off and reads one line:

```sh
./bin/bootwright secret set --context lab-baremetal --name lab-bmc-credentials --username <account> --password-stdin
```

From a script, refresh sudo first, because an invocation whose standard input
is a pipe elevates without asking for a password, then pipe one line into the
same command:

```sh
sudo -v
printf '%s\n' "$BMC_PASSWORD" | ./bin/bootwright secret set --context lab-baremetal --name lab-bmc-credentials --username <account> --password-stdin
```

A sudo policy that logs input (`log_input`, `log_stdin`) records the password
whichever way it is entered.

`plan` refuses with `lifecycle.unsupported` for `Machine/metal-01`: a delivered
host key would be readable from the publicly served installer image, and the
remediation names `Machine/metal-01`. `apply` refuses with the same diagnostic
before it asks for confirmation or registers anything; a `--stage` selection
refuses the same way. `status` still lists every managed service as pending,
because nothing was registered.
`TestLabBaremetalExampleRefusesItsInstallation` holds that refusal in-tree.

A run of this block is an observation, not B73's operator gate: the gate is the
rehearsal below, and B73 records no acceptance baseline until its host-key
repair lands.

## Rehearsing it without hardware

The rehearsal stops at the installation refusal above until private host-key
delivery is repaired (B73). Once it is, the physical path can be driven
end to end on one workstation, because the emulated controller `lab-rhel` uses
implements the same Redfish surface this one drives, including the
`EthernetInterfaces` collection the target proof reads. This rehearsal is the
first run that proves that collection against the pinned emulator image.

Apply a libvirt context whose Machine is installer-provisioned — `os.provided:
false` with no `installProfileRef`, so the substrate realizes the domain and
its controller and installs nothing. Then point a copy of this example at that
controller: the `bmc.address` is the emulated endpoint, and the declared NIC
addresses are the ones the domain was realized with. Nothing else changes, and
the run exercises the claim, the proof, the private publication and the
delivered-key completion exactly as a real server would.

Keep that copy out of the repository. Its addresses belong to one workstation,
and `examples/wip/` is ignored for exactly this.

## What it does not cover

Removal retains the installed system: physical erase is deliberately not part
of this contract. A bonded or VLAN installation interface is not yet derived.
`import-certificate`, the default virtual-media trust, is implemented in the
Redfish client but not yet qualified against real firmware. The lab
controllers' own certificates stay unverified until their authority is carried
as a `caBundle` Secret named by `tls.trustBundleRef`.
