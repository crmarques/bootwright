package ceph

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite Ceph testdata/*.golden")

func matchesGolden(t *testing.T, name string, data []byte, terminated bool) {
	t.Helper()
	matchesGoldenFile(t, filepath.Join("testdata", name+".golden"), data, terminated)
}

func matchesGoldenFile(t *testing.T, path string, data []byte, terminated bool) {
	t.Helper()
	body := data
	if terminated {
		if !bytes.HasSuffix(data, []byte("\n")) || bytes.HasSuffix(data, []byte("\n\n")) {
			t.Fatal("invalid JSON line ending")
		}
		body = data[:len(data)-1]
	}
	var compact bytes.Buffer
	if json.Compact(&compact, body) != nil || !bytes.Equal(compact.Bytes(), body) {
		t.Fatal("golden source is not compact JSON")
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, body, "", "  "); err != nil {
		t.Fatal(err)
	}
	pretty.WriteByte('\n')
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, pretty.Bytes(), 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want, pretty.Bytes()) {
		at := 0
		for at < len(want) && at < pretty.Len() && want[at] == pretty.Bytes()[at] {
			at++
		}
		t.Fatalf("%s differs at byte %d; rerun with -update and review the diff", path, at)
	}
}

func TestFrozenRequestGolden(t *testing.T) {
	r := runtimeRequest()
	r.Cephadm = Tool{Path: "/usr/sbin/cephadm", PackageVersion: "20.2.1-405.el9cp"}
	r.Release = "9.9.1.2"
	r.AnsiblePackageVersion = "5.0.3-1"
	r.NFSGaneshaPackageVersion = "synthetic-native-ganesha"
	r.PackageRepository = DefaultPackageRepository
	r.MonIP = "192.0.2.1"
	r.ClusterSSHUser = "operator"
	r.CallHome = "enabled"
	r.PublicNetworks = []string{"192.0.2.0/24"}
	r.ClusterNetworks = []string{"198.51.100.0/24"}
	r.Config = map[string]map[string]string{
		"osd":    {"osd_memory_target_autotune": "false", "osd_memory_target": "2147483648"},
		"global": {"osd_pool_default_size": "2", "osd_pool_default_min_size": "2"},
	}
	for i := range r.Hosts {
		r.Hosts[i].Labels = []string{"storage"}
		r.Hosts[i].Location = map[string]string{}
		r.Hosts[i].KnownHosts = ""
	}
	for _, action := range []string{"cluster", "policy", "pool", "client", "rbd", "subvolume", "settings"} {
		r.Action = action
		r.Settings = nil
		r.Auxiliary = nil
		r.Policy = nil
		r.Pool = nil
		r.Client = nil
		r.RBD = nil
		r.Subvolume = nil
		if action == "policy" {
			r.Policy = &Policy{DeviceClass: "ssd", FailureDomain: "host", Name: "ssd-hosts", Root: "default", Rule: "ssd-hosts", Size: 2}
		}
		if action == "pool" {
			r.Pool = &Pool{Name: "volumes", Type: "replicated", Role: "rbd", Application: "rbd", Size: 2, MinSize: 2, PGNum: 32, CrushRule: "replicated_rule", Autoscale: PoolAutoscale{Mode: "on"}}
		}
		if action == "client" {
			r.Client = &Client{Name: "consumer", Pool: "volumes", Access: "read-only"}
		}
		if action == "rbd" {
			r.RBD = &RBDImage{Name: "disk", Pool: "volumes", SizeBytes: 10737418240, Features: []string{"layering", "exclusive-lock", "object-map", "fast-diff", "deep-flatten"}, DataPool: ""}
		}
		if action == "subvolume" {
			r.Subvolume = &Subvolume{Filesystem: "files", Mode: "755", Name: "docs", Pool: "fs-data"}
		}
		if action == "settings" {
			r.Settings = &Settings{Modules: []string{"nfs"}}
			r.Auxiliary = []AuxiliaryReference{{Name: "haproxy", Image: r.Registry + "/synthetic/haproxy@sha256:" + strings.Repeat("a", 64)}, {Name: "keepalived", Image: r.Registry + "/synthetic/keepalived@sha256:" + strings.Repeat("b", 64)}}
		}
		data, err := r.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		matchesGolden(t, "request-"+action, data, false)
		decoded, err := DecodeRequest(data)
		if err != nil {
			t.Fatal(err)
		}
		roundtrip, err := decoded.Canonical()
		if err != nil || !bytes.Equal(data, roundtrip) {
			t.Fatal("frozen request did not round-trip")
		}
	}
	r.Action, r.Subvolume, r.Settings, r.Auxiliary = "cluster", nil, nil, nil
	r.Delegated = []DelegatedService{{Name: "ingress.rgw.objects", Hosts: []string{"node-1", "node-2"}}, {Name: "rgw.objects", Hosts: []string{"node-1", "node-2"}}}
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "request-cluster-delegated", data, false)
	decoded, err := DecodeRequest(data)
	if err != nil || !reflect.DeepEqual(decoded.Delegated, r.Delegated) {
		t.Fatalf("frozen delegated services did not round-trip: %+v %v", decoded.Delegated, err)
	}
}

func TestEvidenceGolden(t *testing.T) {
	r := runtimeRequest()
	e := readyEvidence(r)
	data := evidenceBytes(t, e)
	matchesGolden(t, "evidence-ready", data, false)
	if _, err := EvaluateEvidence(data, r, "digest", false); err != nil {
		t.Fatal(err)
	}
	digest, _ := json.Marshal(map[string]string{"digest": ContentDigest()})
	matchesGolden(t, "content-digest", digest, false)
}
