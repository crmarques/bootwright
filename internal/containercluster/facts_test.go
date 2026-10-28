package containercluster

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// The controller and the managed services call each fact of every
// container-cluster kind unguarded, so a kind of the category publishes all.
func TestEveryContainerClusterKindPublishesEveryFact(t *testing.T) {
	for _, kind := range api.CategoryKinds(api.ContainerClusterCategory) {
		facts, ok := FactsOf(kind)
		if !ok || facts.ControllerTools == nil || facts.VirtualIPs == nil || facts.Endpoints == nil || facts.NodeNames == nil {
			t.Errorf("%s publishes no complete container-cluster facts", kind)
		}
	}
	for _, kind := range api.CategoryKinds(api.StorageClusterCategory) {
		if _, ok := FactsOf(kind); ok {
			t.Errorf("%s is no container-cluster kind but publishes its facts", kind)
		}
	}
}
