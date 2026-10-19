package ceph

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const inspectedFSID = "6b8f1c2e-4d7a-5e3f-9a1b-0c2d4e6f8a1b"

var inspectedDigest = "sha256:" + strings.Repeat("d", 64)

var inspectedCertificate = strings.Repeat("c", 64)

func inspectedMachine(name, address string) api.Object {
	addresses := api.ListValue(api.MapValue(api.FieldValue{Name: "name", Value: api.StringValue("mgmt")}, api.FieldValue{Name: "address", Value: api.StringValue(address)}))
	spec := api.MapValue(
		api.FieldValue{Name: "access", Value: api.MapValue(api.FieldValue{Name: "ssh", Value: api.MapValue(api.FieldValue{Name: "addressRef", Value: api.StringValue("mgmt")})})},
		api.FieldValue{Name: "network", Value: api.MapValue(api.FieldValue{Name: "addresses", Value: addresses})},
	)
	return api.NewObject(api.Machine, name, api.Value{}, spec)
}

func inspectedCluster() (api.Catalog, api.Object) {
	node := func(name, fqdn, machine string) api.Value {
		fields := []api.FieldValue{{Name: "name", Value: api.StringValue(name)}, {Name: "machineRef", Value: api.StringValue(machine)}}
		if fqdn != "" {
			fields = append(fields, api.FieldValue{Name: "fqdn", Value: api.StringValue(fqdn)})
		}
		return api.MapValue(fields...)
	}
	nodes := api.ListValue(node("rhel-02", "rhel-02.lab.test", "m2"), node("rhel-01", "rhel-01.lab.test", "m1"), node("rhel-03", "", "m3"))
	cluster := api.NewObject(api.StorageCluster, "storage", api.Value{}, api.MapValue(
		api.FieldValue{Name: "ceph", Value: api.MapValue(api.FieldValue{Name: "topology", Value: api.MapValue(api.FieldValue{Name: "nodes", Value: nodes})})}))
	return api.NewCatalog([]api.Object{cluster, inspectedMachine("m1", "192.0.2.11"), inspectedMachine("m2", "192.0.2.12"), inspectedMachine("m3", "192.0.2.13")}), cluster
}

func inspectedClusterEvidence(t *testing.T, edit func(*Evidence)) json.RawMessage {
	t.Helper()
	e := Evidence{
		Version: EvidenceVersion, Request: "digest", Action: "cluster", Owner: "apply", FSID: inspectedFSID, Owned: true, Exists: true, Ready: true,
		Health: "HEALTH_OK", Hosts: []string{"rhel-01.lab.test"}, Monitors: []string{"rhel-03", "rhel-01.lab.test", "rhel-02.lab.test"},
		MGRActive: "rhel-01.lab.test", MGRStandbys: []string{"rhel-03"}, OSDs: []OSDEvidence{}, Daemons: []UnitDaemon{},
		MonAddrs:  []MonitorAddress{{Host: "rhel-02.lab.test", Address: "198.51.100.12"}},
		Dashboard: DashboardEvidence{URL: "https://rhel-01.lab.test:8443/", CertificateSHA256: inspectedCertificate, Reachable: true},
	}
	if edit != nil {
		edit(&e)
	}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func inspectedImageEvidence(t *testing.T, edit func(*ImageEvidence)) json.RawMessage {
	t.Helper()
	e := ImageEvidence{Postcondition: true, Release: PinnedRelease, Base: PinnedRepository, Tag: PinnedTag, Digest: inspectedDigest, Platform: PinnedPlatform, Request: "digest"}
	if edit != nil {
		edit(&e)
	}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func inspectedProof(implementation string, verb reconciliation.Verb, state reconciliation.BlockState, evidence json.RawMessage) lifecycle.BlockEvidence {
	return lifecycle.BlockEvidence{Kind: string(api.StorageCluster), Object: "storage", Implementation: implementation, Verb: verb, State: state, Evidence: evidence}
}

func provedPair(t *testing.T) []lifecycle.BlockEvidence {
	return []lifecycle.BlockEvidence{
		inspectedProof(ImageImplementation, reconciliation.Apply, reconciliation.BlockDone, inspectedImageEvidence(t, nil)),
		inspectedProof(Implementation, reconciliation.Apply, reconciliation.BlockDone, inspectedClusterEvidence(t, nil)),
	}
}

func TestInspectReadsFSIDImageAndMonitorsFromTheCurrentOperationsEvidence(t *testing.T) {
	catalog, cluster := inspectedCluster()
	got, reason := Inspect(catalog, cluster, provedPair(t))
	want := Inspection{
		FSID:  inspectedFSID,
		Image: ImageInspection{Reference: PinnedRepository + ":" + PinnedTag, Digest: inspectedDigest, Platform: PinnedPlatform},
		Monitors: []MonitorInspection{
			{Host: "rhel-01", FQDN: "rhel-01.lab.test", Address: "192.0.2.11"},
			{Host: "rhel-02", FQDN: "rhel-02.lab.test", Address: "198.51.100.12"},
			{Host: "rhel-03", FQDN: "rhel-03", Address: "192.0.2.13"},
		},
		Dashboard: DashboardInspection{Status: DashboardReady, User: "admin", CertificateSHA256: inspectedCertificate, URLs: []DashboardURL{
			{Host: "rhel-01", URL: "https://rhel-01.lab.test:8443/", Role: DashboardActive},
			{Host: "rhel-03", URL: "https://rhel-03:8443/", Role: DashboardStandby},
		}},
	}
	if reason != "" || !reflect.DeepEqual(got, want) {
		t.Fatalf("inspection = %+v (%q), want %+v", got, reason, want)
	}
}

func TestInspectIgnoresTheSettingsBlockEvidence(t *testing.T) {
	catalog, cluster := inspectedCluster()
	want, reason := Inspect(catalog, cluster, provedPair(t))
	if reason != "" {
		t.Fatalf("the proved pair inspects as %q", reason)
	}
	settings := inspectedClusterEvidence(t, func(e *Evidence) {
		e.Action, e.MonAddrs, e.Dashboard = "settings", []MonitorAddress{}, DashboardEvidence{}
	})
	got, reason := Inspect(catalog, cluster, append(provedPair(t), inspectedProof(Implementation, reconciliation.Apply, reconciliation.BlockDone, settings)))
	if reason != "" || !reflect.DeepEqual(got, want) {
		t.Fatalf("a completed settings block makes the inspection %+v (%q), want %+v", got, reason, want)
	}
}

func TestInspectIgnoresTheUnitBlockEvidence(t *testing.T) {
	catalog, cluster := inspectedCluster()
	want, reason := Inspect(catalog, cluster, provedPair(t))
	if reason != "" {
		t.Fatalf("the proved pair inspects as %q", reason)
	}
	unit := inspectedClusterEvidence(t, func(e *Evidence) {
		e.Action, e.MonAddrs, e.Dashboard = "service", []MonitorAddress{}, DashboardEvidence{}
		e.Daemons = []UnitDaemon{{Host: "rhel-01.lab.test", Name: "nfs.files.0.0.rhel-01.abcdef", Service: "nfs.files"}}
	})
	got, reason := Inspect(catalog, cluster, append(provedPair(t), inspectedProof(Implementation, reconciliation.Apply, reconciliation.BlockDone, unit)))
	if reason != "" || !reflect.DeepEqual(got, want) {
		t.Fatalf("a completed unit block makes the inspection %+v (%q), want %+v", got, reason, want)
	}
}

func TestInspectReportsAReasonUnlessBothProofsAreCompleteAndReady(t *testing.T) {
	catalog, cluster := inspectedCluster()
	otherAction := inspectedClusterEvidence(t, func(e *Evidence) { e.Action = "pool"; e.Resource = ResourceEvidence{ID: "1"} })
	tests := []struct {
		name   string
		proofs func() []lifecycle.BlockEvidence
		reason string
	}{
		{"no evidence at all", func() []lifecycle.BlockEvidence { return nil }, noImageProof},
		{"no image proof", func() []lifecycle.BlockEvidence { return provedPair(t)[1:] }, noImageProof},
		{"no cluster proof", func() []lifecycle.BlockEvidence { return provedPair(t)[:1] }, noClusterProof},
		{"image block still running", func() []lifecycle.BlockEvidence {
			p := provedPair(t)
			p[0].State = reconciliation.BlockRunning
			return p
		}, noImageProof},
		{"cluster block of a destroy", func() []lifecycle.BlockEvidence {
			p := provedPair(t)
			p[1].Verb = reconciliation.Destroy
			return p
		}, noClusterProof},
		{"another cluster's blocks", func() []lifecycle.BlockEvidence {
			p := provedPair(t)
			p[0].Object, p[1].Object = "other", "other"
			return p
		}, noImageProof},
		{"ambiguous image proofs", func() []lifecycle.BlockEvidence { p := provedPair(t); return append(p, p[0]) }, "ambiguous evidence"},
		{"ambiguous cluster proofs", func() []lifecycle.BlockEvidence { p := provedPair(t); return append(p, p[1]) }, "ambiguous evidence"},
		{"image evidence unreadable", func() []lifecycle.BlockEvidence {
			p := provedPair(t)
			p[0].Evidence = json.RawMessage(`{"digest":`)
			return p
		}, "resolved linux/amd64 manifest"},
		{"image digest invalid", func() []lifecycle.BlockEvidence {
			p := provedPair(t)
			p[0].Evidence = inspectedImageEvidence(t, func(e *ImageEvidence) { e.Digest = "sha256:short" })
			return p
		}, "resolved linux/amd64 manifest"},
		{"image platform foreign", func() []lifecycle.BlockEvidence {
			p := provedPair(t)
			p[0].Evidence = inspectedImageEvidence(t, func(e *ImageEvidence) { e.Platform = "linux/arm64" })
			return p
		}, "resolved linux/amd64 manifest"},
		{"image absent", func() []lifecycle.BlockEvidence {
			p := provedPair(t)
			p[0].Evidence = inspectedImageEvidence(t, func(e *ImageEvidence) { e.Absent = true })
			return p
		}, "resolved linux/amd64 manifest"},
		{"cluster not ready", func() []lifecycle.BlockEvidence {
			p := provedPair(t)
			p[1].Evidence = inspectedClusterEvidence(t, func(e *Evidence) { e.Ready, e.Partial = false, true })
			return p
		}, "does not prove a ready cluster"},
		{"cluster evidence of another action", func() []lifecycle.BlockEvidence {
			p := provedPair(t)
			p[1].Evidence = otherAction
			return p
		}, "does not prove a ready cluster"},
		{"cluster evidence unreadable", func() []lifecycle.BlockEvidence {
			p := provedPair(t)
			p[1].Evidence = json.RawMessage(`{}`)
			return p
		}, "does not prove a ready cluster"},
		{"monitor names no declared node", func() []lifecycle.BlockEvidence {
			p := provedPair(t)
			p[1].Evidence = inspectedClusterEvidence(t, func(e *Evidence) { e.Monitors = []string{"foreign.lab.test"} })
			return p
		}, "names no declared node"},
		{"active manager names no declared node", func() []lifecycle.BlockEvidence {
			p := provedPair(t)
			p[1].Evidence = inspectedClusterEvidence(t, func(e *Evidence) { e.MGRActive = "foreign.lab.test" })
			return p
		}, "active manager in the cluster's evidence names no declared node"},
		{"standby manager names no declared node", func() []lifecycle.BlockEvidence {
			p := provedPair(t)
			p[1].Evidence = inspectedClusterEvidence(t, func(e *Evidence) { e.MGRStandbys = []string{"foreign.lab.test"} })
			return p
		}, "standby manager in the cluster's evidence names no declared node"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := Inspect(catalog, cluster, tt.proofs())
			if !strings.Contains(reason, tt.reason) || !reflect.DeepEqual(got, Inspection{}) {
				t.Fatalf("inspection = %+v (%q), want a reason containing %q", got, reason, tt.reason)
			}
		})
	}
}

func TestInspectNeedsTheMonitorsMachineAddress(t *testing.T) {
	_, cluster := inspectedCluster()
	bare := api.NewObject(api.Machine, "m1", api.Value{}, api.MapValue())
	catalog := api.NewCatalog([]api.Object{cluster, bare, inspectedMachine("m2", "192.0.2.12"), inspectedMachine("m3", "192.0.2.13")})
	got, reason := Inspect(catalog, cluster, provedPair(t))
	if !strings.Contains(reason, "Machine contact address") || !reflect.DeepEqual(got, Inspection{}) {
		t.Fatalf("inspection = %+v (%q)", got, reason)
	}
}

func TestInspectReportsAnUnreachableOrUnservedDashboardUnproved(t *testing.T) {
	catalog, cluster := inspectedCluster()
	for _, tt := range []struct {
		name   string
		edit   func(*Evidence)
		urls   int
		reason string
	}{
		{"unreachable port", func(e *Evidence) { e.Dashboard.Reachable = false }, 2, "did not present the stored certificate"},
		{"no served URL", func(e *Evidence) { e.Dashboard = DashboardEvidence{CertificateSHA256: inspectedCertificate} }, 0, "did not observe the active manager"},
		{"no stored certificate", func(e *Evidence) { e.Dashboard = DashboardEvidence{URL: "https://rhel-01.lab.test:8443/"} }, 2, "did not observe the active manager"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			proofs := provedPair(t)
			proofs[1].Evidence = inspectedClusterEvidence(t, tt.edit)
			got, reason := Inspect(catalog, cluster, proofs)
			if reason != "" || got.Dashboard.Status != DashboardUnproved || !strings.Contains(got.Dashboard.Reason, tt.reason) || len(got.Dashboard.URLs) != tt.urls ||
				got.Dashboard.User != "admin" || got.FSID != inspectedFSID {
				t.Fatalf("inspection = %+v (%q)", got, reason)
			}
		})
	}
}
