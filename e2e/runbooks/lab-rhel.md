# Managed RHEL guest on libvirt

One existing controller Machine hosts DNS, NTP, a proxy, an artifact server
and libvirt. Bootwright creates one 4-vCPU, 8-GiB guest with a 60-GiB disk,
boots it through an emulated Redfish BMC and installs RHEL 9.8 from a hosted
DVD package tree. The installation creates the `bootwright` account with the
fleet SSH key and records the installed host key for subsequent sessions.

The input is split into [environment](../../examples/machines/lab-rhel/environment.yaml),
[services](../../examples/machines/lab-rhel/infra/components), [Machines](../../examples/machines/lab-rhel/infra/machines),
[provider](../../examples/machines/lab-rhel/infra/providers/lab-libvirt.yaml),
[network](../../examples/machines/lab-rhel/infra/networkconfigs/lab-guests.yaml), [OS profiles](../../examples/machines/lab-rhel/infra/os) and
[Secret descriptors](../../examples/machines/lab-rhel/secret-descriptors). Descriptors contain no credentials;
this example generates every Secret locally in its context.

## Host prerequisites

- A Linux/amd64 Fedora, RHEL or derivative host that meets the current
  [controller admission requirements](../../specs/controller.md#supported-hosts),
  with working host repositories, required download access, and root or sudo.
  `setup` prepares baseline host packages and the locked Python runtime;
  `apply --stage controller` installs this graph's libvirt, hypervisor and
  installer-media prerequisites through the host's own dnf configuration.
- Hardware virtualization and `/dev/kvm`, plus the guest resources above beyond
  the host and four service containers. Allow space for both RHEL ISOs, the
  extracted DVD tree and temporary installer media as well as the guest disk.
- Entitled RHEL 9.8 x86_64 boot and DVD ISOs. They are operator-supplied;
  Bootwright does not download them for this example.
- An unused guest subnet and bridge, and free service and BMC sockets below.
  Run one of these labs at a time: other examples reuse these resources.

See the [operator guide](../../docs/operator-guide.md#prepare-a-host) for
build prerequisites, privileges and network egress. Independent DNS and time
checks below use `dig` and `chronyd`. These are runbook-only tools: the
temporary installation and cleanup below belong to the operator. Bootwright
installs the packages its own execution needs.

## Lab conventions

Copy and customize the input outside Git. Every address and name here is a
public documentation placeholder, not a usable host configuration.

| Input | Customize together |
| --- | --- |
| Controller `192.0.2.1` and `controller.lab.example.test` | The controller Machine, all four service bind addresses, and the artifact certificate's IP/DNS names. The IP must already exist on the host. |
| Resolver `192.0.2.53`, time source `192.0.2.123` | Reachable DNS forwarders and NTP upstreams, or remove those lists for an isolated lab. |
| Guest `198.51.100.11/24`, subnet `198.51.100.0/24`, gateway `198.51.100.1` | The guest Machine, NetworkConfig and provider attachment. The provider creates the NAT bridge `virbr-lab`; do not pre-create it. |
| `lab.example.test` | Environment base domain, Machine FQDNs, search domain and certificate DNS names. |
| Loopback BMC `127.0.0.1:8000` | Keep loopback binding; its generated certificate covers that address. |

Check listeners before applying:

```sh
ss -lntup | grep -E ':(53|123|3128|8000|8443|8080)\b'
virsh --connect qemu:///system net-list --all
```

The first command reports existing owners; the second requires the libvirt
client and may be run after the controller stage installs it. The controller
address needs free `53/tcp+udp`, `123/udp`, `3128/tcp`, `8443/tcp` and
`8080/tcp`; the BMC needs `8000/tcp` on loopback. A wildcard chronyd listener
can conflict with managed NTP. Configure its bind addresses, or stop it for
the lab and restore it afterward. Configure
[source-scoped firewall access](../../docs/operator-guide.md#network-exposure)
for guests; a service answering locally does not prove guest reachability.

On a FIPS-mode controller choose `rsa` or `ecdsaP256` for
`bootwright-machine-key`; see the
[FIPS guidance](../../docs/operator-guide.md#a-fips-mode-controller).

## Run it

From the repository root, copy the example and make the substitutions above:

```sh
lab_rhel_input=$(mktemp -d /tmp/bootwright-lab-rhel.XXXXXX)
cp -R examples/machines/lab-rhel/. "$lab_rhel_input/"
```

Use a current `./bin/bootwright` executable (`make build` builds it from this
checkout). Keep the variable in this shell, replace the ISO paths, and run the
commands separately. Mutation commands present their plan and confirmation;
sudo authorization may also be needed.

Use the same Bash session for the following package preparation and cleanup.
Before installing the diagnostic tools, save which packages were already
present in a private directory outside the input copy. Keep the printed path
if the session is interrupted; create this record only once for this run.

```bash
lab_rhel_checks=$(mktemp -d /tmp/bootwright-lab-rhel-checks.XXXXXX)
printf 'Diagnostic package record: %s\n' "$lab_rhel_checks"
(
  set -e
  rpm -qa --qf '%{NAME}\n' > "$lab_rhel_checks/before"
  : > "$lab_rhel_checks/added"
  for lab_rhel_check_package in bind-utils chrony; do
    if ! grep -Fxq "$lab_rhel_check_package" "$lab_rhel_checks/before"; then
      printf '%s\n' "$lab_rhel_check_package" >> "$lab_rhel_checks/added"
    fi
  done
)
```

Install the tools from that record. If the transaction fails or is
interrupted, stop before applying the lab and retain the record: installation
may have partly succeeded. Retry only this block with the same record path,
without taking another baseline.

```bash
(
  set -e
  mapfile -t lab_rhel_check_packages < "$lab_rhel_checks/added"
  if ((${#lab_rhel_check_packages[@]})); then
    sudo dnf install --setopt=assumeyes=False --setopt=defaultyes=False \
      --setopt=install_weak_deps=False -- "${lab_rhel_check_packages[@]}"
  fi
)
```

Review the package transaction before confirming it. Package installation can
affect host services; check the listener ownership described above before
applying the lab.

```sh
./bin/bootwright version
./bin/bootwright validate -f "$lab_rhel_input"
./bin/bootwright setup
./bin/bootwright media add --name rhel-9.8-x86_64-boot.iso --from-file /path/to/rhel-9.8-x86_64-boot.iso
./bin/bootwright media add --name rhel-9.8-x86_64-dvd.iso --from-file /path/to/rhel-9.8-x86_64-dvd.iso
./bin/bootwright media list --checksums
./bin/bootwright context init --name lab-rhel --input-dir "$lab_rhel_input"
./bin/bootwright secret generate
./bin/bootwright secret check
./bin/bootwright plan
./bin/bootwright apply --stage controller
./bin/bootwright preflight controller --context lab-rhel
./bin/bootwright apply --stage infrastructure-services
```

`context init` selects the new context and freezes the input copy.
Reuse an existing context with `context use --name lab-rhel` instead of
initializing it again. `plan` previews every block before anything runs; a
stage gates which blocks can start, without removing their dependencies. The
controller apply prepares context prerequisites and pauses, and while that
operation is paused `context update` refuses until it completes or is
destroyed, so review the full plan first.

After the service apply, substitute the customized controller address in
these independent checks:

```sh
curl -sk https://192.0.2.1:8443/ -o /dev/null -w '%{http_code}\n'
dig @192.0.2.1 controller.lab.example.test +short
dig @192.0.2.1 +tcp rhel-01.lab.example.test +short
chronyd -Q -t 10 'server 192.0.2.1 iburst port 123'
systemctl list-units 'bootwright-*'
```

An empty artifact root returns `404`; `curl -k` checks reachability without
verifying its certificate. DNS should return the configured addresses. NTP
prints an offset once the service has selected an upstream. Then finish:

```sh
./bin/bootwright apply
./bin/bootwright status
./bin/bootwright machine list --power-status
./bin/bootwright machine exec --name rhel-01 cat /etc/os-release
./bin/bootwright machine rsh --name rhel-01
./bin/bootwright diff --observe
./bin/bootwright apply
```

Leave `rsh` with `exit`. The unscoped apply creates the provider and guest,
publishes the shared package tree and installs RHEL. The final apply settles
an unchanged completed operation without contacting hosts or repairing drift;
`diff --observe` reports drift. Changed input requires destroy before another
apply. After destroy, import edits with
`context update --name lab-rhel --input-dir "$lab_rhel_input"` before applying;
source-copy edits alone never change the frozen context.

For a power check, run `machine restart --name rhel-01`, then repeat
`machine exec --name rhel-01 uptime` until SSH answers. Restart returns at
proved power-on, before OS boot completes. A fresh apply after destroy is also
supported. Host-restart qualification is tracked separately in the
[acceptance ledger](../../docs/acceptance.md); this runbook does not claim
that every reboot path has passed its operator gate.

## Interrupting an apply

Run `status` first. A failed block is retried by the next `apply`; an unknown
outcome is observed against the frozen request before any retry. Keep the
imported input and bound Secret/media versions unchanged during continuation.
If the observation remains unknown, repair the reported reachability or trust
problem and repeat the same verb; there is no force continuation.
An automation-only rebuild continues: run `make build` and the same apply;
follow [changing the build between runs](../../docs/operator-guide.md#changing-the-build-between-runs) rather than destroying.

To abandon an incomplete apply, stop any guest it created and run the destroy
below. Destroy resolves uncertain effects and proves the guest stopped before
registering removal; a live guest refuses `lifecycle.live` and names its stop
command. A failed installation may need `machine stop --name rhel-01 --force`
when the installer cannot shut down normally. An interrupted destroy continues
with `destroy`, not `apply`.

## Undo

```sh
./bin/bootwright machine stop --name rhel-01
./bin/bootwright destroy --authorize data-loss
./bin/bootwright status
systemctl list-units --all 'bootwright-*'
./bin/bootwright context delete --name lab-rhel --purge
```

Stop the guest only if its realization exists. Destroy removes the managed
services, provider network, guest and guest disks; disk deletion requires
`data-loss` independently of confirmation. For a partial apply, use the
authorization its removal plan requires. Verify removal before deleting the
context: deletion removes local state and secrets, not remote resources.

Media is host-wide and remains reserved until every consuming context is
destroyed. Keep it for other examples, or remove it afterward:

```sh
./bin/bootwright media delete --name rhel-9.8-x86_64-boot.iso
./bin/bootwright media delete --name rhel-9.8-x86_64-dvd.iso
```

Bootwright's host packages and prepared runtime remain installed. Remove only
the diagnostic packages this run added. If resuming in another Bash session,
set `lab_rhel_checks` to the printed record directory first.

```bash
(
  set -e
  mapfile -t lab_rhel_check_packages < "$lab_rhel_checks/added"
  if ((${#lab_rhel_check_packages[@]})); then
    sudo dnf remove --setopt=assumeyes=False --setopt=defaultyes=False \
      --setopt=clean_requirements_on_remove=False -- "${lab_rhel_check_packages[@]}"
  fi
)
```

At the confirmation prompt, continue only if every package to be removed is
in the saved `added` list. Cancel if other packages would be removed, including
software that now depends on these tools. Packages present before the run are
excluded from the requested removal list. The
[DNF cleanup setting](https://dnf5.readthedocs.io/en/latest/commands/remove.8.html)
keeps dependency cleanup off; dependencies installed alongside the tools stay
installed. Retain the record if cleanup fails or is declined.

Restore any host daemon or firewall configuration changed for the lab. After
successful cleanup, remove the diagnostic record directory and the temporary
input copy when no longer needed.
