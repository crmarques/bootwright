package ceph

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

func readyEvidence(r Request) Evidence {
	e := Evidence{Version: EvidenceVersion, Request: "digest", Action: r.Action, Owner: r.Owner, FSID: r.FSID, Owned: true, Exists: true, Ready: true, Health: "HEALTH_OK", Hosts: hostNames(r), Monitors: hostNames(r), MGRActive: "node-1", MGRStandbys: []string{"node-2"}, MonAddrs: []MonitorAddress{}, OSDs: []OSDEvidence{}, Daemons: []UnitDaemon{}}
	for i, h := range r.Hosts {
		e.OSDs = append(e.OSDs, OSDEvidence{Host: h.Name, Path: "/dev/vdb", ID: i, UUID: "osd-" + h.Name})
		if r.Action == "cluster" {
			e.MonAddrs = append(e.MonAddrs, MonitorAddress{Host: h.Name, Address: "192.0.2." + strconv.Itoa(i+1)})
		}
	}
	if r.Action == "cluster" {
		e.Dashboard = DashboardEvidence{URL: "https://node-1:8443/", CertificateSHA256: strings.Repeat("c", 64), Reachable: true}
	}
	return e
}
func evidenceBytes(t *testing.T, e Evidence) []byte {
	t.Helper()
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func runtimeRequest() Request {
	r := coreRequest()
	r.Owner = "apply-original"
	r.FSID = FSIDFor(r.Owner, r.Cluster)
	return r
}

func TestEvidenceRequiresExactOwnedReadyServiceAndDiskScope(t *testing.T) {
	r := runtimeRequest()
	e := readyEvidence(r)
	effect, err := EvaluateEvidence(evidenceBytes(t, e), r, "digest", false)
	if err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("valid count-two among three rejected: %s %v", effect, err)
	}
	tolerated := readyEvidence(r)
	tolerated.Health = "HEALTH_ERR"
	if effect, err := EvaluateEvidence(evidenceBytes(t, tolerated), r, "digest", false); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("a ready cluster whose only checks the worker tolerates was rejected: %s %v", effect, err)
	}
	cases := map[string]func(*Evidence){"owner": func(e *Evidence) { e.Owner = "other" }, "fsid": func(e *Evidence) { e.FSID = "other" }, "request": func(e *Evidence) { e.Request = "other" }, "foreign": func(e *Evidence) { e.Owned = false }, "host": func(e *Evidence) { e.Hosts[0] = "foreign" }, "duplicate": func(e *Evidence) { e.Hosts[0] = e.Hosts[1] }, "mgr": func(e *Evidence) { e.MGRActive = "foreign" }, "mgr-count": func(e *Evidence) { e.MGRStandbys = []string{} }, "root-device": func(e *Evidence) { e.OSDs[0].Path = "/dev/vda" }, "same-osd": func(e *Evidence) { e.OSDs[1].ID = e.OSDs[0].ID }, "partial-osd": func(e *Evidence) { e.OSDs = e.OSDs[:2] }, "contradiction": func(e *Evidence) { e.Removed = true }, "health": func(e *Evidence) { e.Health = "" }}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			e := readyEvidence(r)
			mutate(&e)
			effect, err := EvaluateEvidence(evidenceBytes(t, e), r, "digest", false)
			if err == nil || effect != reconciliation.EffectUnknown {
				t.Fatalf("unsafe evidence accepted: %s %v", effect, err)
			}
		})
	}
}

func TestDaemonAbsenceNeverProvesClusterCleanup(t *testing.T) {
	r := runtimeRequest()
	e := Evidence{Version: EvidenceVersion, Request: "digest", Action: r.Action, Owner: r.Owner, FSID: r.FSID, Owned: true, Removed: true, Hosts: hostNames(r), Monitors: []string{}, MGRStandbys: []string{}, MonAddrs: []MonitorAddress{}, OSDs: []OSDEvidence{}, Daemons: []UnitDaemon{}}
	effect, err := EvaluateEvidence(evidenceBytes(t, e), r, "digest", true)
	if err == nil || effect != reconciliation.EffectUnknown {
		t.Fatalf("daemon absence claimed full disk/key cleanup: %s %v", effect, err)
	}
	for _, hosts := range [][]string{nil, {}, hostNames(r)[:2]} {
		e.Hosts = hosts
		if effect, err := EvaluateEvidence(evidenceBytes(t, e), r, "digest", true); err == nil || effect != reconciliation.EffectUnknown {
			t.Fatalf("missing host treated as removed: %s %v", effect, err)
		}
	}
	e.Hosts = hostNames(r)
	e.Owned = false
	e.Removed = false
	if _, err := EvaluateEvidence(evidenceBytes(t, e), r, "digest", true); err == nil {
		t.Fatal("unowned absence proves removal")
	}
}

func TestEvidenceRejectsMissingUnknownNullAndDuplicateFields(t *testing.T) {
	r := runtimeRequest()
	data := evidenceBytes(t, readyEvidence(r))
	if _, err := EvaluateEvidence(data, r, "digest", false); err != nil {
		t.Fatalf("canonical evidence refused: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "partial")
	missing, _ := json.Marshal(fields)
	fields["partial"] = json.RawMessage(`null`)
	null, _ := json.Marshal(fields)
	fields["partial"] = json.RawMessage(`false`)
	fields["key"] = json.RawMessage(`"secret"`)
	extra, _ := json.Marshal(fields)
	delete(fields, "key")
	delete(fields, "endpoint")
	missingEndpoint, _ := json.Marshal(fields)
	fields["endpoint"] = json.RawMessage(`null`)
	nullEndpoint, _ := json.Marshal(fields)
	fields["endpoint"] = json.RawMessage(`{"certificateSHA256":"","reachable":false,"url":""}`)
	extraEndpoint, _ := json.Marshal(fields)
	duplicate := append([]byte(`{"owner":"foreign",`), data[1:]...)
	for _, bad := range [][]byte{missing, null, extra, duplicate, missingEndpoint, nullEndpoint, extraEndpoint} {
		if _, err := EvaluateEvidence(bad, r, "digest", false); err == nil {
			t.Fatal("noncanonical evidence accepted")
		}
	}
}

func TestPoolAndClientRequireHealthyCompleteCore(t *testing.T) {
	for _, action := range []string{"pool", "client"} {
		t.Run(action, func(t *testing.T) {
			r := runtimeRequest()
			r.Action = action
			r.Pool = &Pool{Name: "volumes"}
			r.Client = &Client{Name: "consumer", Pool: "volumes", Access: "read-only"}
			e := readyEvidence(r)
			if action == "pool" {
				e.Resource = ResourceEvidence{ID: "1"}
			} else {
				e.Client = "client.consumer"
			}
			if _, err := EvaluateEvidence(evidenceBytes(t, e), r, "digest", false); err != nil {
				t.Fatal(err)
			}
			for _, change := range []func(*Evidence){func(e *Evidence) { e.Health = "" }, func(e *Evidence) { e.Monitors = []string{} }, func(e *Evidence) { e.OSDs = []OSDEvidence{} }, func(e *Evidence) { e.MGRActive = "" }} {
				bad := e
				change(&bad)
				if _, err := EvaluateEvidence(evidenceBytes(t, bad), r, "digest", false); err == nil {
					t.Fatal("resource readiness concealed incomplete core")
				}
			}
		})
	}
}

func TestAbsentNativeResourceWithOwnedResidueRemainsPartial(t *testing.T) {
	for _, action := range []string{"pool", "client"} {
		t.Run(action, func(t *testing.T) {
			r := runtimeRequest()
			r.Action = action
			if action == "pool" {
				r.Pool = &Pool{Name: "volumes"}
			} else {
				r.Client = &Client{Name: "consumer", Pool: "volumes", Access: "read-only"}
			}
			e := readyEvidence(r)
			e.Ready = false
			e.Partial = true
			e.Client = ""
			for _, removal := range []bool{false, true} {
				effect, err := EvaluateEvidence(evidenceBytes(t, e), r, "digest", removal)
				if err != nil || effect != reconciliation.EffectPartial {
					t.Fatalf("owned residue treated as finished (removal=%t): %s %v", removal, effect, err)
				}
			}
		})
	}
}

func TestEvidenceV2BoundsTheDashboardAndMonitorAddresses(t *testing.T) {
	r := runtimeRequest()
	if effect, err := EvaluateEvidence(evidenceBytes(t, readyEvidence(r)), r, "digest", false); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("ready v2 evidence rejected: %v %v", effect, err)
	}
	unreachable := readyEvidence(r)
	unreachable.Dashboard.Reachable = false
	if effect, err := EvaluateEvidence(evidenceBytes(t, unreachable), r, "digest", false); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("an unreachable dashboard must stay a completed cluster: %v %v", effect, err)
	}
	for name, change := range map[string]func(*Evidence){
		"v1":                       func(e *Evidence) { e.Version = "ceph-core-evidence-v1" },
		"plain http":               func(e *Evidence) { e.Dashboard.URL = "http://node-1:8443/" },
		"another port":             func(e *Evidence) { e.Dashboard.URL = "https://node-1:9443/" },
		"credential in url":        func(e *Evidence) { e.Dashboard.URL = "https://admin:secret@node-1:8443/" },
		"uppercase fingerprint":    func(e *Evidence) { e.Dashboard.CertificateSHA256 = strings.Repeat("C", 64) },
		"short fingerprint":        func(e *Evidence) { e.Dashboard.CertificateSHA256 = "cc" },
		"reachable without url":    func(e *Evidence) { e.Dashboard.URL = "" },
		"reachable without digest": func(e *Evidence) { e.Dashboard.CertificateSHA256 = "" },
		"foreign monitor host":     func(e *Evidence) { e.MonAddrs[0].Host = "foreign" },
		"duplicate monitor host":   func(e *Evidence) { e.MonAddrs[1].Host = e.MonAddrs[0].Host },
		"monitor name for address": func(e *Evidence) { e.MonAddrs[0].Address = "node-1" },
		"noncanonical address":     func(e *Evidence) { e.MonAddrs[0].Address = "2001:DB8::1" },
	} {
		e := readyEvidence(r)
		change(&e)
		if _, err := EvaluateEvidence(evidenceBytes(t, e), r, "digest", false); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	data := evidenceBytes(t, readyEvidence(r))
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]string{
		"missing reachability": `{"certificateSHA256":"","url":""}`, "password member": `{"certificateSHA256":"","password":"x","reachable":false,"url":""}`,
		"null url": `{"certificateSHA256":"","reachable":false,"url":null}`,
	} {
		fields["dashboard"] = json.RawMessage(raw)
		bad, _ := json.Marshal(fields)
		if _, err := EvaluateEvidence(bad, r, "digest", false); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	pool := runtimeRequest()
	pool.Action = "pool"
	pool.Pool = &Pool{Name: "volumes"}
	e := readyEvidence(pool)
	e.Resource = ResourceEvidence{ID: "1"}
	if _, err := EvaluateEvidence(evidenceBytes(t, e), pool, "digest", false); err != nil {
		t.Fatal(err)
	}
	e.Dashboard = DashboardEvidence{URL: "https://node-1:8443/"}
	if _, err := EvaluateEvidence(evidenceBytes(t, e), pool, "digest", false); err == nil {
		t.Fatal("a pool block carried dashboard evidence")
	}
}

func resourceRequest(action string) Request {
	r := runtimeRequest()
	r.Action = action
	switch action {
	case "pool":
		r.Pool = &Pool{Name: "volumes"}
	case "client":
		r.Client = &Client{Name: "consumer", Pool: "volumes", Access: "read-only"}
	}
	return r
}

func TestEvidenceResourceMembersClosed(t *testing.T) {
	r := runtimeRequest()
	data := evidenceBytes(t, readyEvidence(r))
	if effect, err := EvaluateEvidence(data, r, "digest", false); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("ready evidence rejected: %s %v", effect, err)
	}
	for name, change := range map[string]func(map[string]json.RawMessage){
		"extra member":       func(f map[string]json.RawMessage) { f["resource"] = json.RawMessage(`{"id":"","key":"","path":""}`) },
		"null":               func(f map[string]json.RawMessage) { f["resource"] = json.RawMessage(`null`) },
		"missing path":       func(f map[string]json.RawMessage) { f["resource"] = json.RawMessage(`{"id":""}`) },
		"numeric id":         func(f map[string]json.RawMessage) { f["resource"] = json.RawMessage(`{"id":1,"path":""}`) },
		"poolID beside":      func(f map[string]json.RawMessage) { f["poolID"] = json.RawMessage(`-1`) },
		"poolID in place of": func(f map[string]json.RawMessage) { delete(f, "resource"); f["poolID"] = json.RawMessage(`-1`) },
	} {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatal(err)
		}
		change(fields)
		bad, _ := json.Marshal(fields)
		if effect, err := EvaluateEvidence(bad, r, "digest", false); err == nil || effect != reconciliation.EffectUnknown {
			t.Fatalf("%s accepted: %s %v", name, effect, err)
		}
	}
}

func TestPoolEvidenceResourceIDIsDecimal(t *testing.T) {
	pool := resourceRequest("pool")
	for _, id := range []string{"0", "8"} {
		e := readyEvidence(pool)
		e.Resource = ResourceEvidence{ID: id}
		if effect, err := EvaluateEvidence(evidenceBytes(t, e), pool, "digest", false); err != nil || effect != reconciliation.EffectCompleted {
			t.Fatalf("pool id %q rejected: %s %v", id, effect, err)
		}
	}
	for _, id := range []string{"", "-1", "01", "1.0", " 1", "x", strings.Repeat("1", 20)} {
		e := readyEvidence(pool)
		e.Resource = ResourceEvidence{ID: id}
		if effect, err := EvaluateEvidence(evidenceBytes(t, e), pool, "digest", false); err == nil || effect != reconciliation.EffectUnknown {
			t.Fatalf("pool id %q accepted: %s %v", id, effect, err)
		}
		partial := readyEvidence(pool)
		partial.Ready, partial.Partial = false, true
		partial.Resource = ResourceEvidence{ID: id}
		effect, err := EvaluateEvidence(evidenceBytes(t, partial), pool, "digest", false)
		if id == "" && (err != nil || effect != reconciliation.EffectPartial) || id != "" && (err == nil || effect != reconciliation.EffectUnknown) {
			t.Fatalf("partial pool id %q: %s %v", id, effect, err)
		}
	}
	partial := readyEvidence(pool)
	partial.Ready, partial.Partial = false, true
	partial.Resource = ResourceEvidence{ID: "8"}
	if effect, err := EvaluateEvidence(evidenceBytes(t, partial), pool, "digest", false); err != nil || effect != reconciliation.EffectPartial {
		t.Fatalf("partial pool id 8 rejected: %s %v", effect, err)
	}
	for _, action := range []string{"cluster", "pool", "client"} {
		r := resourceRequest(action)
		e := readyEvidence(r)
		if action == "client" {
			e.Client = "client.consumer"
		}
		e.Resource = ResourceEvidence{ID: "1", Path: "/volumes"}
		if action != "pool" {
			e.Resource.Path = ""
			if effect, err := EvaluateEvidence(evidenceBytes(t, e), r, "digest", false); err == nil || effect != reconciliation.EffectUnknown {
				t.Fatalf("%s accepted a resource id: %s %v", action, effect, err)
			}
			e.Resource = ResourceEvidence{Path: "/volumes"}
		}
		if effect, err := EvaluateEvidence(evidenceBytes(t, e), r, "digest", false); err == nil || effect != reconciliation.EffectUnknown {
			t.Fatalf("%s accepted a resource path: %s %v", action, effect, err)
		}
	}
	absent := readyEvidence(pool)
	absent.Exists, absent.Ready = false, false
	if effect, err := EvaluateEvidence(evidenceBytes(t, absent), pool, "digest", false); err != nil || effect != reconciliation.EffectNoEffect {
		t.Fatalf("an absent pool was not a no-effect observation: %s %v", effect, err)
	}
	absent.Resource = ResourceEvidence{ID: "1"}
	if effect, err := EvaluateEvidence(evidenceBytes(t, absent), pool, "digest", false); err == nil || effect != reconciliation.EffectUnknown {
		t.Fatalf("an absent pool carried a resource identity: %s %v", effect, err)
	}
}

func TestEvidencePolicyReadyNeedsRuleID(t *testing.T) {
	policy := runtimeRequest()
	policy.Action = "policy"
	policy.Policy = &Policy{DeviceClass: "ssd", FailureDomain: "host", Name: "fast", Root: "default", Rule: "fast-hosts", Size: 2}
	for id, want := range map[string]reconciliation.EffectState{"": reconciliation.EffectUnknown, "x1": reconciliation.EffectUnknown, "-1": reconciliation.EffectUnknown, "0": reconciliation.EffectCompleted, "7": reconciliation.EffectCompleted} {
		e := readyEvidence(policy)
		e.Resource = ResourceEvidence{ID: id}
		if effect, _ := EvaluateEvidence(evidenceBytes(t, e), policy, "digest", false); effect != want {
			t.Fatalf("ready policy evidence with rule id %q is %s, want %s", id, effect, want)
		}
	}
}

func TestEvidencePolicyPartialNeedsDecimalRuleID(t *testing.T) {
	policy := runtimeRequest()
	policy.Action = "policy"
	policy.Policy = &Policy{DeviceClass: "ssd", FailureDomain: "host", Name: "fast", Root: "default", Rule: "fast-hosts", Size: 2}
	for id, want := range map[string]reconciliation.EffectState{"x1": reconciliation.EffectUnknown, "-1": reconciliation.EffectUnknown, "8": reconciliation.EffectPartial} {
		partial := readyEvidence(policy)
		partial.Ready, partial.Partial = false, true
		partial.Resource = ResourceEvidence{ID: id}
		effect, err := EvaluateEvidence(evidenceBytes(t, partial), policy, "digest", false)
		if effect != want || (want == reconciliation.EffectUnknown) != (err != nil) {
			t.Fatalf("partial policy evidence with rule id %q is %s (%v), want %s", id, effect, err, want)
		}
	}
}
