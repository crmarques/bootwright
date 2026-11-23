# Controller teardown and manual recovery

Use the case's destroy steps with the build that applied the context. If
the controller is itself a guest, finish its workload cleanup and copy its
evidence to the outer host before shutting it down. Then destroy its owning
context from the outer controller, as in the [FIPS controller runbook](lab-sno-fips-controller/runbook.md#11-destroy-the-bootstrap-context-and-clean-up-host).

## Destroy a readable context

Set `BOOTWRIGHT_BIN` to the executable used for the run and `CONTEXT` to the
context on this controller. Use the case's cleanup order for fixtures and
guests. Some cases, such as [registered RHEL](lab-rhel/runbook.md#10-remove-the-installation),
need destroy to perform work while the guest still runs.

```sh
BW=${BOOTWRIGHT_BIN:-./bin/bootwright}
"$BW" status --context "${CONTEXT:?Set the context name}"
"$BW" destroy --context "$CONTEXT" --dry-run
```

Review the plan. For an attended libvirt destroy whose plan requests `data-loss`, run:

```sh
"$BW" destroy --context "$CONTEXT" --authorize data-loss
```

If it asks you to stop a Machine, follow the case's prerequisites for that
stop, then run `"$BW" machine stop --context "$CONTEXT" --name "$MACHINE"`
with the exact Machine name and retry the destroy. Use the case's authorization
and cleanup steps if its plan calls for anything else. After destroy completes,
check final status and archive the evidence before deleting the context.

```sh
"$BW" status --context "$CONTEXT"
EVIDENCE_CONTEXT=$CONTEXT
```

Complete [Preserve evidence before context purge](README.md#preserve-evidence-before-context-purge), then run:

```sh
"$BW" context delete --name "$CONTEXT" --purge
```

## Remove orphaned libvirt resources by hand

Use these commands when the original build and its recovery steps cannot
finish destroy and the lab resources are confirmed disposable. Stop all
Bootwright work for the affected resources. Save the available status, logs,
and ownership records privately before changing anything. Check both the
controller and any separate libvirt placement host. A controller uninstall
only inspects its local host. This fallback applies to libvirt E2E resources.

On the host that owns the resources, inventory them using the case's provider
connection. The examples use the local system instance. Run one command at
a time and stop if a command fails or ownership is unclear.

```sh
sudo virsh --connect qemu:///system list --all
sudo virsh --connect qemu:///system net-list --all
sudo virsh --connect qemu:///system pool-list --all
sudo systemctl list-units --all 'bootwright-*'
sudo systemctl list-unit-files 'bootwright-*'
sudo podman ps --all
```

### Guest and emulated management controller

Set `DOMAIN` to one exact domain from the inventory. Match its XML ownership
metadata (`https://bootwright.io/substrate/v1`), UUID and disk paths against
the run's context and Machine records. Save the disk list before undefining
the domain.

```sh
sudo virsh --connect qemu:///system dumpxml --inactive -- "${DOMAIN:?Set the owned domain}"
sudo virsh --connect qemu:///system domblklist --inactive --details -- "$DOMAIN"
sudo virsh --connect qemu:///system domstate -- "$DOMAIN"
```

Use `sudo virsh --connect qemu:///system shutdown -- "$DOMAIN"` if the guest is
running. Wait until `domstate` reports `shut off`. Stop and remove its emulated
management controller first if it exists. The same Quadlet commands apply to
a run-owned managed service after its consumers are gone. Set `QUADLET` to
the exact source basename without `.container`, then check the source path and
container name against the run's records before changing them.

```sh
sudo systemctl cat -- "${QUADLET:?Set the owned Quadlet basename}.service"
```

After verifying ownership:

```sh
sudo systemctl stop -- "${QUADLET:?Set the owned Quadlet basename}.service"
sudo rm -- "/etc/containers/systemd/$QUADLET.container"
sudo systemctl daemon-reload
```

If its exact container still exists, inspect it with `sudo podman inspect --
"$CONTAINER"` and remove it with `sudo podman rm -- "$CONTAINER"` after it stops.
For a native systemd unit, use `systemctl cat` to identify its owned source,
then disable and stop the exact unit, remove its source and reload systemd.

Once the guest is shut off and its management controller is stopped, remove
the domain and its firmware state:

```sh
sudo virsh --connect qemu:///system undefine --nvram --tpm -- "${DOMAIN:?Set the owned domain}"
```

For each disposable disk, set `DISK` to the exact absolute file path from the
saved inventory. Verify it belongs to this Machine under the run's libvirt
image directory, check the path and its parents for symlinks, and check
remaining domains and disk backing chains for references. Remove only the
proved files with `sudo rm -- "${DISK:?Set the owned disk path}"`. Keep shared
backing images.

### Network and virtual-media pool

Inspect the network's ownership metadata and the pool's XML target path.
Compare them to the run's provider records. Check that no remaining domain
uses the network or bridge and no guest or controller uses the pool. Set
`NETWORK` and `POOL` to the exact owned names and run only the commands for
objects that remain. For an active pool, review its files with `sudo virsh
--connect qemu:///system vol-list --details -- "$POOL"`. Skip `net-destroy`
or `pool-destroy` if the object is already inactive.

```sh
sudo virsh --connect qemu:///system net-dumpxml --inactive -- "${NETWORK:?Set the owned network}"
sudo virsh --connect qemu:///system pool-dumpxml --inactive -- "${POOL:?Set the owned pool}"
```

```sh
sudo virsh --connect qemu:///system net-destroy -- "${NETWORK:?Set the owned network}"
sudo virsh --connect qemu:///system net-undefine -- "$NETWORK"
sudo virsh --connect qemu:///system pool-destroy -- "${POOL:?Set the owned pool}"
sudo virsh --connect qemu:///system pool-undefine -- "$POOL"
```

Pool undefine leaves the files on disk. Remove only verified run-owned media
files and use `rmdir` on their empty directories. Review the case's cleanup for
any hosted files, firewall services, managed VLANs, and external fixtures.
Keep operator-owned networks, rules, and services. The libvirt
[Machine removal role](../ansible/collections/ansible_collections/bootwright/core/roles/substrate_libvirt_machine/tasks/destroy.yml)
and [provider removal role](../ansible/collections/ansible_collections/bootwright/core/roles/substrate_libvirt_host/tasks/destroy.yml)
show the ownership checks and dependency order for advanced cases.

Repeat the inventory and verify the run's resources are gone on each placement
host. If the context is readable, return to the controller and retry destroy
and ordinary context purge. If Bootwright cannot prove cleanup, use the
[documented orphan acknowledgement](../docs/operator-guide.md#changing-the-build-between-runs)
only after confirming cleanup and archiving the evidence.

```sh
BW=${BOOTWRIGHT_BIN:-./bin/bootwright}
"$BW" context delete --name "${CONTEXT:?Set the context name}" --purge --allow-orphans
```

## Remove the shared controller installation

Run this on the controller only when retiring its shared state. Check that
no contexts remain, then delete each imported image by exact name after
checking it is no longer needed. Keep the original operator-supplied files.

```sh
BW=${BOOTWRIGHT_BIN:-./bin/bootwright}
"$BW" context list
"$BW" catalog media list
```

```sh
"$BW" catalog media delete --name "${IMAGE:?Set the imported image name}"
```

Preview and run uninstall with `--keep-packages` to reset controller state
for a fresh run while retaining the host packages. For host retirement, omit
`--keep-packages` from both commands to also remove install-owned packages.

```sh
"$BW" uninstall --dry-run --keep-packages
"$BW" uninstall --keep-packages
```

For an unreadable store, recover its original build first if possible. Once
all its environments and placement resources are verified gone, follow the
[uninstall flow](../specs/controller.md#the-uninstall-flow). Discard takes any
remaining guest disks under the local Bootwright image prefix and keeps host
packages. Review the preview before running the second command.

```sh
"$BW" uninstall --dry-run --discard-unreadable
"$BW" uninstall --discard-unreadable
```
