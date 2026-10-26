# Physical-install rehearsal substrate

This input creates one empty libvirt guest and its CA-issued HTTPS Redfish BMC.
It installs no OS and owns no DNS, NTP or artifact service. A
separate [lab-baremetal](../lab-baremetal/README.md#rehearsing-it-without-hardware)
context installs into that guest through the physical path. The operator
rehearsal is unrun; offline tests qualify declaration, generation and trust.

The guest reserves 4 CPUs, 8192 MiB RAM and a 60 GiB disk; the provider also
reserves 1 CPU, 2048 MiB and 10 GiB for the host. Use a supported Linux/amd64
controller with working host repositories and virtualization, following the
[operator prerequisites](../../../docs/operator-guide.md#prepare-a-host).
Only this context owns `virbr-lab`, its subnet and BMC port 8000; customize these
before import if already allocated. Never apply another context managing the
same bridge, provider pool or port. All shipped addresses are synthetic.
Replace the controller address/FQDN in the copied Machine before import; the
emulator can remain on loopback when both contexts run on this controller.

From the repository root, with a current built executable:

```sh
rehearsal_input=$(mktemp -d /tmp/bootwright-rehearsal-input.XXXXXX)
cp -R examples/machines/lab-baremetal-substrate/. "$rehearsal_input/"
./bin/bootwright validate -f "$rehearsal_input"
./bin/bootwright setup
./bin/bootwright context init --name rehearsal-substrate --input-dir "$rehearsal_input"
./bin/bootwright secret generate --context rehearsal-substrate
./bin/bootwright plan --context rehearsal-substrate
./bin/bootwright apply --context rehearsal-substrate --stage controller
./bin/bootwright apply --context rehearsal-substrate --stage substrates
./bin/bootwright apply --context rehearsal-substrate --stage machines
./bin/bootwright machine info --context rehearsal-substrate --name rehearsal-01
```

Before the physical context is admitted, obtain the guest's Redfish system URL
and full NIC inventory from its proved substrate output/Redfish inventory.
Use the emitted system identity, not a guessed UUID or `/Systems/1`. The guest
has a virtio root disk, so select `/dev/vda`, and use its actual interface name
and MAC address. Its physical installer receives the address selected in the
second context; do not add an OS install profile here.

Export only the public issuer certificate to a protected local file. Transfer
the emulator's BMC account through protected files into the second context;
never put its password on an argument or collect it in acceptance evidence:

```sh
umask 077
rehearsal_secrets=$(mktemp -d /tmp/bootwright-rehearsal-secrets.XXXXXX)
./bin/bootwright secret show --context rehearsal-substrate --name lab-bmc-ca --part certificate > "$rehearsal_secrets/ca.pem"
./bin/bootwright secret show --context rehearsal-substrate --name lab-bmc-credentials --part username > "$rehearsal_secrets/username"
./bin/bootwright secret show --context rehearsal-substrate --name lab-bmc-credentials --part password > "$rehearsal_secrets/password"
```

The generated BMC username is `admin`; verify the exported username matches
before importing its password as described in the physical runbook. Never
export the CA private key. Its serving leaf has a fresh separate key and a
leaf-then-CA chain, valid no later than the CA's expiration. Ordinary generation
is idempotent. A named CA renewal does not reissue its leaf: explicitly renew
`lab-bmc-tls` afterward, then retire/recreate the consuming context to bind the
new versions and reimport the new public CA into the physical context. Do not
rotate either during an installation or a continuation using frozen versions.

Destroy the physical installation context first, following its retention or
explicit erase policy, and finish its private publication cleanup. Only then
remove this substrate, which deletes its owned virtual disk:

```sh
./bin/bootwright destroy --context rehearsal-substrate --authorize data-loss
```

Verify removal before deleting either context. Delete the protected exported
files when no longer needed. Record installation, private publication cleanup,
reboot and interruption/continuation as unrun until an operator executes them.
