package workflow

import (
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/ownership"
)

const infraComponentRecordNameLabel = "bootwright.name"

type UndeclaredResource struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Cluster  string `json:"cluster,omitempty"`
	Provider string `json:"provider,omitempty"`
	Host     string `json:"host,omitempty"`
}

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
			continue
		}
		declared := true
		switch {
		case r.Machine != "":
			declared = machines[r.Machine]
		case r.Cluster != "":
			declared = clusters[r.Cluster]
		case r.Kind == string(ownership.KindInfraComponent):
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

func infraComponentRecordName(r ownership.ResourceRecord) string {
	if name := r.Labels[infraComponentRecordNameLabel]; name != "" {
		return name
	}
	return strings.TrimPrefix(r.Name, v1alpha1.KindInfraComponent+"-")
}
