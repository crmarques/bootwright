package workflow

import (
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/ownership"
)

// infraComponentRecordNameLabel is the label key the ownership_record role writes
// carrying the bare InfraComponent name (resource record labels: bootwright.name).
// It is the correlation key for infra-component orphan detection, since those
// records stamp the literal "InfraComponent" sentinel as their provider rather
// than a real InfraProvider name.
const infraComponentRecordNameLabel = "bootwright.name"

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
// carries: machine, else cluster, else (for infra-component records) the component
// name, else provider. A record carrying none of those is not flagged — this is
// read-only reporting, so it stays conservative and never produces false positives.
// Pass the FULL desired state (not a --clusters-scoped subset) and the
// context-filtered records.
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
	infraComponents := map[string]bool{}
	for _, c := range state.InfraComponents {
		infraComponents[c.Metadata.Name] = true
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
		case r.Kind == string(ownership.KindInfraComponent):
			// infra-component records stamp the literal "InfraComponent" sentinel as
			// their provider (these components are not provider-scoped), so they cannot
			// correlate against real InfraProvider names — doing so would flag every
			// declared component as undeclared. Correlate by the component's own name.
			declared = infraComponents[infraComponentRecordName(r)]
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

// infraComponentRecordName returns the bare InfraComponent name an infra-component
// ownership record correlates against: the bootwright.name label the role stamps,
// falling back to stripping the "<sentinel>-" prefix the record name carries (the
// record name is "<providerName>-<componentName>" with providerName fixed to the
// InfraComponent kind sentinel, so the sentinel is the prefix to strip).
func infraComponentRecordName(r ownership.ResourceRecord) string {
	if name := r.Labels[infraComponentRecordNameLabel]; name != "" {
		return name
	}
	return strings.TrimPrefix(r.Name, v1alpha1.KindInfraComponent+"-")
}
