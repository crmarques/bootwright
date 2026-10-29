package ceph

import (
	"maps"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/storage/cephcluster"
	"github.com/crmarques/bootwright/internal/trust"
)

const StretchRoot = "default"

var StretchStages = []string{"located", "rule-present", "election-connectivity", "stretch-enabled", "verified"}

type Stretch struct {
	DataSites      []string `json:"dataSites"`
	FailureDomain  string   `json:"failureDomain"`
	Rule           string   `json:"rule"`
	Tiebreaker     string   `json:"tiebreaker"`
	TiebreakerSite string   `json:"tiebreakerSite"`
}

type StretchEvidence struct {
	Enabled bool   `json:"enabled"`
	Stage   string `json:"stage"`
}

func stretchOf(cluster api.Object) *Stretch {
	stretch := cluster.Spec().Get("topology", "stretch")
	if !stretch.Present() {
		return nil
	}
	s := &Stretch{DataSites: slices.Sorted(slices.Values(stretch.Get("dataSites").Strings())), FailureDomain: stretch.Get("failureDomain").Text(), Rule: stretch.Get("ruleName").Text(),
		TiebreakerSite: stretch.Get("tiebreaker", "site").Text()}
	if s.Rule == "" {
		s.Rule = "stretch-rule"
	}
	for _, node := range cluster.Spec().Get("topology", "nodes").Items() {
		if node.Get("name").Text() == stretch.Get("tiebreaker", "node").Text() {
			s.Tiebreaker = cephcluster.NativeHostname(node)
		}
	}
	return s
}

func nodeSite(c api.Catalog, node api.Value) string {
	if site := node.Get("site").Text(); site != "" {
		return site
	}
	m, _ := c.Find(api.Machine, node.Get("machineRef").Text())
	return m.Spec().Get("placement", "site").Text()
}

func stretchLocation(s *Stretch, site string) map[string]string {
	return map[string]string{s.FailureDomain: site, "root": StretchRoot}
}

func locateHosts(r *Request, c api.Catalog, cluster api.Object) error {
	if r.Stretch == nil {
		return nil
	}
	for _, node := range cluster.Spec().Get("topology", "nodes").Items() {
		name, site := cephcluster.NativeHostname(node), nodeSite(c, node)
		at := slices.IndexFunc(r.Hosts, func(h Host) bool { return h.Name == name })
		if at < 0 || name == r.Stretch.Tiebreaker {
			continue
		}
		if !slices.Contains(r.Stretch.DataSites, site) {
			return failure("api.invariant", "stretch Ceph host "+name+" is at no data site of CephCluster "+cluster.Name(), "place its Machine at one of spec.topology.stretch.dataSites")
		}
		r.Hosts[at].Location = stretchLocation(r.Stretch, site)
	}
	return nil
}

func monLocations(r Request, hosts []string) map[string][]string {
	out := map[string][]string{}
	for _, name := range hosts {
		if name == r.Stretch.Tiebreaker {
			out[name] = []string{r.Stretch.FailureDomain + "=" + r.Stretch.TiebreakerSite}
			continue
		}
		for _, h := range r.Hosts {
			if h.Name == name {
				out[name] = []string{r.Stretch.FailureDomain + "=" + h.Location[r.Stretch.FailureDomain]}
			}
		}
	}
	return out
}

func daemonSpec(r Request, kind string, svc api.Value, hosts []string) (*nativeSpec, error) {
	switch {
	case kind == "mds":
		if svc.Get("spec").Len() != 0 || !svc.Get("service_id").Present() {
			return nil, failure("lifecycle.unsupported", "core MDS services carry only service_id and placement", "declare the MDS service with its service_id and placement and no spec")
		}
	case kind == "mon" && r.Stretch != nil:
		return stretchMONSpec(r, svc, hosts)
	case svc.Get("spec").Len() != 0 || svc.Get("service_id").Present():
		return nil, failure("lifecycle.unsupported", "core MON/MGR service-specific options are not yet qualified", "declare MON/MGR placement without spec or service_id")
	}
	return nil, nil
}

func stretchMONSpec(r Request, svc api.Value, hosts []string) (*nativeSpec, error) {
	derived := monLocations(r, hosts)
	if !authoredMonLocations(svc, derived) || svc.Get("service_id").Present() {
		return nil, failure("lifecycle.unsupported", "the MON service of a stretch cluster takes its crush_locations from the stretch topology", "omit the mon service's spec, or set spec.crush_locations to exactly each MON host's failure-domain bucket at its site")
	}
	return &nativeSpec{CrushLocations: derived}, nil
}

func authoredMonLocations(svc api.Value, derived map[string][]string) bool {
	if svc.Get("spec").Len() == 0 {
		return true
	}
	if svc.Get("spec").Len() != 1 || !svc.Get("spec").Has("crush_locations") {
		return false
	}
	authored := map[string][]string{}
	for _, location := range svc.Get("spec", "crush_locations").Fields() {
		authored[location.Name] = location.Value.Strings()
	}
	return maps.EqualFunc(authored, derived, slices.Equal)
}

func poolRule(cluster api.Object) string {
	if s := stretchOf(cluster); s != nil {
		return s.Rule
	}
	return defaultRule
}

func fleetHostKey(h Host, security *Security, e lifecycle.Execution, readers DependencyProofs) (trust.HostKey, error) {
	key, err := provedHostKey(h, e, readers)
	if err == nil && h.Provided && security != nil && security.FIPS {
		err = fipsHostKey(h, key)
	}
	return key, err
}

func stretchRefusals() map[string]error {
	retire := "take back the whole cluster with bootwright destroy --context <context>, restore or change spec.topology.stretch with bootwright context update, then apply; Bootwright never runs mon disable_stretch_mode and never repeats an interrupted stretch stage"
	return map[string]error{
		"stretch-intent":         failure("lifecycle.unsupported", "the CephCluster's spec.topology.stretch, its tiebreaker, failure domain, rule or a host's site differs from the stretch intent the cluster was created with; stretch is frozen day-1 intent", "restore spec.topology.stretch and every node's site as they were applied, or "+retire),
		"stretch-interrupted":    failure("lifecycle.unknown", "a stretch activation stage was attempted and its readback does not show its result, so its outcome is unknown and Bootwright does not repeat it", "inspect the stage the native error names with ceph osd crush tree, ceph osd crush rule dump, ceph mon dump and ceph osd dump; "+retire),
		"stretch-location":       failure("lifecycle.unknown", "a Ceph host's CRUSH bucket or a MON's CRUSH location is not the one its site gives, and only the bootstrap host's bucket and an unset MON location are ever corrected", "inspect ceph osd crush tree and ceph mon dump; move the host bucket the native error names under root=default and its site's failure-domain bucket outside Bootwright, then apply again"),
		"stretch-rule":           failure("lifecycle.unknown", "a CRUSH rule of the stretch rule's name exists that this operation did not create, or it is not the stretch rule this operation created", "rename spec.topology.stretch.ruleName, or remove the foreign rule outside Bootwright before the cluster is created"),
		"stretch-foreign":        failure("lifecycle.unknown", "stretch mode is enabled on the cluster without this operation's attempt, or with another tiebreaker, rule or failure domain", "inspect ceph mon dump and ceph osd dump; Bootwright never adopts stretch mode it did not enable; "+retire),
		"stretch-drift":          failure("lifecycle.unknown", "a stretch activation stage this operation completed no longer holds: a host bucket, a MON location, the stretch rule, the election strategy or stretch mode itself changed", "inspect the stage the native error names with ceph osd crush tree, ceph osd crush rule dump, ceph mon dump and ceph osd dump and restore it outside Bootwright, then apply again; Bootwright never repairs a completed stretch stage"),
		"stretch-pools":          failure("lifecycle.unknown", "a pool of the stretch cluster is not on its stretch rule at size 4 and min_size 2 after stretch mode was enabled", "inspect ceph osd pool ls detail; a pool on another rule or protection keeps the cluster partial, Bootwright never moves a pool another tool created or a .rgw.root found off the rule; remove or move the pool outside Bootwright, then apply again"),
		"tiebreaker-foreign":     tiebreakerForeign(placeholderTiebreaker, "<fsid>"),
		"tiebreaker-unreachable": tiebreakerUnreachable(placeholderTiebreaker),
	}
}

var placeholderTiebreaker = Host{Machine: "<tiebreaker>", Address: "<address>", Port: 22, KnownHostsRef: "<known-hosts>", SSHKeyRef: "<fleet-key>", User: "<cluster-ssh-user>"}

func validStretchEvidence(e Evidence, r Request) bool {
	if r.Action != "cluster" || r.Stretch == nil {
		return e.Stretch == nil
	}
	return e.Stretch != nil && (e.Stretch.Stage == "" || slices.Contains(StretchStages, e.Stretch.Stage))
}

func stretchReady(e Evidence, r Request) bool {
	return r.Action != "cluster" || r.Stretch == nil || e.Stretch != nil && e.Stretch.Stage == "verified" && e.Stretch.Enabled
}
