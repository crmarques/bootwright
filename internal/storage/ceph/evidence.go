package ceph

import (
	"encoding/json"
	"net/netip"
	"regexp"
	"slices"

	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

const EvidenceVersion = "ceph-core-evidence-v3"

const DashboardPort = "8443"

var dashboardURL = regexp.MustCompile(`\Ahttps://(?:[A-Za-z0-9.-]{1,253}|\[[0-9A-Fa-f:.]{2,45}\]):8443/\z`)
var certificateDigest = regexp.MustCompile(`\A[0-9a-f]{64}\z`)
var resourceID = regexp.MustCompile(`\A(?:0|[1-9][0-9]{0,18})\z`)

type MonitorAddress struct {
	Address string `json:"address"`
	Host    string `json:"host"`
}

type DashboardEvidence struct {
	CertificateSHA256 string `json:"certificateSHA256"`
	Reachable         bool   `json:"reachable"`
	URL               string `json:"url"`
}

type OSDEvidence struct {
	Host string `json:"host"`
	ID   int    `json:"id"`
	Path string `json:"path"`
	UUID string `json:"uuid"`
}

type ResourceEvidence struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

type Evidence struct {
	Action      string            `json:"action"`
	Client      string            `json:"client"`
	Dashboard   DashboardEvidence `json:"dashboard"`
	Exists      bool              `json:"exists"`
	FSID        string            `json:"fsid"`
	Health      string            `json:"health"`
	Hosts       []string          `json:"hosts"`
	MGRActive   string            `json:"mgrActive"`
	MGRStandbys []string          `json:"mgrStandbys"`
	MonAddrs    []MonitorAddress  `json:"monAddrs"`
	Monitors    []string          `json:"monitors"`
	OSDs        []OSDEvidence     `json:"osds"`
	Owned       bool              `json:"owned"`
	Owner       string            `json:"owner"`
	Partial     bool              `json:"partial"`
	Ready       bool              `json:"ready"`
	Removed     bool              `json:"removed"`
	Request     string            `json:"request"`
	Resource    ResourceEvidence  `json:"resource"`
	Version     string            `json:"version"`
}

func evidenceFailure() error {
	return failure("lifecycle.unknown", "Ceph evidence does not prove the frozen ownership and resource scope", "")
}

func decodeEvidence(data []byte) (Evidence, error) {
	e, err := reconciliation.DecodeEvidence[Evidence](data, 60*1024, "Ceph")
	if err != nil {
		return e, err
	}
	fields, err := reconciliation.DecodeEvidence[map[string]json.RawMessage](data, 60*1024, "Ceph members")
	if err != nil || len(fields) != 20 {
		return Evidence{}, evidenceFailure()
	}
	required := []string{"version", "request", "action", "fsid", "owner", "owned", "exists", "ready", "removed", "partial", "health", "hosts", "monitors", "monAddrs", "mgrActive", "mgrStandbys", "osds", "resource", "client", "dashboard"}
	for _, key := range required {
		v, ok := fields[key]
		if !ok || string(v) == "null" {
			return Evidence{}, evidenceFailure()
		}
	}
	if canonicaljson.ProveObject(data) != nil {
		return Evidence{}, evidenceFailure()
	}
	osds, err := reconciliation.DecodeEvidence[[]map[string]json.RawMessage](fields["osds"], 60*1024, "Ceph OSD members")
	if err != nil {
		return Evidence{}, evidenceFailure()
	}
	for _, osd := range osds {
		if !closedMembers(osd, "host", "path", "id", "uuid") {
			return Evidence{}, evidenceFailure()
		}
	}
	addresses, err := reconciliation.DecodeEvidence[[]map[string]json.RawMessage](fields["monAddrs"], 60*1024, "Ceph monitor address members")
	if err != nil {
		return Evidence{}, evidenceFailure()
	}
	for _, address := range addresses {
		if !closedMembers(address, "host", "address") {
			return Evidence{}, evidenceFailure()
		}
	}
	dashboard, err := reconciliation.DecodeEvidence[map[string]json.RawMessage](fields["dashboard"], 60*1024, "Ceph dashboard members")
	if err != nil || !closedMembers(dashboard, "url", "certificateSHA256", "reachable") {
		return Evidence{}, evidenceFailure()
	}
	resource, err := reconciliation.DecodeEvidence[map[string]json.RawMessage](fields["resource"], 60*1024, "Ceph resource members")
	if err != nil || !closedMembers(resource, "id", "path") {
		return Evidence{}, evidenceFailure()
	}
	return e, nil
}

func closedMembers(members map[string]json.RawMessage, keys ...string) bool {
	if len(members) != len(keys) {
		return false
	}
	for _, key := range keys {
		v, ok := members[key]
		if !ok || string(v) == "null" {
			return false
		}
	}
	return true
}

func validDashboard(e Evidence, r Request) bool {
	d := e.Dashboard
	if d.URL != "" && !dashboardURL.MatchString(d.URL) || d.CertificateSHA256 != "" && !certificateDigest.MatchString(d.CertificateSHA256) {
		return false
	}
	if d.Reachable && (d.URL == "" || d.CertificateSHA256 == "") {
		return false
	}
	if r.Action != "cluster" {
		return d == DashboardEvidence{} && len(e.MonAddrs) == 0
	}
	seen := map[string]bool{}
	for _, m := range e.MonAddrs {
		address, err := netip.ParseAddr(m.Address)
		if err != nil || address.String() != m.Address || seen[m.Host] || !slices.Contains(hostNames(r), m.Host) {
			return false
		}
		seen[m.Host] = true
	}
	return true
}

func sameNames(actual, want []string) bool {
	a := slices.Clone(actual)
	b := slices.Clone(want)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b) && len(slices.Compact(a)) == len(a)
}
func subsetNames(actual, want []string) bool {
	seen := map[string]bool{}
	for _, name := range actual {
		if seen[name] || !slices.Contains(want, name) {
			return false
		}
		seen[name] = true
	}
	return true
}
func hostNames(r Request) []string {
	names := []string{}
	for _, h := range r.Hosts {
		names = append(names, h.Name)
	}
	return names
}

func servicePlacement(r Request, kind string) ([]string, int) {
	for _, raw := range r.Services {
		v, err := reconciliation.DecodeEvidence[nativeService](raw, 60*1024, "Ceph native service")
		if err != nil || v.ServiceType != kind {
			continue
		}
		count := len(v.Placement.Hosts)
		if v.Placement.Count != nil {
			count = int(*v.Placement.Count)
		}
		return v.Placement.Hosts, count
	}
	return nil, 0
}

func validOSDs(e Evidence, r Request, complete bool) bool {
	expected := map[string]bool{}
	for _, h := range r.Hosts {
		for _, d := range h.Devices {
			expected[h.Name+"\x00"+d.Path] = true
		}
	}
	ids := map[int]bool{}
	uuids := map[string]bool{}
	seen := map[string]bool{}
	for _, osd := range e.OSDs {
		key := osd.Host + "\x00" + osd.Path
		if !expected[key] || seen[key] || osd.ID < 0 || ids[osd.ID] || osd.UUID == "" || uuids[osd.UUID] {
			return false
		}
		seen[key] = true
		ids[osd.ID] = true
		uuids[osd.UUID] = true
	}
	return !complete || len(expected) > 0 && len(seen) == len(expected)
}

func validateScope(e Evidence, r Request) bool {
	if !subsetNames(e.Hosts, hostNames(r)) || !validOSDs(e, r, false) {
		return false
	}
	mons, _ := servicePlacement(r, "mon")
	mgrs, _ := servicePlacement(r, "mgr")
	active := []string{}
	if e.MGRActive != "" {
		active = append(active, e.MGRActive)
	}
	if !subsetNames(e.Monitors, mons) || !subsetNames(append(active, e.MGRStandbys...), mgrs) {
		return false
	}
	if e.Resource.Path != "" {
		return false
	}
	if r.Action == "client" {
		return r.Client != nil && (e.Client == "" || e.Client == "client."+r.Client.Name) && e.Resource == (ResourceEvidence{})
	}
	if e.Client != "" {
		return false
	}
	if r.Action == "cluster" && e.Resource != (ResourceEvidence{}) {
		return false
	}
	if r.Action == "pool" && e.Resource.ID != "" && !resourceID.MatchString(e.Resource.ID) {
		return false
	}
	return true
}

func validateReady(e Evidence, r Request) bool {
	if !sameNames(e.Hosts, hostNames(r)) {
		return false
	}
	_, monCount := servicePlacement(r, "mon")
	_, mgrCount := servicePlacement(r, "mgr")
	if monCount <= 0 || len(e.Monitors) != monCount || mgrCount <= 0 || e.MGRActive == "" || len(e.MGRStandbys)+1 != mgrCount || !validOSDs(e, r, true) || !slices.Contains([]string{"HEALTH_OK", "HEALTH_WARN", "HEALTH_ERR"}, e.Health) {
		return false
	}
	switch r.Action {
	case "cluster":
		return true
	case "pool":
		return r.Pool != nil && resourceID.MatchString(e.Resource.ID)
	case "client":
		return r.Client != nil && e.Client == "client."+r.Client.Name
	}
	return false
}

// EvaluateEvidence rejects foreign, incomplete or contradictory identity
// before deriving any lifecycle outcome. An unreachable host never proves
// absence; cluster removal must enumerate the entire frozen host scope.
func EvaluateEvidence(data []byte, r Request, digest string, removal bool) (reconciliation.EffectState, error) {
	e, err := decodeEvidence(data)
	if err != nil {
		return reconciliation.EffectUnknown, err
	}
	if e.Version != EvidenceVersion || e.Request != digest || digest == "" || e.Action != r.Action || e.Owner != r.Owner || r.Owner == "" || e.FSID != r.FSID || r.FSID == "" || !validateScope(e, r) || !validDashboard(e, r) {
		return reconciliation.EffectUnknown, evidenceFailure()
	}
	if e.Ready && (!e.Owned || !e.Exists || e.Partial || e.Removed) || e.Removed && (!e.Owned || e.Exists || e.Ready || e.Partial) || e.Partial && (!e.Owned || !e.Exists || e.Ready || e.Removed) {
		return reconciliation.EffectUnknown, evidenceFailure()
	}
	// Daemon absence cannot prove cluster disk/key/authorization cleanup. No
	// image is qualified to publish such a removal proof in this version.
	if removal && r.Action == "cluster" && !e.Ready {
		return reconciliation.EffectUnknown, evidenceFailure()
	}
	if !e.Exists && !e.Ready && !e.Partial {
		if r.Action == "cluster" && (!sameNames(e.Hosts, hostNames(r)) || len(e.Monitors) != 0 || len(e.OSDs) != 0 || e.MGRActive != "" || len(e.MGRStandbys) != 0) {
			return reconciliation.EffectUnknown, evidenceFailure()
		}
		if e.Resource != (ResourceEvidence{}) || e.Client != "" {
			return reconciliation.EffectUnknown, evidenceFailure()
		}
		if removal {
			if !e.Removed {
				return reconciliation.EffectUnknown, evidenceFailure()
			}
			return reconciliation.EffectCompleted, nil
		}
		return reconciliation.EffectNoEffect, nil
	}
	if !e.Owned {
		return reconciliation.EffectUnknown, evidenceFailure()
	}
	if e.Ready {
		if !validateReady(e, r) {
			return reconciliation.EffectUnknown, evidenceFailure()
		}
		if removal {
			return reconciliation.EffectNoEffect, nil
		}
		return reconciliation.EffectCompleted, nil
	}
	if e.Partial {
		return reconciliation.EffectPartial, nil
	}
	return reconciliation.EffectUnknown, evidenceFailure()
}
