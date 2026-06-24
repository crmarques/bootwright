package workflow

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/ownership"
)

// UndeclaredResource is a Bootwright-owned resource recorded in the context ownership
// store that no longer corresponds to any declared desired-state object — an orphan
// left by removing the object from desired state without destroying it. It is reported
// (never auto-removed) by state-check and destroy --dry-run so an operator notices it;
// a full `bootwright destroy` reclaims it via the ownership-record sweep. Apply never
// prunes it (apply does not destroy).
type UndeclaredResource struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Cluster  string `json:"cluster,omitempty"`
	Provider string `json:"provider,omitempty"`
	Host     string `json:"host,omitempty"`
}

// OwnershipOrphans returns the Bootwright-owned ownership records whose backing
// desired-state object is gone, correlated on the most specific identity the record
// carries: machine, else cluster, else provider. A record carrying none of those is
// not flagged — this is read-only reporting, so it stays conservative and never
// produces false positives. Pass the FULL desired state (not a --clusters-scoped
// subset) and the context-filtered records.
func OwnershipOrphans(state v1alpha1.State, records []ownership.ResourceRecord) []UndeclaredResource {
	machines := map[string]bool{}
	for _, m := range state.Machines {
		machines[m.Metadata.Name] = true
	}
	clusters := map[string]bool{}
	for _, c := range state.ContainerClusters {
		clusters[c.Metadata.Name] = true
	}
	for _, c := range state.StorageClusters {
		clusters[c.Metadata.Name] = true
	}
	providers := map[string]bool{}
	for _, p := range state.InfraProviders {
		providers[p.Metadata.Name] = true
	}

	var out []UndeclaredResource
	for _, r := range records {
		if r.Owner != "" && r.Owner != ownership.Owner {
			continue // never report a resource Bootwright does not own
		}
		declared := true
		switch {
		case r.Machine != "":
			declared = machines[r.Machine]
		case r.Cluster != "":
			declared = clusters[r.Cluster]
		case r.Provider != "":
			declared = providers[r.Provider]
		}
		if declared {
			continue
		}
		out = append(out, UndeclaredResource{
			Kind: r.Kind, Name: r.Name, Cluster: r.Cluster, Provider: r.Provider, Host: r.Host,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out
}
