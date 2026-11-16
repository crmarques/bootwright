package ceph

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite Ceph testdata/*.golden")

// matchesGolden compares one compact JSON document with testdata/<name>.golden,
// which holds it indented for review. A terminated format ends in exactly one
// LF, which is stripped before the comparison; any other format ends in no
// whitespace at all. Indenting is lossless only for compact input, so the body
// must equal its json.Compact form before its indented golden can stand for
// the exact bytes.
func matchesGolden(t *testing.T, name string, data []byte, terminated bool) {
	t.Helper()
	body := data
	if terminated {
		trimmed, found := bytes.CutSuffix(data, []byte("\n"))
		if !found || bytes.HasSuffix(trimmed, []byte("\n")) {
			t.Fatalf("%s: a terminated format ends in exactly one LF: %q", name, data[max(0, len(data)-16):])
		}
		body = trimmed
	} else if len(bytes.TrimRight(data, " \t\r\n")) != len(data) {
		t.Fatalf("%s: an unterminated format ends in no whitespace: %q", name, data[max(0, len(data)-16):])
	}
	var compact, indented bytes.Buffer
	if err := json.Compact(&compact, body); err != nil {
		t.Fatalf("%s: the bytes are not one JSON document: %v", name, err)
	}
	if !bytes.Equal(compact.Bytes(), body) {
		t.Fatalf("%s: the bytes are not compact JSON, so an indented golden cannot pin them:\n%s", name, body)
	}
	if err := json.Indent(&indented, body, "", "  "); err != nil {
		t.Fatalf("%s: indenting: %v", name, err)
	}
	indented.WriteByte('\n')
	matchesTextGolden(t, name, indented.Bytes())
}

// matchesTextGolden compares bytes that are not one compact JSON document, such
// as JSON Lines, YAML or indented JSON, with testdata/<name>.golden byte for
// byte. git diff --check refuses a line ending in a space or tab and a blank
// line at the end of a file, so a golden holding either could never be
// committed.
func matchesTextGolden(t *testing.T, name string, data []byte) {
	t.Helper()
	text := string(data)
	if text == "\n" || strings.HasSuffix(text, "\n\n") {
		t.Fatalf("%s: the bytes end in a blank line, which git diff --check refuses", name)
	}
	for number, line := range strings.Split(text, "\n") {
		if strings.TrimRight(line, " \t") != line {
			t.Fatalf("%s: line %d ends in a space or tab, which git diff --check refuses", name, number+1)
		}
	}
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v; run with -update to write it", err)
	}
	if string(want) != text {
		t.Errorf("%s differs (-golden +got); rerun with -update if the change is intended:\n%s%s",
			path, lineDiff(string(want), text), firstDifference(string(want), text))
	}
}

// firstDifference quotes both texts around the first byte they differ at,
// which locates a change inside one long line such as an embedded document.
func firstDifference(want, got string) string {
	at := 0
	for at < len(want) && at < len(got) && want[at] == got[at] {
		at++
	}
	excerpt := func(text string) string { return text[max(0, at-40):min(len(text), at+40)] }
	return fmt.Sprintf("first difference at byte %d: golden %q, got %q", at, excerpt(want), excerpt(got))
}

// lineDiff lists the lines only the golden holds (-) and only the output holds
// (+), each numbered in its own text, along a longest common subsequence.
func lineDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	common := make([][]int, len(w)+1)
	for i := range common {
		common[i] = make([]int, len(g)+1)
	}
	for i := len(w) - 1; i >= 0; i-- {
		for j := len(g) - 1; j >= 0; j-- {
			if w[i] == g[j] {
				common[i][j] = common[i+1][j+1] + 1
			} else {
				common[i][j] = max(common[i+1][j], common[i][j+1])
			}
		}
	}
	var out strings.Builder
	for i, j := 0, 0; i < len(w) || j < len(g); {
		switch {
		case i < len(w) && j < len(g) && w[i] == g[j]:
			i, j = i+1, j+1
		case i < len(w) && (j == len(g) || common[i+1][j] >= common[i][j+1]):
			fmt.Fprintf(&out, "-%d: %s\n", i+1, w[i])
			i++
		default:
			fmt.Fprintf(&out, "+%d: %s\n", j+1, g[j])
			j++
		}
	}
	return out.String()
}

func frozenGoldenRequest() Request {
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
	return r
}

func TestFrozenRequestGolden(t *testing.T) {
	r := frozenGoldenRequest()
	for _, action := range []string{"cluster", "policy", "pool", "client", "rbd", "subvolume", "settings", "service"} {
		r.Action = action
		r.Settings = nil
		r.Service = nil
		r.Delegated = nil
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
			r.Client = &Client{Access: "readOnly", DataPools: []string{"archive", "ec-data"}, Name: "consumer", Pool: "volumes"}
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
		if action == "service" {
			r.Service = &ServiceUnit{Type: "nfs", ID: "files", Documents: []json.RawMessage{
				json.RawMessage(`{"placement":{"count":2,"hosts":["node-1","node-2"]},"service_id":"nfs.files","service_type":"ingress","spec":{"backend_service":"nfs.files","enable_haproxy_protocol":true,"frontend_port":2049,"monitor_port":9049,"virtual_ip":"192.0.2.50/24"}}`),
				json.RawMessage(`{"placement":{"count":2,"hosts":["node-1","node-2"]},"service_id":"files","service_type":"nfs","spec":{"enable_haproxy_protocol":true,"port":12049}}`)}}
			r.Delegated = []DelegatedService{{Name: "ingress.nfs.files", Hosts: []string{"node-1", "node-2"}}, {Name: "nfs.files", Hosts: []string{"node-1", "node-2"}}}
			r.Auxiliary = frozenAuxiliaryReferences()
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
	r.Action, r.Subvolume, r.Settings, r.Service, r.Auxiliary, r.Delegated = "client", nil, nil, nil, nil, nil
	for name, client := range map[string]*Client{
		"client-filesystem": {Access: "readOnly", DataPools: []string{}, Filesystem: "files", Name: "reader"},
		"client-subvolume":  {Access: "readWrite", DataPools: []string{}, Filesystem: "files", Group: "csi", Name: "writer", Subvolume: "docs"},
	} {
		r.Client = client
		data, err := r.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		matchesGolden(t, "request-"+name, data, false)
		decoded, err := DecodeRequest(data)
		if err != nil || !reflect.DeepEqual(decoded.Client, client) {
			t.Fatalf("frozen %s did not round-trip: %+v %v", name, decoded.Client, err)
		}
	}
	r.Action, r.Client = "cluster", nil
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

func TestFIPSSecurityGolden(t *testing.T) {
	r := frozenGoldenRequest()
	r.Security = &Security{FIPS: true, CephXKeyType: "aes"}
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "request-fips", data, false)
	if _, err := DecodeRequest(data); err != nil {
		t.Fatal(err)
	}
}

func TestFIPSHostSecurityGolden(t *testing.T) {
	r := packageRequest(t)
	r.Security = &Security{FIPS: true, CephXKeyType: "aes"}
	host := HostRequest{Cluster: scopeOf(r), Host: r.Hosts[0].Name, Pulls: []ImagePull{}, Version: HostVersion}
	data, err := host.canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "request-host-fips", data, false)
	if _, err := DecodeHostRequest(data); err != nil {
		t.Fatal(err)
	}
}
