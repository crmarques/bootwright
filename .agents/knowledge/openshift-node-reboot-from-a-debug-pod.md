# Rebooting an OpenShift node from a debug pod

`oc debug node/<node> -- chroot /host systemctl reboot` reboots the node
before the debug pod's container exits, so the kubelet never records its end.
After the reboot the kubelet finds the pod still bound to the node with no
terminated container, starts it again, and the node reboots again. In
[X302](../../specs/milestones/delivered.md#x302--two-isolated-environments-on-one-node)'s run the node booted twelve times in 43 minutes, each boot logging
the same `default/master-0-debug-<id>` container followed by
`systemd-logind: System is rebooting.`. It stopped only when the pod reached
`ContainerStatusUnknown`. A wait on the node's `bootID` looks hung through
the loop, and `journalctl --list-boots` on the node shows it.

**Decision it informs.** A runbook reboots a node with
`oc debug node/<node> -- chroot /host shutdown -r +1`: the command returns at
once, the pod completes, and the node reboots a minute later. After a loop,
delete the leftover debug pod. The TPM2 checks of
[lab-sno-fips-controller](../../e2e/lab-sno-fips-controller/runbook.md)
use this form.

Observed on OpenShift 4.22.21 (RHCOS 9.8 kernel 5.14.0-687) on libvirt.
