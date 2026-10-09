# Three RHEL guests on libvirt

This extends [lab-rhel](../lab-rhel/README.md) to three guests sharing the
`rhel-9-8` install profile. The addresses and names are documentation examples.
Adjust a copy for the host, following lab-rhel's prerequisites, media and network
conventions. Reserve 12 vCPUs, 24 GiB of guest memory and three 60 GiB disks in
addition to the controller, service containers and media. The emulated BMCs
use loopback ports `8000`, `8001` and `8002`.

Run one example per host: this one shares service sockets and the guest bridge
with lab-rhel and lab-sno. Copy it outside the checkout before editing:

```sh
lab_rhel_three_input=$(mktemp -d /tmp/bootwright-lab-rhel-three.XXXXXX)
cp -R examples/lab-rhel-3-nodes/. "$lab_rhel_three_input/"
```

After adjusting that copy, run each command separately from the repository
root. Use the clean M1 build; retire live contexts with their original build
before installing this build's automation, as the
[upgrade guidance](../../specs/milestones.md#status) requires.

```sh
make build
./bin/bootwright version
./bin/bootwright setup
./bin/bootwright media add --name rhel-9.8-x86_64-boot.iso --from-file /path/to/rhel-9.8-x86_64-boot.iso
./bin/bootwright media add --name rhel-9.8-x86_64-dvd.iso --from-file /path/to/rhel-9.8-x86_64-dvd.iso
./bin/bootwright context init --name lab-rhel-3-nodes
./bin/bootwright context update --name lab-rhel-3-nodes --input-dir "$lab_rhel_three_input" --yes
./bin/bootwright secret generate
./bin/bootwright secret check
./bin/bootwright apply --stage controller
./bin/bootwright preflight controller --context lab-rhel-3-nodes
./bin/bootwright plan
./bin/bootwright apply --stage infra-components
./bin/bootwright apply
./bin/bootwright status
./bin/bootwright machine list --power-status
./bin/bootwright machine exec --name rhel-01 cat /etc/os-release
./bin/bootwright machine exec --name rhel-02 cat /etc/os-release
./bin/bootwright machine exec --name rhel-03 cat /etc/os-release
./bin/bootwright apply
./bin/bootwright machine stop --name rhel-01
./bin/bootwright machine stop --name rhel-02
./bin/bootwright machine stop --name rhel-03
./bin/bootwright destroy --authorize data-loss
./bin/bootwright status
systemctl list-units --all 'bootwright-*'
systemctl list-units --state=failed 'bootwright-*'
```

The executable admits up to eight blocks at once. The four independent service
blocks can run together; once the provider is ready, the three machine
realizations can run together. One package-tree publication serves all three
installations. An installation starts as soon as its own machine and required
services and tree are ready, while other independent work continues. All three
installer waits can overlap. A dependency, exclusive resource, failed retry or
unknown effect can still delay admission; graph waves are a preview, not a
barrier between whole groups of blocks.

For [M1 closing validation](../../docs/operator-guide.md#m1-closing-validation),
keep timestamped operation progress and logs locally. Prove overlapping service
and machine work, exactly one shared publication and overlapping installation
waits. Repeat the apply, then stop every guest and destroy; confirm no managed
units, shared tree, machine work or adapter scratch remain. Repeat the fresh
apply and host restart checks from lab-rhel for all three guests. A faster
overall runtime or a wide plan wave alone does not prove overlap.

After completing those fresh apply and host restart checks, stop all three
guests, destroy the context's managed resources and check their removal before
deleting the context:

```sh
./bin/bootwright machine stop --name rhel-01
./bin/bootwright machine stop --name rhel-02
./bin/bootwright machine stop --name rhel-03
./bin/bootwright destroy --authorize data-loss
./bin/bootwright status
systemctl list-units --all 'bootwright-*'
./bin/bootwright context delete --name lab-rhel-3-nodes --purge
```

The media store is host-wide. Keep the images for another example or delete
them using lab-rhel's undo commands after every consuming context is destroyed.
