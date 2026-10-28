package kubevirt

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

// HostImplementation and MachineImplementation are the frozen identities of the
// two capabilities this package offers. A plan records one per block, and a
// continuation refuses when the executable no longer offers it.
const (
	HostImplementation    = "substrate-host-kubevirt-v1"
	MachineImplementation = "machine-kubevirt-v1"
)

// HostKind and MachineKind are the API kinds the two capabilities realize.
// Other implementations claim both, so a block resolves by kind and
// implementation together.
const (
	HostKind    = "InfraProvider"
	MachineKind = "Machine"
)

const (
	hostRequestVersion    = "substrate-host-kubevirt-v1"
	machineRequestVersion = "machine-kubevirt-v1"
	hostEvidenceVersion   = "substrate-host-kubevirt-evidence-v1"
	machineEvidence       = "machine-kubevirt-evidence-v1"
)

// verbs are the API permissions the arm's blocks and port entry points use in
// the provider's namespace, each proved by one SelfSubjectAccessReview before
// any Machine of the provider is touched.
var verbs = []Access{
	{Group: "kubevirt.io", Resource: "virtualmachines", Verb: "create"},
	{Group: "kubevirt.io", Resource: "virtualmachines", Verb: "delete"},
	{Group: "kubevirt.io", Resource: "virtualmachines", Verb: "get"},
	{Group: "kubevirt.io", Resource: "virtualmachines", Verb: "patch"},
	{Group: "kubevirt.io", Resource: "virtualmachineinstances", Verb: "get"},
	{Group: "subresources.kubevirt.io", Resource: "virtualmachines", Subresource: "start", Verb: "update"},
	{Group: "subresources.kubevirt.io", Resource: "virtualmachines", Subresource: "stop", Verb: "update"},
	{Group: "cdi.kubevirt.io", Resource: "datavolumes", Verb: "create"},
	{Group: "cdi.kubevirt.io", Resource: "datavolumes", Verb: "delete"},
	{Group: "cdi.kubevirt.io", Resource: "datavolumes", Verb: "get"},
	{Group: "upload.cdi.kubevirt.io", Resource: "uploadtokenrequests", Verb: "create"},
	{Resource: "persistentvolumeclaims", Verb: "list"},
}

// HostBlockID and MachineBlockID name the blocks each capability contributes.
// A consumer states requirements by API object, never by these identities.
func HostBlockID(provider string) string {
	return reconciliation.BlockIdentity("substrate-host-", provider)
}

func MachineBlockID(machine string) string { return reconciliation.BlockIdentity("machine-", machine) }

// HostContentDigest and MachineContentDigest bind a plan to the exact behavior
// this build implements: the request shape, the proof's checks and the
// VirtualMachine the arm creates.
func HostContentDigest() string {
	return contentDigest("bootwright.substrate.kubevirt.host-v1", HostImplementation, hostRequestVersion, hostEvidenceVersion,
		"access-reviews", "root-class-rwx", "kube-system-uid", "ingress-ca")
}

func MachineContentDigest() string {
	return contentDigest("bootwright.substrate.kubevirt.machine-v1", MachineImplementation, machineRequestVersion, machineEvidence,
		"run-strategy-manual", "efi-no-secure-boot", "evict-live-migrate", "bridge-binding", "root-template-blank", "owner-annotation-mark")
}

func contentDigest(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}
