package lifecycle

import (
	"context"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func declaredStatus(kind api.Kind, object api.Object, claimed, refused []string) RealizationStatus {
	if !slices.Contains(claimed, string(kind)) || slices.Contains(refused, object.Identity()) {
		return RealizationUnsupported
	}
	return RealizationPending
}

func storageSummaries(catalog api.Catalog, claimed, refused []string) []ClusterSummary {
	clusters := clusterSummaries(catalog, api.StorageClusterCategory, claimed, refused)
	for index, cluster := range clusters {
		clusters[index].Resources = []ResourceSummary{}
		declared, _ := catalog.Find(api.Kind(cluster.Kind), cluster.Name)
		if management := declared.Spec().Get("management").Text(); management != "" && management != "managed" {
			continue
		}
		for _, kind := range api.Children(api.Kind(cluster.Kind)) {
			var rows []ResourceSummary
			for _, object := range catalog.OfKind(kind) {
				if object.Spec().Get("clusterRef").Text() == cluster.Name {
					rows = append(rows, ResourceSummary{Kind: string(kind), Name: object.Name(), Status: declaredStatus(kind, object, claimed, refused)})
				}
			}
			slices.SortFunc(rows, func(x, y ResourceSummary) int { return strings.Compare(x.Name, y.Name) })
			clusters[index].Resources = append(clusters[index].Resources, rows...)
		}
	}
	return clusters
}

func realizedStorage(clusters []ClusterSummary, realized map[string]RealizationStatus) []ClusterSummary {
	clusters = realizedClusters(clusters, realized)
	for _, cluster := range clusters {
		for index, resource := range cluster.Resources {
			if status, named := realized[resource.Kind+"/"+resource.Name]; named {
				cluster.Resources[index].Status = status
			}
		}
	}
	return clusters
}

func (s Service) StorageResources(ctx context.Context, contextName string) (*StorageResourcesResult, error) {
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	name, err := s.resolve(ctx, contextName)
	if err != nil {
		return nil, err
	}
	result := &StorageResourcesResult{Clusters: []ClusterSummary{}, Frozen: []reconciliation.Block{}}
	err = s.workspace.ReadLifecycle(ctx, name, func(view View) error {
		return s.storageResources(ctx, view, result)
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s Service) storageResources(ctx context.Context, view View, result *StorageResourcesResult) error {
	state, report, err := s.compiler.Compile(ctx, view.Inputs())
	if err != nil {
		return err
	}
	if state != nil && report != nil {
		result.Clusters = storageSummaries(state.Effective(), s.capabilityKinds(), Identities(s.unsupportedRefusals(state)))
	}
	store := s.store(view)
	index, err := store.Index(ctx)
	if err != nil || index.Current == "" {
		return err
	}
	operation, err := store.ReadOperation(ctx, index.Current)
	if err != nil {
		return err
	}
	plan, err := store.ReadPlan(ctx, operation.ID)
	if err != nil {
		return err
	}
	states, _, err := blockRecords(ctx, store, operation.ID, plan)
	if err != nil {
		return err
	}
	realized, err := realizations(ctx, store, operation, plan, states)
	if err != nil {
		return err
	}
	result.Clusters = realizedStorage(result.Clusters, realized)
	for _, block := range plan.Blocks {
		if storageFamily(api.Kind(block.Kind)) {
			result.Frozen = append(result.Frozen, block)
		}
	}
	return nil
}

func storageFamily(kind api.Kind) bool {
	for _, root := range api.CategoryKinds(api.StorageClusterCategory) {
		if kind == root || slices.Contains(api.Children(root), kind) {
			return true
		}
	}
	return false
}
