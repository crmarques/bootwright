package ceph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/trust"
)

type Runner interface {
	Run(context.Context, lifecycle.RunRequest) (lifecycle.RunResult, error)
}

const (
	PinnedRelease        = "9.9.1.2"
	PinnedPackageVersion = "20.2.1-405.el9cp"
	PinnedAnsibleVersion = "5.0.3-1"
	PinnedCephVersion    = "20.2.1"
	PinnedRepository     = "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9"
	PinnedRegistry       = "cp.icr.io"
	PinnedTag            = "v9.9.1-25074"
	PinnedTransport      = "verified-tunnel"
	PinnedPlatform       = "linux/amd64"
)

func ValidImageDigest(value string) bool {
	digits, found := strings.CutPrefix(value, "sha256:")
	if !found || len(digits) != 64 || digits == strings.Repeat("0", 64) {
		return false
	}
	for _, c := range digits {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func Supported(release, packageVersion, ansibleVersion, base, version, transport string) bool {
	return release == PinnedRelease && packageVersion == PinnedPackageVersion && ansibleVersion == PinnedAnsibleVersion &&
		base == PinnedRepository && (version == PinnedTag || ValidImageDigest(version)) && transport == PinnedTransport
}

type DependencyProofs interface {
	DataDisk(reconciliation.Block, []byte, string, string, int64) bool
	HostKey([]lifecycle.BlockEvidence, string, string) (trust.HostKey, error)
	PackageTree([]lifecycle.BlockEvidence, string) (PackageTree, error)
}

const unsupportedReason = "the selected Ceph release, image or cluster SSH transport is not the IBM Storage Ceph release this build realizes"
const unsupportedRemedy = "set ceph.release " + PinnedRelease + ", ceph.packageVersion " + PinnedPackageVersion + ", ceph.cephadm.ansible.packageVersion " + PinnedAnsibleVersion +
	", ceph.image.base " + PinnedRepository + ", ceph.image.version " + PinnedTag + " or the sha256 digest of its linux/amd64 manifest, and ceph.cephadm.clusterSSH.transport " + PinnedTransport +
	"; native transport is refused and host-key verification cannot be disabled"

func failure(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
func unsupportedFailure() error {
	return failure("lifecycle.unsupported", unsupportedReason, unsupportedRemedy)
}

func ContentDigest() string {
	d := sha256.Sum256([]byte(Implementation + "\x00" + Version + "\x00" + EvidenceVersion + "\x00" + PackageVersion + "\x00pinned-tunnel-image-v1"))
	return hex.EncodeToString(d[:])
}
