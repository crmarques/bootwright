package ceph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
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

var AuxiliaryNames = []string{"alertmanager", "alloy", "grafana", "haproxy", "keepalived", "loki", "nginx", "node_exporter", "nvmeof",
	"oauth2_proxy", "prometheus", "promtail", "samba", "samba_metrics", "snmp_gateway"}

var auxiliaryFamilies = map[string][]string{
	"alertmanager": {"alertmanager"}, "alloy": {"alloy"}, "grafana": {"grafana"}, "ingress": {"haproxy", "keepalived"}, "loki": {"loki"},
	"mgmt-gateway": {"nginx"}, "node-exporter": {"node_exporter"}, "nvmeof": {"nvmeof"}, "oauth2-proxy": {"oauth2_proxy"},
	"prometheus": {"prometheus"}, "promtail": {"promtail"}, "smb": {"samba", "samba_metrics"}, "snmp-gateway": {"snmp_gateway"},
}

func auxiliaryNeeds(service api.Value) []string {
	t := service.Get("service_type").Text()
	if t == "ingress" && service.Get("spec", "keepalive_only").Bool() {
		return []string{"keepalived"}
	}
	return slices.Clone(auxiliaryFamilies[t])
}

var ZonePoolSuffixes = []string{"meta", "control", "dedup", "log", "otp", "buckets.index", "buckets.non-ec", "buckets.data"}

var zonePlacementSuffixes = map[string]string{"indexPoolRef": "buckets.index", "dataExtraPoolRef": "buckets.non-ec", "dataPoolRef": "buckets.data"}

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
