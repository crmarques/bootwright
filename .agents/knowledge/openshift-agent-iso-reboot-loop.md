# OpenShift Agent ISO Reboot Loop

## Symptom

During `openshift-install agent wait-for install-complete`, the install reaches
`Writing image to disk: 100%` and then repeatedly logs:

```text
Unable to retrieve cluster metadata from Agent Rest API: [GET /v2/clusters/{cluster_id}][404] v2GetClusterNotFound
```

## Likely Cause

The rendezvous host rebooted into the agent ISO again after the disk write.
The installer cached the original Assisted Service cluster ID, but the fresh
ISO boot starts a new in-memory Assisted Service database, so the old
`/v2/clusters/{cluster_id}` lookup returns 404.

For Redfish virtual media paths, this usually means the one-shot CD boot
override still won the post-write reboot. For libvirt through
sushy-emulator, `BootSourceOverrideEnabled=Once` is ignored and setting the
boot target to `Cd` becomes a persistent libvirt boot order change.
Later Redfish patches set the inactive domain definition back to disk, but
libvirt cannot change disk boot order on the already-running VM, so the active
domain can still keep the CD-first boot order used by an in-guest reboot.

## Fix

For libvirt-backed emulated Redfish, do not set a Redfish CD boot override.
Keep the libvirt domain disk-first with CD fallback before power-on; an empty
install disk falls through to the attached ISO, and the post-write reboot then
boots the newly written disk. For non-libvirt Redfish, change subsequent boots
back to disk with a Redfish boot-device PATCH after the live ISO reaches SSH.
Keep virtual media attached while `openshift-install agent wait-for
install-complete` runs. Do not eject the live ISO as the mechanism for
preventing reboot loops; newer RHCOS live images can still read squashfs pages
from virtual media after SSH is available.
