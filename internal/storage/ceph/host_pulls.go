package ceph

import (
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/storage/cephcluster"
)

const PullConcurrency = 3

const cephImageGroup = "pull-ceph-image"

type ImagePull struct {
	Group string   `json:"group"`
	Names []string `json:"names"`
}

func pullCandidates(c api.Catalog, cluster api.Object, auxiliary []AuxiliaryReference) map[string][]string {
	all := []string{}
	native := map[string]string{}
	for _, node := range cluster.Spec().Get("topology", "nodes").Items() {
		hostname := cephcluster.NativeHostname(node)
		native[node.Get("name").Text()] = hostname
		all = append(all, hostname)
	}
	smb := len(clusterSMBResources(c, cluster.Name())) != 0
	out := map[string][]string{}
	for _, a := range auxiliary {
		hosts, exact := []string{}, !(smb && (a.Name == "samba" || a.Name == "samba_metrics"))
		needed := false
		for _, svc := range cluster.Spec().Get("services").Items() {
			if !exact || !slices.Contains(auxiliaryNeeds(svc), a.Name) {
				continue
			}
			needed = true
			names, issues := cephcluster.ResolveServicePlacement(cluster, svc)
			if len(issues) != 0 || len(names) == 0 {
				exact = false
				break
			}
			for _, name := range names {
				hostname, ok := native[name]
				if !ok {
					exact = false
					break
				}
				hosts = append(hosts, hostname)
			}
		}
		if !exact || !needed {
			hosts = slices.Clone(all)
		}
		slices.Sort(hosts)
		out[a.Name] = slices.Compact(hosts)
	}
	return out
}

func hostPulls(candidates map[string][]string, auxiliary []AuxiliaryReference, host string) []ImagePull {
	names := []string{}
	for _, a := range auxiliary {
		if slices.Contains(candidates[a.Name], host) {
			names = append(names, a.Name)
		}
	}
	pulls := []ImagePull{}
	for batch := range slices.Chunk(names, PullConcurrency) {
		pulls = append(pulls, ImagePull{Group: pullGroup(batch), Names: slices.Clone(batch)})
	}
	return pulls
}

func pullGroup(names []string) string {
	return "pull-" + strings.ReplaceAll(strings.Join(names, "-"), "_", "-")
}

func pullDescription(names []string) string {
	settings := make([]string, 0, len(names))
	for _, name := range names {
		settings = append(settings, "container_image_"+name)
	}
	if len(settings) == 1 {
		return "pull " + settings[0] + " by digest"
	}
	return "pull " + strings.Join(settings[:len(settings)-1], ", ") + " and " + settings[len(settings)-1] + " by digest together"
}

func validPulls(r HostRequest) bool {
	if r.Pulls == nil || len(r.Pulls) > len(AuxiliaryNames) {
		return false
	}
	frozen := make([]string, 0, len(r.Cluster.Auxiliary))
	for _, a := range r.Cluster.Auxiliary {
		frozen = append(frozen, a.Name)
	}
	last := -1
	for i, pull := range r.Pulls {
		if len(pull.Names) == 0 || len(pull.Names) > PullConcurrency || pull.Group != pullGroup(pull.Names) || i < len(r.Pulls)-1 && len(pull.Names) != PullConcurrency {
			return false
		}
		for _, name := range pull.Names {
			at := slices.Index(frozen, name)
			if at <= last {
				return false
			}
			last = at
		}
	}
	return true
}

func pullGroups(pulls []ImagePull, machineName string) []reconciliation.Group {
	groups := []reconciliation.Group{{ID: cephImageGroup, Description: "pull the frozen Ceph image by digest", Machines: []string{machineName}}}
	for _, pull := range pulls {
		groups = append(groups, reconciliation.Group{ID: pull.Group, Description: pullDescription(pull.Names), Machines: []string{machineName}})
	}
	return groups
}
