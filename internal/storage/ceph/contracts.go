package ceph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/storage"
	"github.com/crmarques/bootwright/internal/trust"
)

type Runner interface {
	Run(context.Context, lifecycle.RunRequest) (lifecycle.RunResult, error)
}

const (
	PinnedTransport = "verified-tunnel"
	PinnedPlatform  = "linux/amd64"
)

func ValidImageDigest(value string) bool {
	digits, found := strings.CutPrefix(value, "sha256:")
	if !found || len(digits) != 64 {
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
	return storage.NativeVersion(release) && storage.NativeVersion(packageVersion) && storage.NativeVersion(ansibleVersion) && base != "" && storage.NativeVersion(version) && transport == PinnedTransport
}

type DependencyProofs interface {
	DataDisk(reconciliation.Block, []byte, string, string, int64) bool
	HostKey([]lifecycle.BlockEvidence, string, string) (trust.HostKey, error)
	PackageTree([]lifecycle.BlockEvidence, string) (PackageTree, error)
}

const unsupportedReason = "the selected Ceph coordinates or cluster SSH transport are incomplete or unsafe"
const unsupportedRemedy = "declare bounded version strings, an image repository and verified-tunnel transport; native transport is refused and host-key verification cannot be disabled"

func failure(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
func unsupportedFailure() error {
	return failure("lifecycle.unsupported", unsupportedReason, unsupportedRemedy)
}

func ContentDigest() string {
	d := sha256.Sum256([]byte(Implementation + "\x00" + Version + "\x00" + EvidenceVersion + "\x00" + PackageVersion + "\x00selected-tunnel-image-v2"))
	return hex.EncodeToString(d[:])
}
