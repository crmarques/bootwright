//go:build linux && amd64

package main

import (
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/storage/ceph"
)

// cephMarkerSources maps each Ceph plugin source that names a marker record,
// a file a Markers store keeps beneath a Ceph host root, to the
// implementations whose workers read or write it. The shared worker sources
// name every Ceph implementation whose worker derives from them.
func cephMarkerSources() map[string][]string {
	core, host := ceph.Implementation, ceph.HostImplementation
	export, smb, replication := ceph.ConsumerImplementation, ceph.SMBImplementation, ceph.ReplicationImplementation
	return map[string][]string{
		"ceph_native.py":               {core, export, smb, replication},
		"ceph_native_resources.py":     {core, export, smb, replication},
		"ceph_native_nfs.py":           {core, smb},
		"ceph_native_nvmeof.py":        {core},
		"ceph_native_object.py":        {core},
		"ceph_native_observability.py": {core},
		"ceph_native_stretch.py":       {core},
		"ceph_native_units.py":         {core},
		"ceph_tunnel_custody.py":       {core},
		"ceph_host_runtime.py":         {host, core},
		"ceph_packages.py":             {host},
		"ceph_export_native.py":        {export},
		"ceph_smb_native.py":           {smb},
		"ceph_replication_native.py":   {replication},
	}
}

// cephMarkerNames is every marker record name, or literal fragment of one, the
// Ceph sources carry: TestEveryCephMarkerNameIsDeclared holds it to them, and
// each is a proof identifier of the implementations its sources name.
func cephMarkerNames() []string {
	return []string{
		"-attempt", "-attempt-%d.json", "-attempt.json", "-complete.json", "-facts.json", "-id", "-id.json",
		"-inputs.json", "-pulled.json", "-ready.json", "-reserved.json", "-rgw.json", ".json", "artifact.json",
		"attempt-call_home_initialize.json", "auth.json", "auxiliary-", "bootstrap-attempt.json",
		"bootstrap-enrollment.json", "bootstrap-original-ready.json", "bootstrap-original-reserved.json",
		"bootstrap-output-source.json", "bootstrap-prepared.json", "bootstrap-protocol.json", "bootstrap-scope.json",
		"cache-directory-intent.json", "cache-directory.json", "cache-pending.json", "cache-ready.json",
		"call-home-accepted.json", "client-", "client-applied-", "client-attempt-", "client-generator.json",
		"complete-call_home_initialize.json", "complete.json", "dashboard-port-", "dashboard-port-repair.json",
		"dashboard-redeploy.json", "dashboard-redeployed.json", "decision.json", "device-identities.json",
		"filesystem-", "filesystem-attempt.", "filesystem-id.", "gateway-", "group-attempt.", "group.", "image-",
		"initial-mgr-original-ready.json", "manifest.json", "monitoring-log-config-attempt.json",
		"monitoring-log-config-complete.json", "native-teardown.json", "nfs-export-attempt.", "nfs-export-id.",
		"nfs-pool-id.json", "nfs-pool.json", "nvmeof-attempt.", "nvmeof-complete.", "object-bucket-", "object-user-",
		"osd-device-", "osd-device-intent.json", "owner.json", "physical-device-binding.json", "podman-auth.json",
		"policy-", "policy-attempt-", "policy-id-", "policy.json", "pool-", "pool-attempt-", "pool-id-",
		"pool-rbd-attempt-", "pool-rbd-complete-", "pool-setting-", "pull-attempt-%d.json", "pulled.json", "rbd-",
		"rbd-attempt.", "rbd-id.", "registry-file.json", "registry-intent.json", "registry-pending.json",
		"registry-removal.json", "registry-removed.json", "replication-", "rgw-root.json", "route.json",
		"runtime-profile.json", "scope.json", "service-", "service-inputs.json", "smb-applied.", "smb-attempt.",
		"ssh-authorization.json", "sshd-policy.json", "stretch-", "subvolume-", "subvolume-attempt.", "subvolume-path.",
		"synthetic.json", "tiebreaker-removal-attempt.json", "tiebreaker-removal-complete.json", "transport-directory.json", "transport-file-ready-", "transport-file-relabeled-",
		"transport-file-reserved-", "transport-ready.json", "transport-relabel.json", "transport.json",
		"tunnel-config-attempt-", "tunnel-config-attempt-call_home.json", "tunnel-config-complete-",
		"tunnel-config-complete-call_home.json", "tunnel-config-intent.json", "tunnel-configured.json",
		"tunnel-manager-files.json", "tunnel-mgr-source.json", "tunnel-mounted.json", "tunnel-redeploy.json",
		"tunnel-stage.json", "verified.json",
	}
}

const cephMarkerProof = "marker:"

var (
	cephMarkerCall = regexp.MustCompile(`markers\.(?:read|write)\(`)
	// cephMarkerJSON is a quoted record name; cephMarkerOperand is a quoted
	// operand of a concatenation that builds one.
	cephMarkerJSON    = regexp.MustCompile(`"([^"\\\n]*\.json)"`)
	cephMarkerOperand = regexp.MustCompile(`"([^"\\\n]*)"\s*\+|\+\s*"([^"\\\n]*)"`)
)

// cephMarkerLiterals is every quoted record name in a source that keeps
// Markers records, and every quoted operand of a concatenation on a line that
// carries a record name or after a markers.read or markers.write call, with
// the sources that carry it, and the sources that keep Markers records.
func cephMarkerLiterals() (map[string][]string, []string) {
	found, sources := map[string][]string{}, []string(nil)
	assets := ansible.Automation()
	for _, name := range slices.Sorted(maps.Keys(assets)) {
		if !strings.HasPrefix(name, automationCollection+"plugins/") || path.Ext(name) != ".py" {
			continue
		}
		text := string(assets[name])
		if !cephMarkerCall.MatchString(text) && !strings.Contains(text, "class Markers") {
			continue
		}
		source := path.Base(name)
		sources = append(sources, source)
		add := func(literal string) {
			if strings.ContainsFunc(literal, func(r rune) bool { return 'a' <= r && r <= 'z' }) && !slices.Contains(found[literal], source) {
				found[literal] = append(found[literal], source)
			}
		}
		for line := range strings.SplitSeq(text, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			for _, match := range cephMarkerJSON.FindAllStringSubmatch(line, -1) {
				add(match[1])
			}
			builds := cephMarkerJSON.MatchString(line)
			start := len(line)
			if call := cephMarkerCall.FindStringIndex(line); call != nil {
				start = call[1]
			}
			if builds {
				start = 0
			}
			for _, match := range cephMarkerOperand.FindAllStringSubmatch(line[start:], -1) {
				add(match[1] + match[2])
			}
		}
	}
	return found, sources
}

// cephMarkerProofs is each Ceph implementation's marker proof identifiers.
func cephMarkerProofs() map[string][]string {
	proofs, sources := map[string][]string{}, cephMarkerSources()
	found, _ := cephMarkerLiterals()
	for _, literal := range cephMarkerNames() {
		named := map[string]bool{}
		for _, source := range found[literal] {
			for _, implementation := range sources[source] {
				named[implementation] = true
			}
		}
		for implementation := range named {
			proofs[implementation] = append(proofs[implementation], cephMarkerProof+literal)
		}
	}
	return proofs
}

// TestEveryCephMarkerNameIsDeclared holds the declared Ceph marker record
// names to the literals the sources that keep Markers records carry, and each
// such source to the implementations it names, so a renamed, added or removed
// marker changes the proof identifiers of the contract goldens that list it.
// A name built only from values, with no quoted record name or operand, is
// outside what it sees.
func TestEveryCephMarkerNameIsDeclared(t *testing.T) {
	found, sources := cephMarkerLiterals()
	known := cephMarkerSources()
	for _, source := range sources {
		if _, ok := known[source]; !ok {
			t.Errorf("%s keeps Markers records but cephMarkerSources names no implementation for it", source)
		}
	}
	for source := range known {
		if !slices.Contains(sources, source) {
			t.Errorf("cephMarkerSources names %s, which keeps no Markers record: remove it", source)
		}
	}
	if !slices.Contains(slices.Collect(maps.Keys(found)), "owner.json") {
		t.Error(`the scan no longer finds "owner.json": the scan is broken`)
	}
	declared := cephMarkerNames()
	for _, literal := range slices.Sorted(maps.Keys(found)) {
		if !slices.Contains(declared, literal) {
			t.Errorf("%q in %s is a Ceph marker record name the contract does not declare: add it to cephMarkerNames", literal, strings.Join(found[literal], ", "))
		}
	}
	for _, literal := range declared {
		if _, ok := found[literal]; !ok {
			t.Errorf("%q is declared but no Ceph source carries it: remove it from cephMarkerNames", literal)
		}
	}
	if !slices.IsSorted(declared) || len(slices.Compact(slices.Clone(declared))) != len(declared) {
		t.Error("cephMarkerNames is not sorted and unique")
	}
}
