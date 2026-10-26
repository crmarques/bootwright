# Three RHEL guests on libvirt

Three Machines share the RHEL 9.8 install profile, package tree, four managed
services and controller-hosted libvirt provider. Each guest has 4 vCPUs,
8 GiB of memory, a 60-GiB disk and an emulated HTTPS Redfish BMC. Installation
creates the `bootwright` account and records each guest's SSH host key.

## Host prerequisites

Follow [lab-rhel's prerequisites](../lab-rhel/README.md#host-prerequisites)
and [network conventions](../lab-rhel/README.md#lab-conventions), allowing
12 vCPUs, 24 GiB of guest memory and three 60-GiB disks beyond the host,
services, ISOs and package tree. The three BMC listeners use loopback ports
`8000`, `8001` and `8002`. Run one lab per host; service sockets and
`virbr-lab` overlap with other examples.

Customize all three Machine addresses (`198.51.100.11`, `.12`, `.13`), the
provider subnet/gateway, NetworkConfig routes and domain, controller address,
service bind addresses and artifact certificate together. Replace upstream DNS
and time placeholders with reachable sources. Keep BMC listeners on loopback.
The [input](environment.yaml), [infrastructure](infra) and
[Secret descriptors](secret-descriptors) contain only documentation values;
edit a copy outside Git. All four Secrets are generated locally. FIPS-mode
controllers need an RSA or P-256 fleet SSH key.

## Run it

Run from the repository root with a current `./bin/bootwright` executable
(`make build` builds it). Copy, customize, then keep this variable in the same
shell:

```sh
lab_rhel_three_input=$(mktemp -d /tmp/bootwright-lab-rhel-three.XXXXXX)
cp -R examples/machines/lab-rhel-3-nodes/. "$lab_rhel_three_input/"
```

Replace the ISO paths and run each command separately. Setup prepares the
host baseline; the controller stage installs this graph's packages from the
host's configured repositories. Mutations may request confirmation and sudo.

```sh
./bin/bootwright version
./bin/bootwright validate -f "$lab_rhel_three_input"
./bin/bootwright setup
./bin/bootwright media add --name rhel-9.8-x86_64-boot.iso --from-file /path/to/rhel-9.8-x86_64-boot.iso
./bin/bootwright media add --name rhel-9.8-x86_64-dvd.iso --from-file /path/to/rhel-9.8-x86_64-dvd.iso
./bin/bootwright media list --checksums
./bin/bootwright context init --name lab-rhel-3-nodes --input-dir "$lab_rhel_three_input"
./bin/bootwright secret generate
./bin/bootwright secret check
./bin/bootwright plan
./bin/bootwright apply --stage controller
./bin/bootwright preflight controller --context lab-rhel-3-nodes
./bin/bootwright apply --stage infra-components
./bin/bootwright apply
./bin/bootwright status
./bin/bootwright machine list --power-status
./bin/bootwright machine exec --name rhel-01,rhel-02,rhel-03 cat /etc/os-release
./bin/bootwright diff --observe
./bin/bootwright apply
```

Initialization freezes the input copy and selects this context. For a later
session select it with
`context use --name lab-rhel-3-nodes`. Run lab-rhel's
[independent service checks](../lab-rhel/README.md#run-it) after the service
stage and before finishing apply. An unscoped apply finishes all remaining
stages. The final unchanged apply settles without contacting hosts or
repairing drift. Changed desired state requires destroy first, then
`context update --name lab-rhel-3-nodes --input-dir "$lab_rhel_three_input"`
to import the edited copy before applying again.

Independent service work and guest realizations can overlap; one package-tree
publication serves all three installations. An installation starts once its
own dependencies are ready, and installation waits can overlap. Graph waves
are a preview, not a barrier; a failure or unknown effect may delay other
work. Inspect progress and operation logs to establish actual overlap.

## Recovery and cleanup

Use `status` and the [recovery rules](../lab-rhel/README.md#interrupting-an-apply)
after interruption or failure. Repeat apply with unchanged frozen input to
observe unknown outcomes and retry failed work. Continue an interrupted
destroy with destroy. Do not delete the context to retry installation.
An automation-only rebuild continues: run `make build` and the same apply;
follow [changing the build between runs](../../../docs/operator-guide.md#changing-the-build-between-runs) rather than destroying.

Stop every realized guest before removal; an installer that cannot shut down
normally may need `machine stop --name <name> --force`. Then:

```sh
./bin/bootwright machine stop --name rhel-01,rhel-02,rhel-03
./bin/bootwright destroy --authorize data-loss
./bin/bootwright status
systemctl list-units --all 'bootwright-*'
./bin/bootwright context delete --name lab-rhel-3-nodes --purge
```

For an incomplete apply, stop only guests whose realization exists, and use
the authorization the destroy plan requires. Destroy deletes guest disks and
managed services/provider resources. A fresh apply after destroy can recreate
them; context deletion instead discards local state and secrets after removal.
Media stays host-wide: retain it for other contexts or use
[lab-rhel's media cleanup](../lab-rhel/README.md#undo) once no context reserves
it. Host packages and runtime remain. Restore local daemon/firewall changes
and remove the temporary input copy when finished.
