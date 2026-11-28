# Machine arm references

## InfraProvider arms

### vSphere arm

The [vSphere provider schema](../api/machines.md#vsphere-arm) owns this arm.

## Profile and segment fields

See [Machine profiles and segments](../api/machines.md#machine-profiles-and-segments).

## Machine fields

See [hardware and substrate](../api/machines.md#hardware-and-substrate).

## Template-clone installer arm

See [MachineInstallProfile](../api/machines.md#machineinstallprofile) and the
[vSphere lifecycle](../substrates.md#vsphere-machine-realization).

### Source ownership and clone completion

The publisher generalizes and stops a single-root RHEL VM before publication.
The image must support the selected cloud-init datasource on first boot.
The publisher removes machine and subscription identity, SSH host keys,
credentials and network state before publication. It keeps a published
revision immutable and readable until the clone completes. Bootwright reads
and clones the source through the provider credential. It never updates the
source or includes the source in a removal plan.

For KubeVirt, the provider verifies the cluster identity and reads the
declared `VirtualMachineSnapshot` by namespace and name. It requires the
authored UID, a ready snapshot, and one ready content object bound to it.
It resolves the sole persistent boot volume in the captured VM to one ready
CSI VolumeSnapshot. The source read checks the generated VM snapshot content
name against the published UID, the captured VM against the source UID, and the
CSI snapshot controller owner against that content. It checks the CSI binding by
name, namespace, and UID and returns the backing identities, driver and restore
size. The provider proof records those facts against its frozen request.
The Machine block takes the backing from that proof and reads it again
before submitting the clone and after the root DataVolume succeeds.
It verifies the target storage class uses the snapshot's CSI driver.
It refuses an undersized root or an existing root claim before creation.
A successful disk realization proves the VM remained stopped and its owned
root DataVolume populated an owned, bound claim. Removal uses the clone's
ownership record after publication is withdrawn. The source
and destination can occupy different namespaces. The operator grants the
provider's identity the snapshot reads and CDI source authorization in the
source namespace. The read of the bound CSI VolumeSnapshotContent uses a
cluster-scoped grant limited to the published backing's name. VM and
DataVolume privileges belong to the destination namespace. The VM submits
the resolved CSI VolumeSnapshot to CDI and receives an independent writable
root. The implementation must prove that CDI used the pinned backing across the
asynchronous population window before the first boot. If either source
identity changes during that window, it leaves the target stopped and
reports the changed source. It derives the VM hardware and networks from
the destination profile and Machine.
[Red Hat snapshot clone procedure](https://docs.redhat.com/en/documentation/openshift_container_platform/4.20/html/virtualization/advanced-vm-creation)
and [cross-namespace CDI authorization](https://docs.redhat.com/en/documentation/openshift_container_platform/4.20/html/virtualization/storage)
document the native mechanisms.
The [upstream snapshot controller](https://github.com/kubevirt/kubevirt/blob/v1.5.1/pkg/storage/snapshot/snapshot.go)
defines the generated content name and CSI owner reference.

For vSphere, the provider resolves one published inventory template by
absolute path through verified vCenter TLS. A qualified backing identity
must prove the expected root disk revision. The VM configuration revision
alone does not prove disk contents. The clone task includes the operation
ownership mark, explicit destination and independent disk placement, and
creates a stopped VM. A lost reply leaves the operation unresolved until
the provider proves the original task and target ownership.
[Clone specification](https://developer.broadcom.com/xapis/vsphere-web-services-api/latest/vim.vm.CloneSpec.html)
and [disk backing identity](https://developer.broadcom.com/xapis/vsphere-web-services-api/latest/vim.vm.device.VirtualDisk.FlatVer2BackingInfo.html)
describe the native mechanisms.

A clone first boots with its own cloud-init instance identity, bootstrap
network and authorized SSH client public key. The guest receives its
unique SSH host key through an authenticated private delivery channel
qualified for the provider. The adapter proves first boot over pinned SSH
and verifies the requested OS and network facts before reporting ready.

The KubeVirt adapter renders a NoCloud seed for one statically addressed
IPv4 interface matched by MAC. The seed mounts by reference to an immutable
Secret on the stopped VM with its recorded VM and root disk UIDs. The seed
holds a private HTTPS fetch URL and its trust anchor. The VM configuration
holds only the Secret reference. The guest checks the fetched host key
against its expected public half before installing it. First-boot
verification checks the same VM instance before and after pinned SSH and
reads the clone marker, a valid machine ID, the exact RHEL release,
architecture, hostname and address. Withdrawing the seed detaches its mount
from the stopped VM and deletes the exact Secret UID. A seedless restart
must prove a distinct VM instance with the same guest identity. The
lifecycle must record readiness and withdraw the publication and seed before
handing off the clone.

Destroy proves only recorded clone resources absent, using the existing
ownership and data-loss gates, and reaches that result after a publisher
retires the source. A staged destroy that would blank a retained clone's
root refuses until it has a qualified inverse.
