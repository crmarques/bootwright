package ceph

import (
	"slices"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

var (
	cephRequestInvalid = diagnostics.Stop{Code: "ceph.request-invalid", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "a Ceph request, its frozen identity or its planning state breaks its bounds or its shape, so no adapter ran", Remedy: "report the message as a defect"}
	cephUnconfigured = diagnostics.Stop{Code: "ceph.unconfigured", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryNone,
		Meaning: "a Ceph capability runs without an adapter, a proof reader or a bound Secret it needs", Remedy: "report the message as a defect"}
	cephRecordsMissing = diagnostics.Stop{Code: "ceph.records-missing", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryAbandonContext,
		Meaning: "a Ceph block holds no original apply, identity or proof that its operation needs, or holds an ambiguous one", Remedy: "restore the context store from a backup"}
	cephEvidenceMismatch = diagnostics.Stop{Code: "ceph.evidence-mismatch", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the evidence a Ceph block holds names another request or does not prove what the block froze", Remedy: "repeat the operation, which observes the Ceph state again"}
	cephProofMismatch = diagnostics.Stop{Code: "ceph.proof-mismatch", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the proof of an earlier Ceph block names another request, operation or host set than the block that reads it", Remedy: "restore the original apply evidence"}
	cephOtherBuild = diagnostics.Stop{Code: "ceph.other-build", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseBuild, Recovery: diagnostics.RecoveryRegisteringBuild,
		Meaning: "a Ceph block froze values this build does not derive or read, so no adapter ran", Remedy: "destroy it with the build that applied it, then apply with this build"}
	cephHostKeyInvalid = diagnostics.Stop{Code: "ceph.host-key-invalid", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseRecords, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the SSH host key a Ceph host delivered or its Secret pins is not one usable entry", Remedy: "bind the host's key through its access.ssh.knownHostsRef, then repeat the operation"}
	cephDisksShared = diagnostics.Stop{Code: "ceph.disks-shared", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "physical Ceph hosts report one disk identity, so an OSD service would claim a disk shared by hosts", Remedy: "select independently owned local disks"}
	cephFipsUnproved = diagnostics.Stop{Code: "ceph.fips-unproved", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "a Ceph host no longer proves the installed FIPS kernel and crypto policy the operation froze", Remedy: "restore the installed FIPS policy"}
	cephUnsupportedRelease = diagnostics.Stop{Code: "ceph.unsupported-release", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "the selected Ceph coordinates or cluster SSH transport are incomplete or unsafe, so no adapter ran", Remedy: "declare bounded version strings, an image repository and verifiedTunnel transport"}
	cephLicenseUnaccepted = diagnostics.Stop{Code: "ceph.license-unaccepted", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "the IBM license of the Ceph entitlement has not been accepted, so no adapter ran", Remedy: "review the provider license and declare Entitlement.spec.license.accept"}
	cephImageInvalid = diagnostics.Stop{Code: "ceph.image-invalid", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a Ceph image or its registry does not match the entitlement or the digest the input authored", Remedy: "select an image beneath the Entitlement registry by a tag or a sha256 digest"}
	cephUnsupportedProfile = diagnostics.Stop{Code: "ceph.unsupported-profile", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "the requested distribution, trust profile or kind is one the core Ceph adapter does not realize", Remedy: "select the IBM Storage Ceph distribution and the core profile"}
	cephUnsupportedTopology = diagnostics.Stop{Code: "ceph.unsupported-topology", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "the hosts, SSH keys, addresses or provided Machines a Ceph cluster selects are not a topology the core adapter realizes", Remedy: "declare a topology the core Ceph profile supports"}
	cephUnsupportedService = diagnostics.Stop{Code: "ceph.unsupported-service", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a native Ceph service selects a type, option, placement or binding the core adapter does not realize", Remedy: "declare only the services and options the core Ceph profile supports"}
	cephUnsupportedDisk = diagnostics.Stop{Code: "ceph.unsupported-disk", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "an OSD service or disk selection is not one the core adapter allocates", Remedy: "select explicit owned data disks the core Ceph profile supports"}
	cephUnsupportedPool = diagnostics.Stop{Code: "ceph.unsupported-pool", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a Ceph pool, placement policy or consumer names a setting or pool kind the core adapter does not realize", Remedy: "select a replicated pool and the settings the core Ceph profile supports"}
	cephPackageSourceUnsupported = diagnostics.Stop{Code: "ceph.package-source-unsupported", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "the package source a Ceph host selects is not one the core adapter can acquire packages from", Remedy: "select the hosted DVD or subscription profile used to install each Ceph Machine"}
	cephBudgetExceeded = diagnostics.Stop{Code: "ceph.budget-exceeded", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseBound, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a Ceph operation needs a run deadline past the bound Ceph holds its derived deadlines to, so no adapter ran", Remedy: "declare fewer hosts, namespaces or initiator hosts"}
	cephRetirementUnavailable = diagnostics.Stop{Code: "ceph.retirement-unavailable", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "the planned Ceph cluster would refuse automatic retirement", Remedy: "review the original erase policy and cluster consumers before apply"}
	cephRetirementRefused = diagnostics.Stop{Code: "ceph.retirement-refused", Level: diagnostics.LevelRemoval, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a physical Ceph host holds no disk-erasure authority that covers every selected disk, so its retirement took nothing back", Remedy: "retain this context and erase every selected Ceph disk"}
	cephHostsSurvive = diagnostics.Stop{Code: "ceph.hosts-survive", Level: diagnostics.LevelRemoval, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "a Ceph cluster would be removed while hosts that outlive the destroy still run it", Remedy: "remove the cluster in the same destroy as every Bootwright-owned virtual host Machine it runs on"}
	cephSecretInvalid = diagnostics.Stop{Code: "ceph.secret-invalid", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "a Secret bound to a Ceph resource is not a value the adapter can lend", Remedy: "bind a Secret holding one line of 1 to 1024 bytes of UTF-8 text"}
	cephStretchIntent = diagnostics.Stop{Code: "ceph.stretch-intent-changed", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "the stretch topology the input declares differs from the intent the cluster was created with", Remedy: "restore the stretch topology the cluster was created with"}
	cephSSHPolicyExcluded = diagnostics.Stop{Code: "ceph.ssh-policy-excluded", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseInput, Recovery: diagnostics.RecoveryChangeInput,
		Meaning: "an SSH crypto policy admits none of the algorithms of a Ceph host's pinned host key, so no session opened", Remedy: "pin a host key the SSH crypto policy admits"}
	cephSSHPolicyUnreadable = diagnostics.Stop{Code: "ceph.ssh-policy-unreadable", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyNone,
		Cause: diagnostics.CauseEnvironment, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the controller's SSH crypto policy could not be read or evaluated, so no Ceph session opened", Remedy: "restore the controller's SSH crypto policy files, then repeat the operation"}
	cephDeviceRefused = diagnostics.Stop{Code: "ceph.device-refused", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "a disk a Ceph host selects for an OSD is not one the operation may allocate, so the adapter stopped", Remedy: "select empty disks that the host presents by their declared identity"}
	cephUnqualified = diagnostics.Stop{Code: "ceph.unqualified", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible,
		Cause: diagnostics.CauseBuild, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the selected Ceph build or host lacks a capability the operation needs, so the adapter stopped", Remedy: "select a Ceph build whose release qualification covers the operation"}
	cephForeign = diagnostics.Stop{Code: "ceph.identity-foreign", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the cluster holds a resource of a declared name that this operation did not create", Remedy: "rename the declared resource, or remove the foreign one outside Bootwright, then apply again"}
	cephDrifted = diagnostics.Stop{Code: "ceph.identity-drifted", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "a resource this operation created is gone or no longer matches what the operation froze", Remedy: "restore the resource to its declared state outside Bootwright, then apply again"}
	cephPrerequisite = diagnostics.Stop{Code: "ceph.prerequisite", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the cluster or its hosts lack a state the operation needs before it may continue", Remedy: "restore the state the message names, then apply again"}
	cephNotReady = diagnostics.Stop{Code: "ceph.not-ready", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "a native Ceph command refused the operation or its daemons did not reach readiness within the bound", Remedy: "correct the cause the native error names outside Bootwright, then apply again"}
	cephOSDUnproved = diagnostics.Stop{Code: "ceph.osd-unproved", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "an OSD disk this operation prepared or cleared did not prove its state", Remedy: "inspect the disk with wipefs --no-act and lvmdevices, then apply again"}
	cephPackageUnproved = diagnostics.Stop{Code: "ceph.package-unproved", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryFixThenRepeat,
		Meaning: "the Ceph package preparation of a host refused its request, source or result, so the host's packages are unproved", Remedy: "inspect the host's package journal and the declared package sources, then apply again"}
	cephBoundaryUnproved = diagnostics.Stop{Code: "ceph.boundary-unproved", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "an adapter could not prove one of its boundaries, so what it did is unproved", Remedy: "inspect the retained operation evidence, then repeat the operation"}
	cephEnrollmentBudget = diagnostics.Stop{Code: "ceph.enrollment-budget", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseBound, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the enrollment worker had too little of its budget left to admit a host", Remedy: "apply again to continue the prepared cluster enrollment"}
	cephConsumerIncomplete = diagnostics.Stop{Code: "ceph.consumer-incomplete", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "a Ceph consumer created nothing yet or began and did not complete", Remedy: "repeat the operation, which observes the consumer before anything else starts"}
	cephUnproved = diagnostics.Stop{Code: "ceph.unproved", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "a Ceph adapter ended without proving its postcondition", Remedy: "repeat the operation, which observes the Ceph state before anything else starts"}
	cephResultInvalid = diagnostics.Stop{Code: "ceph.result-invalid", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseDefect, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "a Ceph adapter returned an outcome or a value it never produces", Remedy: "repeat the operation, which observes the Ceph state before anything else starts"}
	cephEvidenceUnproved = diagnostics.Stop{Code: "ceph.evidence-unproved", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "the evidence of a Ceph block does not prove the ownership and resource scope the block froze", Remedy: "repeat the operation, which observes the Ceph state before anything else starts"}
	cephTiebreakerUnproved = diagnostics.Stop{Code: "ceph.tiebreaker-unproved", Level: diagnostics.LevelBlock, Certainty: diagnostics.CertaintyUnproved,
		Cause: diagnostics.CauseTarget, Recovery: diagnostics.RecoveryRepeat,
		Meaning: "a provided stretch tiebreaker Machine did not prove its Ceph footprint, its transport boundary or its removal", Remedy: "repeat the operation, which observes the tiebreaker before anything else starts"}
)

var stops = []diagnostics.Stop{
	cephRequestInvalid, cephUnconfigured, cephRecordsMissing, cephEvidenceMismatch, cephProofMismatch, cephOtherBuild, cephHostKeyInvalid, cephDisksShared, cephFipsUnproved,
	cephUnsupportedRelease, cephLicenseUnaccepted, cephImageInvalid, cephUnsupportedProfile, cephUnsupportedTopology, cephUnsupportedService, cephUnsupportedDisk, cephUnsupportedPool,
	cephPackageSourceUnsupported, cephBudgetExceeded, cephRetirementUnavailable, cephRetirementRefused, cephHostsSurvive, cephSecretInvalid, cephStretchIntent, cephSSHPolicyExcluded, cephSSHPolicyUnreadable,
	cephDeviceRefused, cephUnqualified, cephForeign, cephDrifted, cephPrerequisite, cephNotReady, cephOSDUnproved, cephPackageUnproved, cephBoundaryUnproved, cephEnrollmentBudget,
	cephConsumerIncomplete, cephUnproved, cephResultInvalid, cephEvidenceUnproved, cephTiebreakerUnproved,
}

func Stops() []diagnostics.Stop { return slices.Clone(stops) }
